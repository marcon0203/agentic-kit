package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

// PluginHandlers is the 插件体系's HTTP transport (spec-20). P1 scope
// matches the spec's own phasing: upload + private install/uninstall/
// update work end to end; the public market listing and moderation queue
// (visibility=public review flow) are P5's job, not wired to a route here
// yet even though the domain service already supports them.
type PluginHandlers struct {
	svc    *plugin.Service
	assets assetGetter
}

// assetGetter is the one OSS method GetAsset needs — satisfied by the same
// object store the Skill upload feature already wires
// (internal/adapter/oss.Store), passed in separately from svc because
// serving a static file is a pure passthrough, not a plugin.Service
// concern.
type assetGetter interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// NewPluginHandlers wires the CRUD/install surface. assets may be nil —
// GetAsset then rejects with a clear "not configured" error instead of a
// nil-pointer panic, same convention as every other OSS-optional feature.
func NewPluginHandlers(svc *plugin.Service, assets assetGetter) *PluginHandlers {
	return &PluginHandlers{svc: svc, assets: assets}
}

// maxPluginZipUploadBytes bounds the raw multipart body before it's ever
// handed to archive/zip — a hard ceiling on the wire, ahead of
// plugin.Service.Upload's own MaxPackageBytes check, so an oversized
// request never gets far enough to allocate that much memory.
const maxPluginZipUploadBytes = plugin.MaxPackageBytes

type pluginDTO struct {
	ID           string    `json:"id"`
	PluginID     string    `json:"plugin_id"`
	Version      string    `json:"version"`
	DisplayName  string    `json:"display_name,omitempty"`
	Manifest     any       `json:"manifest"`
	Visibility   string    `json:"visibility"`
	ReviewStatus string    `json:"review_status"`
	Status       int16     `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

func toPluginDTO(p plugin.Plugin) pluginDTO {
	displayName, _ := p.Manifest["display_name"].(string)
	return pluginDTO{
		ID: strconv.FormatInt(p.ID, 10), PluginID: p.PluginID, Version: p.Version,
		DisplayName: displayName, Manifest: map[string]any(p.Manifest),
		Visibility: string(p.Visibility), ReviewStatus: string(p.ReviewStatus),
		Status: int16(p.Status), CreatedAt: p.CreatedAt,
	}
}

func toPluginDTOs(items []plugin.Plugin) []pluginDTO {
	out := make([]pluginDTO, 0, len(items))
	for _, p := range items {
		out = append(out, toPluginDTO(p))
	}
	return out
}

type pluginInstallationDTO struct {
	PluginID   string         `json:"plugin_id"`
	Version    string         `json:"version"`
	Resolution string         `json:"resolution"`
	Config     map[string]any `json:"config"`
	Granted    []string       `json:"granted"`
	Status     int16          `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
}

func toPluginInstallationDTO(in plugin.Installation) pluginInstallationDTO {
	granted := in.Granted
	if granted == nil {
		granted = []string{}
	}
	return pluginInstallationDTO{
		PluginID: in.PluginID, Version: in.Version, Resolution: string(in.Resolution),
		Config: map[string]any(in.Config), Granted: granted,
		Status: int16(in.Status), CreatedAt: in.CreatedAt,
	}
}

// RegisterSigningKey handles POST /plugins/signing-key — a publisher
// registers the Ed25519 public key Upload will verify their packages
// against (spec-20 §5.3). Must be called at least once before any Upload
// from this account can succeed; calling it again rotates the key.
func (h *PluginHandlers) RegisterSigningKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		PublicKey string `json:"public_key"` // base64, 32 raw bytes
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "public_key must be base64")
		return
	}

	if err := h.svc.RegisterSigningKey(r.Context(), userID, pub); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Upload handles POST /plugins (multipart/form-data: `signature` — base64
// over the SHA-256 digest of the raw .akp bytes — and a `zip` file field
// carrying the .akp package itself, spec-20 §3.1). plugin_id/version are
// read out of the package's own plugin.json rather than as separate form
// fields — there is exactly one source of truth for them, the manifest.
func (h *PluginHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPluginZipUploadBytes)
	if err := r.ParseMultipartForm(maxPluginZipUploadBytes); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed multipart body, or package exceeds the upload limit")
		return
	}

	sig, err := base64.StdEncoding.DecodeString(r.FormValue("signature"))
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "signature must be base64")
		return
	}

	file, _, err := r.FormFile("zip")
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "missing zip file field")
		return
	}
	defer func() { _ = file.Close() }()

	pkg, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "failed to read upload")
		return
	}

	manifest, files, err := extractAKP(pkg)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, err.Error())
		return
	}
	pluginID, _ := manifest["id"].(string)
	version, _ := manifest["version"].(string)

	created, err := h.svc.Upload(r.Context(), userID, plugin.UploadCommand{
		PluginID: pluginID, Version: version, Manifest: manifest,
		Package: pkg, Signature: sig, Files: files,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toPluginDTO(created))
}

