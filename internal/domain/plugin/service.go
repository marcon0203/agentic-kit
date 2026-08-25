package plugin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// MaxPackageBytes caps a single .akp upload (spec-20 §5.3's automated
// gate). No number is prescribed by the spec; a generous but bounded
// ceiling keeps one plugin from filling OSS storage indefinitely.
const MaxPackageBytes = 50 * 1024 * 1024 // 50 MiB

// Service is the 插件体系 application service.
type Service struct {
	repo      Repository
	keys      PublisherKeys
	validator ManifestValidator
	admins    AdminDirectory
	cipher    CredentialCipher
	// wasm runs the automated gate's compilability/entry-existence check
	// (spec-20 §5.3). Nil is tolerated — Upload simply skips that one
	// check — so the domain package stays testable and wireable before
	// internal/adapter/extism exists in a given deployment path.
	wasm WasmValidator
	// objectStore backs Upload's file storage (spec-20 §3.1's .akp
	// contents besides plugin.json). Set via WithObjectStore — a separate
	// opt-in step, same shape as resource.Service.WithSkillUploads, since
	// OSS is genuinely optional infrastructure rather than a required
	// collaborator every deployment has.
	objectStore ObjectStore
}

func NewService(repo Repository, keys PublisherKeys, validator ManifestValidator, admins AdminDirectory, cipher CredentialCipher, wasm WasmValidator) *Service {
	return &Service{repo: repo, keys: keys, validator: validator, admins: admins, cipher: cipher, wasm: wasm}
}

// WithObjectStore enables Upload's file storage.
func (s *Service) WithObjectStore(store ObjectStore) *Service {
	s.objectStore = store
	return s
}

// RegisterSigningKey stores a publisher's Ed25519 public verification key,
// one per account (UPSERT — registering again rotates it). The private key
// never touches this call: it is generated and held by the publisher.
func (s *Service) RegisterSigningKey(ctx context.Context, userID int64, publicKey []byte) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return domain.Invalid(domain.CodeValidationFailed, "public key must be 32 bytes (Ed25519)")
	}
	if err := s.keys.Upsert(ctx, userID, publicKey); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// UploadCommand is one plugin package upload. Signature is computed by the
// publisher locally over the SHA-256 digest of the uploaded .akp package
// bytes — the server only ever verifies, never signs.
type UploadCommand struct {
	PluginID  string
	Version   string
	Manifest  map[string]any
	Package   []byte // raw .akp bytes, hashed here to verify Signature
	Signature []byte
	// Files is every non-manifest entry the .akp zip contained (spec-20
	// §3.1's layout: plugin.wasm, ui/*, assets/*, README.md), keyed by its
	// path inside the archive. Extracting the zip is the caller's job
	// (transport-layer file-format work); Upload only ever sees a clean
	// path->content map, same convention as resource.UploadSkillCommand.
	Files map[string][]byte
}

// wasmFileName is the .akp layout's fixed name for a plugin's backend
// WASM module (spec-20 §3.1) — absent for a frontend-only (renderers-only)
// plugin.
const wasmFileName = "plugin.wasm"

