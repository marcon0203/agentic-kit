package plugin

import (
	"context"
	"errors"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Port sentinels.
var (
	ErrNotFound          = errors.New("plugin: not found")
	ErrVersionDuplicate  = errors.New("plugin: version already exists")
	ErrInstallationExist = errors.New("plugin: already installed")
)

// Repository persists plugin versions and installations.
type Repository interface {
	CreateVersion(ctx context.Context, p Plugin) (Plugin, error)
	GetVersion(ctx context.Context, pluginID, version string) (Plugin, error)
	// GetLatestVersion returns the most recently created enabled version.
	GetLatestVersion(ctx context.Context, pluginID string) (Plugin, error)
	ListVersions(ctx context.Context, pluginID string) ([]Plugin, error)
	// ListMarket returns one row per plugin_id (its latest version), for
	// versions that are public and passed review.
	ListMarket(ctx context.Context) ([]Plugin, error)
	ListByPublisher(ctx context.Context, publisherID int64) ([]Plugin, error)
	SetVisibility(ctx context.Context, id int64, visibility Visibility) (Plugin, error)
	ListPendingReview(ctx context.Context) ([]Plugin, error)
	SetReviewStatus(ctx context.Context, id int64, status ReviewStatus) (Plugin, error)

	CreateInstallation(ctx context.Context, in Installation) (Installation, error)
	GetInstallation(ctx context.Context, ownerUserID int64, pluginID string) (Installation, error)
	ListInstallations(ctx context.Context, ownerUserID int64) ([]Installation, error)
	UpdateInstallation(ctx context.Context, in Installation) (Installation, error)
	DeleteInstallation(ctx context.Context, ownerUserID int64, pluginID string) error
}

// ManifestValidator validates a plugin.json document against
// schemas/plugin.schema.json. Matches internal/adapter/schema.Validator's
// signature exactly so that adapter is reused as-is — no new adapter code
// needed: schema.NewValidator(dslschema.NewPluginValidator()) satisfies
// this port directly.
type ManifestValidator interface {
	Validate(def map[string]any) ([]domain.FieldError, error)
}

// AdminDirectory answers whether a user may work the plugin review queue.
// Matches operation.AdminDirectory's shape so the same
// internal/adapter/postgres.AdminDirectory implementation is reused.
type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

// CredentialCipher encrypts and decrypts a single credential value, same
// shape as the resource context's port.
type CredentialCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// PublisherKeys stores each publisher's Ed25519 public verification key —
// the private key never touches the server; a publisher signs locally and
// this only ever verifies.
type PublisherKeys interface {
	Get(ctx context.Context, userID int64) ([]byte, error)
	Upsert(ctx context.Context, userID int64, publicKey []byte) error
}

// ErrNoSigningKey is returned by PublisherKeys.Get when the caller hasn't
// registered a key yet.
var ErrNoSigningKey = errors.New("plugin: no signing key registered")

// WasmValidator confirms an uploaded plugin's compiled WASM module
// actually exports every function its manifest's tools/connectors/hooks
// entries claim to have — spec-20 §5.3's automated gate, run once at
// upload time so a broken entry point is caught before the plugin can
// ever be installed, not discovered the first time an agent calls it.
// Implementations live in internal/adapter/extism.
type WasmValidator interface {
	ValidateEntries(ctx context.Context, wasmKey string, wasmBytes []byte, funcNames []string) error
}