// extractAKP unzips a .akp package into its manifest (plugin.json, parsed)
// and every other entry as a path->content map, rejecting zip-slip paths —
// same approach as extractSkillZip, since this is the same "an upload is
// an untrusted zip" transport-layer job.
func extractAKP(pkg []byte) (map[string]any, map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return nil, nil, errBadZip
	}

	files := make(map[string][]byte, len(zr.File))
	var manifestRaw []byte
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return nil, nil, errZipSlip
		}
		if int64(f.UncompressedSize64) > maxPluginZipUploadBytes {
			return nil, nil, errBadZip
		}

		rc, err := f.Open()
		if err != nil {
			return nil, nil, errBadZip
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, nil, errBadZip
		}
		if name == "plugin.json" {
			manifestRaw = content
			continue
		}
		files[name] = content
	}
	if manifestRaw == nil {
		return nil, nil, errValidation("package must contain plugin.json")
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, nil, errValidation("plugin.json is not valid JSON")
	}
	return manifest, files, nil
}

// List handles GET /plugins — every version the caller has published
// themselves. The public market listing (visibility=public,
// review_status=passed) is a separate P5 surface, deliberately not this
// endpoint: what you get back here is "what I uploaded," not "what's
// installable."
func (h *PluginHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMine(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": toPluginDTOs(items)})
}

// Get handles GET /plugins/{id} — {id} is a plugin_id ref (e.g.
// "acme.charts"), returning its latest enabled version.
func (h *PluginHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	p, err := h.svc.GetLatestVersion(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toPluginDTO(p))
}

type installPluginRequest struct {
	Version *string        `json:"version,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
	Granted []string       `json:"granted,omitempty"`
}

// Install handles POST /plugins/{id}/install. An empty body installs the
// latest version with no config/granted permissions.
func (h *PluginHandlers) Install(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req installPluginRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
			return
		}
	}

	in, err := h.svc.Install(r.Context(), userID, plugin.InstallCommand{
		PluginID: chi.URLParam(r, "id"), Version: req.Version,
		Config: plugin.Config(req.Config), Granted: req.Granted,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toPluginInstallationDTO(in))
}

type updatePluginInstallRequest struct {
	Version    *string        `json:"version,omitempty"`
	Resolution *string        `json:"resolution,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
	Granted    []string       `json:"granted,omitempty"`
}

// UpdateInstall handles PATCH /plugins/{id}/install — change the pinned
// version, resolution mode, config, or granted permissions. Nil fields
// (omitted in the request) are left alone, same PATCH semantics as every
// other context's Update.
func (h *PluginHandlers) UpdateInstall(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req updatePluginInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	var resolution *plugin.Resolution
	if req.Resolution != nil {
		res := plugin.Resolution(*req.Resolution)
		resolution = &res
	}

	in, err := h.svc.UpdateInstallation(r.Context(), userID, chi.URLParam(r, "id"), plugin.UpdateInstallationCommand{
		Version: req.Version, Resolution: resolution, Config: plugin.Config(req.Config), Granted: req.Granted,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toPluginInstallationDTO(in))
}

// Uninstall handles DELETE /plugins/{id}/install.
func (h *PluginHandlers) Uninstall(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Uninstall(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetAsset handles GET /plugins/assets/{id}/{ver}/* — spec-20 §5.2's
// "独立资源域" content delivery for a renderer's iframe (§4.2): the entry
// HTML plus whatever JS/CSS it references relative to itself, all fetched
// through this same endpoint. Deliberately unauthenticated (see router.go)
// and deliberately not scoped to the caller's own installations — once a
// version exists at all, its files are addressable the same way any static
// asset CDN's are; {id}/{ver} is resolved against the real plugins table
// first, so the OSS key actually read is never the untrusted wildcard path
// alone.
func (h *PluginHandlers) GetAsset(w http.ResponseWriter, r *http.Request) {
	if h.assets == nil {
		writeErr(w, r, http.StatusServiceUnavailable, ErrValidationFailed, "plugin asset storage is not configured on this deployment (OSS)")
		return
	}

	pluginID, version := chi.URLParam(r, "id"), chi.URLParam(r, "ver")
	path := chi.URLParam(r, "*")
	if !validAssetPath(path) {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid asset path")
		return
	}

	p, err := h.svc.GetVersion(r.Context(), pluginID, version)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	content, err := h.assets.Get(r.Context(), p.OSSPrefix+"/"+path)
	if err != nil {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "asset not found")
		return
	}
	defer func() { _ = content.Close() }()

	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

// validAssetPath rejects the shapes that would let a wildcard path escape
// the plugin's own OSS prefix (zip-slip's request-path equivalent) — same
// checks extractSkillZip/extractAKP already apply to a zip entry's name.
func validAssetPath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "/") && !strings.Contains(path, "..")
}