// Upload validates the manifest, verifies the publisher's signature over
// the package, and creates a new private/pending-review version. A
// publisher can install and test their own upload immediately (private
// visibility needs no review); entering the market queue is a separate,
// deliberate SetVisibility(Public) call.
func (s *Service) Upload(ctx context.Context, publisherID int64, cmd UploadCommand) (Plugin, error) {
	if s.objectStore == nil {
		return Plugin{}, domain.Invalid(domain.CodeValidationFailed, "plugin upload is not configured on this deployment (OSS)")
	}
	if len(cmd.Package) > MaxPackageBytes {
		return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, fmt.Sprintf("package exceeds the %d byte limit", MaxPackageBytes))
	}

	fieldErrs, err := s.validator.Validate(cmd.Manifest)
	if err != nil {
		return Plugin{}, domain.Internal(err)
	}
	if len(fieldErrs) > 0 {
		return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, "manifest failed schema validation").WithDetails(fieldErrs...)
	}
	if manifestID, _ := cmd.Manifest["id"].(string); manifestID != cmd.PluginID {
		return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, "manifest id does not match upload id")
	}
	if manifestVersion, _ := cmd.Manifest["version"].(string); manifestVersion != cmd.Version {
		return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, "manifest version does not match upload version")
	}

	pubKey, err := s.keys.Get(ctx, publisherID)
	if err != nil {
		if errors.Is(err, ErrNoSigningKey) {
			return Plugin{}, domain.Invalid(domain.CodePluginNoSigningKey, "no signing key registered — register one before uploading")
		}
		return Plugin{}, domain.Internal(err)
	}
	digest := sha256.Sum256(cmd.Package)
	if !ed25519.Verify(pubKey, digest[:], cmd.Signature) {
		return Plugin{}, domain.Invalid(domain.CodePluginSignatureInvalid, "package signature does not verify against the registered key")
	}

	// Automated gate (spec-20 §5.3): a manifest can *claim* its
	// tools/connectors/hooks entries exist — only actually resolving them
	// against the compiled module proves it, catching a broken package at
	// upload time instead of the first time an agent tries to call it.
	wasmBytes := cmd.Files[wasmFileName]
	funcNames, err := wasmEntryFuncNames(cmd.Manifest)
	if err != nil {
		return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, err.Error())
	}
	if len(funcNames) > 0 {
		if len(wasmBytes) == 0 {
			return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, "manifest declares tools/connectors/hooks entries but no plugin.wasm was provided")
		}
		if s.wasm != nil {
			wasmKey := cmd.PluginID + "@" + cmd.Version
			if err := s.wasm.ValidateEntries(ctx, wasmKey, wasmBytes, funcNames); err != nil {
				return Plugin{}, domain.Invalid(domain.CodePluginManifestInvalid, "wasm module failed automated validation: "+err.Error())
			}
		}
	}

	prefix := pluginOSSPrefix(cmd.PluginID, cmd.Version)
	for path, content := range cmd.Files {
		key := prefix + "/" + path
		if err := s.objectStore.Put(ctx, key, bytes.NewReader(content), contentTypeFor(path)); err != nil {
			return Plugin{}, domain.Internal(fmt.Errorf("upload %q: %w", path, err))
		}
	}

	owner := publisherID
	created, err := s.repo.CreateVersion(ctx, Plugin{
		PluginID: cmd.PluginID, Version: cmd.Version, Manifest: cmd.Manifest,
		OSSPrefix: prefix, PublisherID: &owner, Signature: encodeSignature(cmd.Signature),
		Visibility: VisibilityPrivate, ReviewStatus: ReviewPending, Status: StatusEnabled,
	})
	if err != nil {
		if errors.Is(err, ErrVersionDuplicate) {
			return Plugin{}, domain.Conflict(domain.CodePluginVersionDuplicate, "this plugin id + version already exists")
		}
		return Plugin{}, domain.Internal(err)
	}
	return created, nil
}

// GetVersion returns one specific version.
func (s *Service) GetVersion(ctx context.Context, pluginID, version string) (Plugin, error) {
	p, err := s.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Plugin{}, domain.NotFound(domain.CodePluginNotFound, "plugin version not found")
		}
		return Plugin{}, domain.Internal(err)
	}
	return p, nil
}

// GetLatestVersion returns the most recently published enabled version.
func (s *Service) GetLatestVersion(ctx context.Context, pluginID string) (Plugin, error) {
	p, err := s.repo.GetLatestVersion(ctx, pluginID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Plugin{}, domain.NotFound(domain.CodePluginNotFound, "plugin not found")
		}
		return Plugin{}, domain.Internal(err)
	}
	return p, nil
}

// ListVersions returns every version of one plugin, newest first.
func (s *Service) ListVersions(ctx context.Context, pluginID string) ([]Plugin, error) {
	rows, err := s.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Plugin{}
	}
	return rows, nil
}

// ListMarket returns the 组件广场"插件" tab listing: one row per plugin_id,
// its latest public+passed version.
func (s *Service) ListMarket(ctx context.Context) ([]Plugin, error) {
	rows, err := s.repo.ListMarket(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Plugin{}
	}
	return rows, nil
}

// ListMine returns every version this publisher has uploaded.
func (s *Service) ListMine(ctx context.Context, publisherID int64) ([]Plugin, error) {
	rows, err := s.repo.ListByPublisher(ctx, publisherID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Plugin{}
	}
	return rows, nil
}

// SetVisibility flips a version between private and public. Only its own
// publisher may do this — flipping to public is what enters the version
// into the moderation queue (ListPendingReview).
func (s *Service) SetVisibility(ctx context.Context, callerID, id int64, visibility Visibility) (Plugin, error) {
	current, err := s.getByIDOwned(ctx, callerID, id)
	if err != nil {
		return Plugin{}, err
	}
	updated, err := s.repo.SetVisibility(ctx, current.ID, visibility)
	if err != nil {
		return Plugin{}, domain.Internal(err)
	}
	return updated, nil
}

