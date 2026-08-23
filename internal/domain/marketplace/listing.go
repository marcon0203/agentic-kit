// Package marketplace is the 应用广场 bounded context: publishing a resource,
// browsing what others published, and subscribing to it under snapshot
// isolation. Its two non-negotiable rules (spec-08) are expressed here
// rather than in a handler:
//
//   - Blackbox distribution — a subscriber ever only sees display_meta and
//     constraints_summary, never `definition`. This context has no port that
//     can return a definition, so the rule holds by construction.
//   - Snapshot isolation — a subscription binds to one exact listing version
//     and the underlying resource is frozen; a new author version never
//     silently moves an existing subscriber.
package marketplace

import "time"

// Kind is the type of resource a listing distributes.
type Kind string

const (
	KindAgent  Kind = "agent"
	KindBundle Kind = "bundle"
	KindSkill  Kind = "skill"
	KindMCP    Kind = "mcp"
)

// AllKinds is browse's default fan-out, in a fixed order so paging is
// deterministic.
var AllKinds = []Kind{KindAgent, KindBundle, KindSkill, KindMCP}

// ParseKind validates a wire value.
func ParseKind(s string) (Kind, bool) {
	switch Kind(s) {
	case KindAgent, KindBundle, KindSkill, KindMCP:
		return Kind(s), true
	default:
		return "", false
	}
}

// Distribution is a listing's visibility in the square. Stopping
// distribution and being taken down are deliberately distinct: the first is
// the author's choice and leaves existing subscribers working, the second is
// a moderator action (spec-18) that also disables the underlying resource.
type Distribution int16

const (
	DistributionPublished Distribution = 1
	DistributionStopped   Distribution = 2
	DistributionTakenDown Distribution = 3
)

func (d Distribution) Published() bool { return d == DistributionPublished }

// DisplayMeta is the *publicly showable* metadata, stored separately from
// the resource definition. Under blackbox distribution this is the only
// descriptive content a subscriber receives.
type DisplayMeta map[string]any

func (m DisplayMeta) DisplayName() string { s, _ := m["display_name"].(string); return s }
func (m DisplayMeta) Description() string { s, _ := m["description"].(string); return s }

// Listing is one published version of a resource.
type Listing struct {
	ID              int64
	AuthorID        int64
	Kind            Kind
	ResourceID      int64
	Ref             string
	Version         string
	Visibility      string
	Changelog       string
	Distribution    Distribution
	SubscriberCount int32
	RunCount        int64
	DisplayMeta     DisplayMeta
	PublishedAt     time.Time
}

// Subscription binds a subscriber to one exact listing version.
type Subscription struct {
	ID           int64
	SubscriberID int64
	ListingID    int64
	LocalAlias   string
	CreatedAt    time.Time
}

// ConstraintsSummary is the blackbox rule's deliberate exception (spec-08):
// no persona and no orchestration graph ever leave, but a subscriber must be
// able to estimate run cost *before* subscribing. Skills and MCP servers have
// no constraints concept, so they have no summary.
type ConstraintsSummary struct {
	MaxToolCalls         *int32
	TimeoutSeconds       *int32
	EstimatedTokensRange string
}

// VersionHistoryEntry is one row of a listing_ref's published history.
// Changelog is author-written: the platform never auto-diffs versions,
// because a diff would leak blackbox content.
type VersionHistoryEntry struct {
	Version     string
	Changelog   string
	PublishedAt time.Time
}

// Author is the read model this context needs about a publisher. It is
// deliberately not the IAM user entity — the square only ever shows these
// fields.
type Author struct {
	ID          int64
	Email       string
	DisplayName string
	IsAdmin     bool
	CreatedAt   time.Time
}

// ListingView is a listing enriched with everything the square displays.
// Assembling it needs several ports, so the service builds it once and both
// detail and subscription responses reuse it.
type ListingView struct {
	Listing            Listing
	Author             Author
	ConstraintsSummary *ConstraintsSummary
	Versions           []VersionHistoryEntry
	Subscribed         bool
}

// SubscriptionView is a subscription plus the listing it points at and
// whether the author has since published something newer. LatestVersion
// being different from the subscribed version is exactly what the frontend
// renders as "有新版本" — and upgrading is always explicit.
type SubscriptionView struct {
	Subscription    Subscription
	Listing         ListingView
	LatestVersion   string
	LatestChangelog string
}

// HasUpgrade reports whether the author published a version newer than the
// one this subscription is pinned to.
func (v SubscriptionView) HasUpgrade() bool {
	return v.LatestVersion != "" && v.LatestVersion != v.Listing.Listing.Version
}
