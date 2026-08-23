package marketplace

import (
	"context"
	"errors"
)

// Port-level sentinels. Adapters translate their storage-specific signals
// into these, which is what keeps pgx.ErrNoRows and pgconn error codes out
// of the service.
var (
	// ErrNotFound means the row does not exist. Distinct from a domain
	// error: the service decides whether a missing row is a 404, a 409, or
	// simply an absent optional value.
	ErrNotFound = errors.New("marketplace: not found")

	// ErrDuplicate means a uniqueness constraint rejected the write.
	ErrDuplicate = errors.New("marketplace: already exists")
)

// ListingRepository persists listings.
type ListingRepository interface {
	Create(ctx context.Context, l Listing) (Listing, error)
	GetByID(ctx context.Context, id int64) (Listing, error)

	// GetLatestPublishedByRef returns the newest still-distributed version
	// of a listing_ref — the square's stable cross-version identifier.
	GetLatestPublishedByRef(ctx context.Context, ref string) (Listing, error)
	GetByRefAndVersion(ctx context.Context, ref, version string) (Listing, error)

	// ListPublishedPage returns one keyset page of published listings of a
	// single kind, ordered by id. Only display-safe columns are selected —
	// the blackbox rule is enforced in the SQL, not by filtering afterwards.
	ListPublishedPage(ctx context.Context, kind Kind, afterID int64, limit int32) ([]Listing, error)

	ListVersionHistory(ctx context.Context, ref string) ([]VersionHistoryEntry, error)

	SetDistribution(ctx context.Context, id int64, d Distribution) error
	IncrementSubscriberCount(ctx context.Context, id int64) error
	DecrementSubscriberCount(ctx context.Context, id int64) error
}

// SubscriptionRepository persists subscriptions.
type SubscriptionRepository interface {
	Create(ctx context.Context, subscriberID, listingID int64) (Subscription, error)
	GetByIDForSubscriber(ctx context.Context, id, subscriberID int64) (Subscription, error)
	GetByListingAndSubscriber(ctx context.Context, listingID, subscriberID int64) (Subscription, error)

	// GetByRefForSubscriber finds a subscription to *any* version of a
	// listing_ref — used to reject a second subscribe to the same resource.
	GetByRefForSubscriber(ctx context.Context, ref string, subscriberID int64) (Subscription, error)

	ListForSubscriberPage(ctx context.Context, subscriberID, afterID int64, limit int32) ([]Subscription, error)
	Delete(ctx context.Context, id, subscriberID int64) error

	// Repoint moves a subscription onto another version's listing.
	Repoint(ctx context.Context, id, subscriberID, listingID int64) (Subscription, error)
}

// ResourceCatalog is this context's window onto the resources being
// published. Note what is absent: there is no way to read a `definition`.
// The blackbox rule is a property of this port's shape, not a check someone
// has to remember to run.
type ResourceCatalog interface {
	// ResolvePrivateID finds the author's own resource row for an exact
	// kind/ref/version, returning ErrNotFound if they don't own one.
	ResolvePrivateID(ctx context.Context, ownerID int64, kind Kind, ref, version string) (int64, error)

	// SetDisplayMeta attaches the publicly-showable metadata to a resource.
	SetDisplayMeta(ctx context.Context, kind Kind, resourceID int64, meta DisplayMeta) error

	// DisplayMetaForListing reads back the display metadata for a listing.
	DisplayMetaForListing(ctx context.Context, kind Kind, listingID int64) (DisplayMeta, error)

	// ConstraintsForListing returns the execution-constraints summary, or
	// nil for kinds that have none.
	ConstraintsForListing(ctx context.Context, kind Kind, listingID int64) (*ConstraintsSummary, error)

	// Freeze marks a resource version immutable. Called on subscribe and on
	// upgrade: it is the mechanism behind snapshot isolation, ensuring an
	// author can never edit what a subscriber already committed to.
	Freeze(ctx context.Context, kind Kind, resourceID int64) error

	// PublishedDependents lists still-published resources that depend on
	// this one, which is what blocks stopping distribution.
	PublishedDependents(ctx context.Context, ownerID int64, kind Kind, ref string) ([]Dependent, error)
}

// Dependent is a published resource that depends on another.
type Dependent struct {
	Kind    Kind
	Ref     string
	Version string
}

// DependencyIssue is one problem found while walking a publish candidate's
// transitive dependency closure.
type DependencyIssue struct {
	Field  string
	Reason string
	Cycle  bool
}

// DependencyValidator walks the full Bundle -> Agent -> Skill/MCP closure of
// a publish candidate and reports *every* problem at once, so an author
// fixes one list instead of rediscovering the next failure per attempt.
type DependencyValidator interface {
	Validate(ctx context.Context, ownerID int64, kind Kind, ref, version string) ([]DependencyIssue, error)
}

// UserDirectory resolves the author shown on a listing.
type UserDirectory interface {
	Lookup(ctx context.Context, userID int64) (Author, error)
}
