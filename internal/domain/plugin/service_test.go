package plugin_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeRepo struct {
	versions      map[string]plugin.Plugin // key: pluginID+"@"+version
	installations map[string]plugin.Installation
	nextID        int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{versions: map[string]plugin.Plugin{}, installations: map[string]plugin.Installation{}, nextID: 1}
}

func versionKey(pluginID, version string) string { return pluginID + "@" + version }
func installKey(ownerID int64, pluginID string) string {
	return pluginID + "#" + string(rune(ownerID))
}

func (f *fakeRepo) CreateVersion(_ context.Context, p plugin.Plugin) (plugin.Plugin, error) {
	key := versionKey(p.PluginID, p.Version)
	if _, exists := f.versions[key]; exists {
		return plugin.Plugin{}, plugin.ErrVersionDuplicate
	}
	p.ID = f.nextID
	f.nextID++
	f.versions[key] = p
	return p, nil
}

func (f *fakeRepo) GetVersion(_ context.Context, pluginID, version string) (plugin.Plugin, error) {
	p, ok := f.versions[versionKey(pluginID, version)]
	if !ok {
		return plugin.Plugin{}, plugin.ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) GetLatestVersion(_ context.Context, pluginID string) (plugin.Plugin, error) {
	var latest plugin.Plugin
	found := false
	for _, p := range f.versions {
		if p.PluginID == pluginID && (!found || p.ID > latest.ID) {
			latest, found = p, true
		}
	}
	if !found {
		return plugin.Plugin{}, plugin.ErrNotFound
	}
	return latest, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, pluginID string) ([]plugin.Plugin, error) {
	var out []plugin.Plugin
	for _, p := range f.versions {
		if p.PluginID == pluginID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListMarket(_ context.Context) ([]plugin.Plugin, error) {
	var out []plugin.Plugin
	for _, p := range f.versions {
		if p.Visibility == plugin.VisibilityPublic && p.ReviewStatus == plugin.ReviewPassed {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListByPublisher(_ context.Context, publisherID int64) ([]plugin.Plugin, error) {
	var out []plugin.Plugin
	for _, p := range f.versions {
		if p.PublisherID != nil && *p.PublisherID == publisherID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) SetVisibility(_ context.Context, id int64, visibility plugin.Visibility) (plugin.Plugin, error) {
	for key, p := range f.versions {
		if p.ID == id {
			p.Visibility = visibility
			f.versions[key] = p
			return p, nil
		}
	}
	return plugin.Plugin{}, plugin.ErrNotFound
}

func (f *fakeRepo) ListPendingReview(_ context.Context) ([]plugin.Plugin, error) {
	var out []plugin.Plugin
	for _, p := range f.versions {
		if p.Visibility == plugin.VisibilityPublic && p.ReviewStatus == plugin.ReviewPending {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) SetReviewStatus(_ context.Context, id int64, status plugin.ReviewStatus) (plugin.Plugin, error) {
	for key, p := range f.versions {
		if p.ID == id {
			p.ReviewStatus = status
			f.versions[key] = p
			return p, nil
		}
	}
	return plugin.Plugin{}, plugin.ErrNotFound
}

func (f *fakeRepo) CreateInstallation(_ context.Context, in plugin.Installation) (plugin.Installation, error) {
	key := installKey(in.OwnerUserID, in.PluginID)
	if _, exists := f.installations[key]; exists {
		return plugin.Installation{}, plugin.ErrInstallationExist
	}
	in.ID = f.nextID
	f.nextID++
	f.installations[key] = in
	return in, nil
}

func (f *fakeRepo) GetInstallation(_ context.Context, ownerUserID int64, pluginID string) (plugin.Installation, error) {
	in, ok := f.installations[installKey(ownerUserID, pluginID)]
	if !ok {
		return plugin.Installation{}, plugin.ErrNotFound
	}
	return in, nil
}

func (f *fakeRepo) ListInstallations(_ context.Context, ownerUserID int64) ([]plugin.Installation, error) {
	var out []plugin.Installation
	for _, in := range f.installations {
		if in.OwnerUserID == ownerUserID {
			out = append(out, in)
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateInstallation(_ context.Context, in plugin.Installation) (plugin.Installation, error) {
	key := installKey(in.OwnerUserID, in.PluginID)
	if _, ok := f.installations[key]; !ok {
		return plugin.Installation{}, plugin.ErrNotFound
	}
	f.installations[key] = in
	return in, nil
}

func (f *fakeRepo) DeleteInstallation(_ context.Context, ownerUserID int64, pluginID string) error {
	key := installKey(ownerUserID, pluginID)
	if _, ok := f.installations[key]; !ok {
		return plugin.ErrNotFound
	}
	delete(f.installations, key)
	return nil
}

type fakeKeys struct {
	byUser map[int64]ed25519.PublicKey
}

func newFakeKeys() *fakeKeys { return &fakeKeys{byUser: map[int64]ed25519.PublicKey{}} }

func (f *fakeKeys) Get(_ context.Context, userID int64) ([]byte, error) {
	k, ok := f.byUser[userID]
	if !ok {
		return nil, plugin.ErrNoSigningKey
	}
	return k, nil
}

func (f *fakeKeys) Upsert(_ context.Context, userID int64, publicKey []byte) error {
	f.byUser[userID] = publicKey
	return nil
}

type passValidator struct{}

func (passValidator) Validate(map[string]any) ([]domain.FieldError, error) { return nil, nil }

type failValidator struct{ errs []domain.FieldError }

func (v failValidator) Validate(map[string]any) ([]domain.FieldError, error) { return v.errs, nil }

type fakeAdmins struct{ admins map[int64]bool }

func (f *fakeAdmins) IsAdmin(_ context.Context, userID int64) (bool, error) { return f.admins[userID], nil }

type fakeCipher struct{}

func (fakeCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (fakeCipher) Decrypt(s string) (string, error) { return s[len("enc:"):], nil }

// ── Helpers ──────────────────────────────────────────────────────────

func signedUpload(t *testing.T, priv ed25519.PrivateKey, pluginID, version string) plugin.UploadCommand {
	t.Helper()
	pkg := []byte("fake .akp bytes for " + pluginID + "@" + version)
	digest := sha256.Sum256(pkg)
	sig := ed25519.Sign(priv, digest[:])
	return plugin.UploadCommand{
		PluginID: pluginID, Version: version,
		Manifest:  map[string]any{"id": pluginID, "version": version, "manifest_version": float64(1)},
		OSSPrefix: "plugins/" + pluginID + "/" + version,
		Package:   pkg, Signature: sig,
	}
}

// ── Tests ────────────────────────────────────────────────────────────

func TestUpload_RejectsUnregisteredPublisher(t *testing.T) {
	svc := plugin.NewService(newFakeRepo(), newFakeKeys(), passValidator{}, &fakeAdmins{}, fakeCipher{})
	pub, priv, _ := ed25519.GenerateKey(nil)
	_ = pub

	_, err := svc.Upload(context.Background(), 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodePluginNoSigningKey {
		t.Fatalf("expected CodePluginNoSigningKey, got %v", err)
	}
}

func TestUpload_RejectsBadSignature(t *testing.T) {
	keys := newFakeKeys()
	pub, _, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub
	_, otherPriv, _ := ed25519.GenerateKey(nil)

	svc := plugin.NewService(newFakeRepo(), keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	_, err := svc.Upload(context.Background(), 1, signedUpload(t, otherPriv, "acme.charts", "1.0.0"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodePluginSignatureInvalid {
		t.Fatalf("expected CodePluginSignatureInvalid, got %v", err)
	}
}

func TestUpload_RejectsManifestSchemaFailure(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	svc := plugin.NewService(newFakeRepo(), keys, failValidator{errs: []domain.FieldError{{Field: "id", Reason: "bad"}}}, &fakeAdmins{}, fakeCipher{})
	_, err := svc.Upload(context.Background(), 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodePluginManifestInvalid {
		t.Fatalf("expected CodePluginManifestInvalid, got %v", err)
	}
}

func TestUpload_SucceedsAsPrivatePending(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	svc := plugin.NewService(newFakeRepo(), keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	p, err := svc.Upload(context.Background(), 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if p.Visibility != plugin.VisibilityPrivate || p.ReviewStatus != plugin.ReviewPending {
		t.Fatalf("expected private/pending, got %v/%v", p.Visibility, p.ReviewStatus)
	}
}

func TestUpload_RejectsDuplicateVersion(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	svc := plugin.NewService(newFakeRepo(), keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	ctx := context.Background()
	if _, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0")); err != nil {
		t.Fatalf("first Upload: %v", err)
	}
	_, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodePluginVersionDuplicate {
		t.Fatalf("expected CodePluginVersionDuplicate, got %v", err)
	}
}

func TestInstall_OwnerCanInstallOwnPrivateUpload(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	svc := plugin.NewService(newFakeRepo(), keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	ctx := context.Background()
	if _, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	in, err := svc.Install(ctx, 1, plugin.InstallCommand{PluginID: "acme.charts"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if in.Version != "1.0.0" || in.Resolution != plugin.ResolutionPinned {
		t.Fatalf("unexpected installation: %+v", in)
	}
}

func TestInstall_RejectsUnpublishedPluginForOtherUsers(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	svc := plugin.NewService(newFakeRepo(), keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	ctx := context.Background()
	if _, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	_, err := svc.Install(ctx, 2, plugin.InstallCommand{PluginID: "acme.charts"})
	de, ok := domain.AsError(err)
	if !ok || de.Kind != domain.KindForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestInstall_EncryptsCredentialConfig(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	repo := newFakeRepo()
	svc := plugin.NewService(repo, keys, passValidator{}, &fakeAdmins{}, fakeCipher{})
	ctx := context.Background()
	if _, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	in, err := svc.Install(ctx, 1, plugin.InstallCommand{
		PluginID: "acme.charts",
		Config:   plugin.Config{"api_key": "shh", "display_name": "my charts"},
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Redacted on the returned value — a credential is gone, never masked.
	if _, present := in.Config["api_key"]; present {
		t.Fatalf("expected api_key redacted from Install response, got %+v", in.Config)
	}
	if in.Config["display_name"] != "my charts" {
		t.Fatalf("expected non-credential field preserved, got %+v", in.Config)
	}
	// Stored ciphertext should not be the plaintext.
	stored := repo.installations[installKey(1, "acme.charts")]
	if stored.Config["api_key"] == "shh" {
		t.Fatal("expected credential to be encrypted before storage")
	}
}

func TestReview_RequiresAdmin(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	repo := newFakeRepo()
	svc := plugin.NewService(repo, keys, passValidator{}, &fakeAdmins{admins: map[int64]bool{}}, fakeCipher{})
	ctx := context.Background()
	created, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := svc.SetVisibility(ctx, 1, created.ID, plugin.VisibilityPublic); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}

	_, err = svc.Review(ctx, 999, created.ID, true)
	de, ok := domain.AsError(err)
	if !ok || de.Kind != domain.KindForbidden {
		t.Fatalf("expected forbidden for non-admin reviewer, got %v", err)
	}
}

func TestReview_ApprovePutsVersionInMarket(t *testing.T) {
	keys := newFakeKeys()
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys.byUser[1] = pub

	repo := newFakeRepo()
	svc := plugin.NewService(repo, keys, passValidator{}, &fakeAdmins{admins: map[int64]bool{42: true}}, fakeCipher{})
	ctx := context.Background()
	created, err := svc.Upload(ctx, 1, signedUpload(t, priv, "acme.charts", "1.0.0"))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := svc.SetVisibility(ctx, 1, created.ID, plugin.VisibilityPublic); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}

	pending, err := svc.ListPendingReview(ctx, 42)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending review, got %v (err=%v)", pending, err)
	}

	if _, err := svc.Review(ctx, 42, created.ID, true); err != nil {
		t.Fatalf("Review: %v", err)
	}

	market, err := svc.ListMarket(ctx)
	if err != nil || len(market) != 1 {
		t.Fatalf("expected the approved version in the market listing, got %v (err=%v)", market, err)
	}

	// Now installable by a different user.
	if _, err := svc.Install(ctx, 2, plugin.InstallCommand{PluginID: "acme.charts"}); err != nil {
		t.Fatalf("Install by another user after approval: %v", err)
	}
}

func TestUninstall_NotInstalledReturnsNotFound(t *testing.T) {
	svc := plugin.NewService(newFakeRepo(), newFakeKeys(), passValidator{}, &fakeAdmins{}, fakeCipher{})
	err := svc.Uninstall(context.Background(), 1, "acme.charts")
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodePluginNotInstalled {
		t.Fatalf("expected CodePluginNotInstalled, got %v", err)
	}
}
