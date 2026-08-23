// Package operation is the 运营中心 bounded context (spec-18): the audit
// trail a user can read back over their own decisions, and the report
// queue an administrator works.
//
// It owns two pieces of state nothing else does — reports, and the
// append-only audit log — and deliberately owns nothing else. The run
// list and the cost report it displays are read from the contexts that
// already own them, so this context stays a moderator's workspace rather
// than a second home for other people's rules.
package operation

import (
	"encoding/json"
	"time"
)

// ReportStatus is where a report sits in the queue.
type ReportStatus string

const (
	ReportPending  ReportStatus = "pending"
	ReportResolved ReportStatus = "resolved"
)

// Resolution is how an administrator closed a report. There are exactly
// two outcomes: the report was unfounded, or the listing comes down.
type Resolution string

const (
	ResolutionDismiss  Resolution = "dismiss"
	ResolutionTakedown Resolution = "takedown"
)

func ParseResolution(s string) (Resolution, bool) {
	switch Resolution(s) {
	case ResolutionDismiss, ResolutionTakedown:
		return Resolution(s), true
	default:
		return "", false
	}
}

// Report is one complaint against a marketplace listing.
type Report struct {
	ID             int64
	ListingID      int64
	ReporterUserID int64
	Reason         string
	Status         ReportStatus
	Resolution     *Resolution
	CreatedAt      time.Time
	ResolvedAt     *time.Time
}

// Pending reports whether this report is still open. Only a pending report
// can be resolved (80002).
func (r Report) Pending() bool { return r.Status == ReportPending }

// Listing is the narrow read model this context needs of a marketplace
// listing: enough to name it in the queue and to take it down. It reads
// nothing about what the listing contains — a moderator sees the published
// surface, same as any other non-author.
type Listing struct {
	ID              int64
	Ref             string
	Kind            string
	ResourceID      int64
	SubscriberCount int32
}

// ReportView pairs a report with the listing it names, which is what the
// queue actually renders.
type ReportView struct {
	Report  Report
	Listing Listing
}

// AuditEntry is one append-only record of something a user did. Detail is
// carried as raw JSON: its shape is set by whichever context wrote the
// entry, and re-parsing it here would couple this context to all of them.
type AuditEntry struct {
	ID         int64
	Action     string
	TargetType string
	TargetID   string
	Detail     json.RawMessage
	CreatedAt  time.Time
}

// Audit actions this context writes itself.
const ActionTakedown = "moderation.takedown"

// TargetTypeListing is what a takedown entry points at.
const TargetTypeListing = "marketplace_listing"
