package agent

import (
	"context"
	"errors"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// ErrDuplicateVersion is the port contract for "this ref+version already
// exists". Repository implementations must translate their storage-specific
// signal (a Postgres 23505 unique violation, say) into this sentinel — that
// translation is exactly what keeps pgconn out of the domain.
var ErrDuplicateVersion = errors.New("agent: ref/version already exists")

// ErrVersionLocked is the port contract for "storage refused the delete
// because a version is snapshot-locked by a subscriber". The service checks
// for subscribers up front, but the DB's immutable trigger (migration 0006)
// is the authority if that check races a concurrent subscribe.
var ErrVersionLocked = errors.New("agent: version is locked by a subscription")

// Repository is the persistence port for this context. It is declared here,
// by the consumer, not by the storage layer — internal/adapter/postgres
// implements it. Every method is scoped by ownerID: multi-tenant isolation
// is an invariant of the port, not something each caller remembers.
type Repository interface {
	// ListLatestByOwner returns one row per agent_ref (its newest version),
	// keyset-paginated by agent_ref. It over-fetches by one row so the
	// service can tell whether a further page exists.
	ListLatestByOwner(ctx context.Context, ownerID int64, q domain.PageQuery) ([]Agent, error)

	// GetByID returns one version by its numeric id — the entry point for
	// edit/PATCH/Delete, which route by id rather than the DSL's agent key.
	GetByID(ctx context.Context, ownerID, id int64) (Agent, error)

	// ListVersions returns every version of one ref, newest first. An empty
	// slice means the ref does not exist.
	ListVersions(ctx context.Context, ownerID int64, ref string) ([]Agent, error)

	// Create persists a new version, returning ErrDuplicateVersion if the
	// ref+version pair is taken.
	Create(ctx context.Context, a Agent) (Agent, error)

	// DeleteByRef removes every version of a ref, returning ErrVersionLocked
	// if storage refuses.
	DeleteByRef(ctx context.Context, ownerID int64, ref string) error

	// CountActiveSubscribedVersions counts versions of this ref that are
	// published AND currently subscribed by someone.
	CountActiveSubscribedVersions(ctx context.Context, ownerID int64, ref string) (int64, error)

	// FindReferencingBundles lists the owner's Bundles whose DSL names this
	// agent ref.
	FindReferencingBundles(ctx context.Context, ownerID int64, ref string) ([]BundleRef, error)
}

// ResourceCatalog resolves a capability reference against the owner's
// resources. The Agent context only asks "is this ref usable?"; that a tool
// ref may resolve against three different tables is a storage-shape detail
// the adapter owns.
type ResourceCatalog interface {
	ToolStatus(ctx context.Context, ownerID int64, ref string) (RefStatus, error)
	SkillStatus(ctx context.Context, ownerID int64, ref string) (RefStatus, error)
}

// DefinitionValidator checks a definition against the Agent DSL JSON Schema.
// Declared as a port so the domain depends on the *rule* ("definitions are
// schema-valid") rather than on a particular schema library.
type DefinitionValidator interface {
	Validate(def map[string]any) ([]domain.FieldError, error)
}
