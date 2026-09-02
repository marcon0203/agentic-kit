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

// PluginHandlers is the 插件体系's HTTP transport (spec-20): upload,
// private install/uninstall/update, the public market listing, visibility
// toggling, and the admin moderation queue (spec-20 §5.4/P5).
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

// Market handles GET /plugins/market — the 组件广场"插件" tab listing: one
// row per plugin_id, its latest visibility=public + review_status=passed
// version (spec-20 §5.4). Deliberately a different endpoint from List:
// what List returns is "what I uploaded," what Market returns is "what's
// installable," and those are almost never the same set for a given
// caller.
func (h *PluginHandlers) Market(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	items, err := h.svc.ListMarket(r.Context())
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": toPluginDTOs(items)})
}

// ListInstalled handles GET /plugins/installed — every plugin the caller
// has installed into their own account, so the market UI can mark an
// already-installed entry instead of always offering "安装".
func (h *PluginHandlers) ListInstalled(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListInstallations(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	out := make([]pluginInstallationDTO, 0, len(items))
	for _, in := range items {
		out = append(out, toPluginInstallationDTO(in))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": out})
}

type installedToolDTO struct {
	Ref               string `json:"ref"`
	PluginID          string `json:"plugin_id"`
	PluginDisplayName string `json:"plugin_display_name,omitempty"`
	ToolName          string `json:"tool_name"`
	Description       string `json:"description,omitempty"`
}

// ListInstalledTools handles GET /plugins/installed/tools — every
// "plugin:{plugin_id}/{tool_name}" ref the caller's installed plugins make
// available, resolved from each installed version's manifest. This is
// what the Agent editor's capability picker needs to let a plugin's tools
// be added to capabilities.tools[] the same way any resource-center ref
// is (spec-20 §5.1).
func (h *PluginHandlers) ListInstalledTools(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListInstalledTools(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	out := make([]installedToolDTO, 0, len(items))
	for _, t := range items {
		out = append(out, installedToolDTO{Ref: t.Ref, PluginID: t.PluginID, PluginDisplayName: t.PluginDisplayName, ToolName: t.ToolName, Description: t.Description})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": out})
}

// List handles GET /plugins — every version the caller has published
// themselves. The public market listing is Market, above — deliberately
// not this endpoint: what you get back here is "what I uploaded," not
// "what's installable."
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

type setPluginVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

// SetVisibility handles PATCH /plugins/{id} — {id} here is a specific
// version's numeric row id (pluginDTO.ID), not a plugin_id ref, since
// visibility is a per-version flag: flipping one version to public does
// not touch any other version of the same plugin. Only the version's own
// publisher may do this (plugin.Service.SetVisibility enforces it);
// flipping to public is what enters the version into the moderation queue
// (spec-20 §5.4's private→public flow).
func (h *PluginHandlers) SetVisibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "id must be a numeric plugin version id")
		return
	}
	var req setPluginVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	visibility := plugin.Visibility(req.Visibility)
	if visibility != plugin.VisibilityPrivate && visibility != plugin.VisibilityPublic {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, `visibility must be "private" or "public"`)
		return
	}

	updated, err := h.svc.SetVisibility(r.Context(), userID, id, visibility)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toPluginDTO(updated))
}

// ListPendingReview handles GET /moderation/plugins — the admin review
// queue of every version awaiting a public-visibility decision (spec-20
// §5.4). Admin gating happens inside plugin.Service.ListPendingReview,
// same convention as operation.Service's report queue.
func (h *PluginHandlers) ListPendingReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListPendingReview(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": toPluginDTOs(items)})
}

type reviewPluginRequest struct {
	Approve bool `json:"approve"`
}

// Review handles POST /moderation/plugins/{id}/review — {id} is the
// version's numeric row id, same as SetVisibility. Approving sets
// review_status=passed (making the version eligible for Market); rejecting
// sets review_status=rejected. Admin-only, enforced inside the service.
func (h *PluginHandlers) Review(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "id must be a numeric plugin version id")
		return
	}
	var req reviewPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	updated, err := h.svc.Review(r.Context(), userID, id, req.Approve)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toPluginDTO(updated))
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