// getByIDOwned fetches a version by scanning the publisher's own uploads —
// there is no GetByID in Repository (every other read is keyed by
// plugin_id+version), so ownership + id lookup goes through ListByPublisher.
func (s *Service) getByIDOwned(ctx context.Context, callerID, id int64) (Plugin, error) {
	mine, err := s.repo.ListByPublisher(ctx, callerID)
	if err != nil {
		return Plugin{}, domain.Internal(err)
	}
	for _, p := range mine {
		if p.ID == id {
			return p, nil
		}
	}
	return Plugin{}, domain.NotFound(domain.CodePluginNotFound, "plugin version not found")
}

// ListPendingReview returns the moderation queue. Admin-gated, reusing the
// same AdminDirectory the 运营中心 report queue checks against.
func (s *Service) ListPendingReview(ctx context.Context, callerID int64) ([]Plugin, error) {
	if err := s.requireAdmin(ctx, callerID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListPendingReview(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Plugin{}
	}
	return rows, nil
}

// Review approves or rejects a pending public version.
func (s *Service) Review(ctx context.Context, callerID, id int64, approve bool) (Plugin, error) {
	if err := s.requireAdmin(ctx, callerID); err != nil {
		return Plugin{}, err
	}
	status := ReviewRejected
	if approve {
		status = ReviewPassed
	}
	updated, err := s.repo.SetReviewStatus(ctx, id, status)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Plugin{}, domain.NotFound(domain.CodePluginNotFound, "plugin version not found")
		}
		return Plugin{}, domain.Internal(err)
	}
	return updated, nil
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	ok, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		return domain.Internal(err)
	}
	if !ok {
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	return nil
}

// InstallCommand installs a plugin into the caller's own account. A nil
// Version installs the latest.
type InstallCommand struct {
	PluginID string
	Version  *string
	Config   Config
	Granted  []string
}

// Install resolves the target version, checks it is actually installable
// (public+passed, or the caller's own private upload for self-testing),
// encrypts config credentials, and creates the installation row.
func (s *Service) Install(ctx context.Context, ownerID int64, cmd InstallCommand) (Installation, error) {
	var target Plugin
	var err error
	if cmd.Version != nil {
		target, err = s.repo.GetVersion(ctx, cmd.PluginID, *cmd.Version)
	} else {
		target, err = s.repo.GetLatestVersion(ctx, cmd.PluginID)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Installation{}, domain.NotFound(domain.CodePluginNotFound, "plugin version not found")
		}
		return Installation{}, domain.Internal(err)
	}

	isOwnPrivate := target.PublisherID != nil && *target.PublisherID == ownerID
	isMarketReady := target.Visibility == VisibilityPublic && target.ReviewStatus == ReviewPassed
	if !isOwnPrivate && !isMarketReady {
		return Installation{}, domain.Forbidden(domain.CodeForbidden, "this plugin version is not available to install")
	}

	encrypted, err := s.encryptConfig(cmd.Config)
	if err != nil {
		return Installation{}, domain.Invalid(domain.CodeValidationFailed, "invalid config")
	}

	created, err := s.repo.CreateInstallation(ctx, Installation{
		OwnerUserID: ownerID, PluginID: cmd.PluginID, Version: target.Version,
		Resolution: ResolutionPinned, Config: encrypted, Granted: cmd.Granted, Status: StatusEnabled,
	})
	if err != nil {
		if errors.Is(err, ErrInstallationExist) {
			return Installation{}, domain.Conflict(domain.CodePluginVersionDuplicate, "already installed — use update instead")
		}
		return Installation{}, domain.Internal(err)
	}
	created.Config = created.Config.Redact()
	return created, nil
}

// UpdateInstallationCommand patches an installation. Nil fields are left
// alone.
type UpdateInstallationCommand struct {
	Version    *string
	Resolution *Resolution
	Config     Config
	Granted    []string
}

// UpdateInstallation patches an installed plugin's pinned version,
// resolution mode, config, or granted permissions.
func (s *Service) UpdateInstallation(ctx context.Context, ownerID int64, pluginID string, cmd UpdateInstallationCommand) (Installation, error) {
	current, err := s.repo.GetInstallation(ctx, ownerID, pluginID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Installation{}, domain.NotFound(domain.CodePluginNotInstalled, "plugin not installed")
		}
		return Installation{}, domain.Internal(err)
	}

	if cmd.Version != nil {
		if _, err := s.repo.GetVersion(ctx, pluginID, *cmd.Version); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Installation{}, domain.NotFound(domain.CodePluginNotFound, "plugin version not found")
			}
			return Installation{}, domain.Internal(err)
		}
		current.Version = *cmd.Version
	}
	if cmd.Resolution != nil {
		current.Resolution = *cmd.Resolution
	}
	if cmd.Config != nil {
		encrypted, err := s.encryptConfig(cmd.Config)
		if err != nil {
			return Installation{}, domain.Invalid(domain.CodeValidationFailed, "invalid config")
		}
		current.Config = encrypted
	}
	if cmd.Granted != nil {
		current.Granted = cmd.Granted
	}

	updated, err := s.repo.UpdateInstallation(ctx, current)
	if err != nil {
		return Installation{}, domain.Internal(err)
	}
	updated.Config = updated.Config.Redact()
	return updated, nil
}

// Uninstall removes an installation.
func (s *Service) Uninstall(ctx context.Context, ownerID int64, pluginID string) error {
	if err := s.repo.DeleteInstallation(ctx, ownerID, pluginID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodePluginNotInstalled, "plugin not installed")
		}
		return domain.Internal(err)
	}
	return nil
}

// GetInstallation returns one installation, redacted.
func (s *Service) GetInstallation(ctx context.Context, ownerID int64, pluginID string) (Installation, error) {
	in, err := s.repo.GetInstallation(ctx, ownerID, pluginID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Installation{}, domain.NotFound(domain.CodePluginNotInstalled, "plugin not installed")
		}
		return Installation{}, domain.Internal(err)
	}
	in.Config = in.Config.Redact()
	return in, nil
}

// ListInstallations returns every plugin installed in the caller's account.
func (s *Service) ListInstallations(ctx context.Context, ownerID int64) ([]Installation, error) {
	rows, err := s.repo.ListInstallations(ctx, ownerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Installation{}
	}
	for i := range rows {
		rows[i].Config = rows[i].Config.Redact()
	}
	return rows, nil
}

// encryptConfig mirrors resource.Service's credential encryption: every
// IsCredentialKey field's string value is replaced by its ciphertext,
// everything else passes through.
func (s *Service) encryptConfig(config Config) (Config, error) {
	out := make(Config, len(config))
	for k, v := range config {
		if !IsCredentialKey(k) {
			out[k] = v
			continue
		}
		str, ok := v.(string)
		if !ok {
			return nil, domain.Invalid(domain.CodeValidationFailed, "credential field must be a string")
		}
		ciphertext, err := s.cipher.Encrypt(str)
		if err != nil {
			return nil, err
		}
		out[k] = ciphertext
	}
	return out, nil
}

// DecryptConfig reverses encryptConfig — used only to build a real plugin
// instance from stored config at run time.
func (s *Service) DecryptConfig(config Config) (Config, error) {
	out := make(Config, len(config))
	for k, v := range config {
		if !IsCredentialKey(k) {
			out[k] = v
			continue
		}
		str, ok := v.(string)
		if !ok {
			return nil, domain.Invalid(domain.CodeValidationFailed, "credential field must be a string")
		}
		plaintext, err := s.cipher.Decrypt(str)
		if err != nil {
			return nil, err
		}
		out[k] = plaintext
	}
	return out, nil
}

// pluginOSSPrefix is where one plugin version's files live in the bucket —
// one prefix per (plugin_id, version), matching the UNIQUE constraint on
// the plugins table: a version's contents never change after upload, so
// its prefix never needs to either.
func pluginOSSPrefix(pluginID, version string) string {
	return fmt.Sprintf("plugins/%s/%s", pluginID, version)
}

// contentTypeFor guesses a content type from a file's extension, mirroring
// resource.contentTypeFor — good enough for OSS metadata, not sniffed from
// content and doesn't need to be exact.
func contentTypeFor(path string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	if strings.HasSuffix(path, ".md") {
		return "text/markdown"
	}
	return "application/octet-stream"
}

func encodeSignature(sig []byte) string {
	const hextable = "0123456789abcdef"
	out := make([]byte, len(sig)*2)
	for i, b := range sig {
		out[i*2] = hextable[b>>4]
		out[i*2+1] = hextable[b&0x0f]
	}
	return string(out)
}

// wasmEntryFuncNames collects the exported function name half of every
// tools/connectors/hooks entry in a manifest — the set the automated gate
// must confirm actually exists in the compiled module. renderers are
// deliberately excluded: they run in a frontend iframe (spec-20 §3.2), not
// the WASM sandbox, so their entry is a file path, not an exported
// function.
func wasmEntryFuncNames(manifest map[string]any) ([]string, error) {
	extensions, _ := manifest["extensions"].(map[string]any)
	var names []string
	for _, point := range []string{"tools", "connectors", "hooks"} {
		items, _ := extensions[point].([]any)
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			entry, _ := m["entry"].(string)
			if entry == "" {
				continue
			}
			_, fn, ok := strings.Cut(entry, "#")
			if !ok || fn == "" {
				return nil, fmt.Errorf("extensions.%s entry %q: expected \"<file>#<function>\"", point, entry)
			}
			names = append(names, fn)
		}
	}
	return names, nil
}
