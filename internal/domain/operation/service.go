package operation

import (
	"context"
	"errors"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Port sentinels.
var ErrNotFound = errors.New("operation: not found")

// ReportRepository persists reports.
type ReportRepository interface {
	Create(ctx context.Context, listingID, reporterUserID int64, reason string) (Report, error)
	Get(ctx context.Context, id int64) (Report, error)
	// ListPending returns the open queue, newest first, over-fetching by
	// one so the service can tell whether another page exists.
	ListPending(ctx context.Context, beforeID int64, limit int) ([]Report, error)
	Resolve(ctx context.Context, id int64, resolution Resolution, resolvedBy int64) (Report, error)
}

// AuditReader reads the append-only log. There is no writer here for other
// contexts' entries: each context records its own, and this one only reads
// them back.
type AuditReader interface {
	ListForActor(ctx context.Context, actorUserID, beforeID int64, limit int) ([]AuditEntry, error)
}

// AuditWriter records this context's own decisions.
type AuditWriter interface {
	Record(ctx context.Context, actorUserID *int64, action, targetType, targetID string, detail map[string]any) error
}

// ListingDirectory resolves listings for the queue and for takedown.
type ListingDirectory interface {
	ByRef(ctx context.Context, ref string) (Listing, error)
	ByID(ctx context.Context, id int64) (Listing, error)
	// Stop marks a listing as taken down (distribution 3).
	Stop(ctx context.Context, id int64) error
}

// ResourceDisabler disables the thing a listing points at. Taking a
// listing down is not enough on its own: existing subscribers hold a
// snapshot and would keep running it, so the underlying resource is
// disabled too and their next run meets the ordinary 30002 check.
type ResourceDisabler interface {
	Disable(ctx context.Context, kind string, resourceID int64) error
}

// AdminDirectory answers whether a user may work the queue.
type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

// Service is the 运营中心 application service.
type Service struct {
	reports  ReportRepository
	audit    AuditReader
	auditLog AuditWriter
	listings ListingDirectory
	disabler ResourceDisabler
	admins   AdminDirectory
}

func NewService(reports ReportRepository, audit AuditReader, auditLog AuditWriter, listings ListingDirectory, disabler ResourceDisabler, admins AdminDirectory) *Service {
	return &Service{reports: reports, audit: audit, auditLog: auditLog, listings: listings, disabler: disabler, admins: admins}
}

// ListMyAuditLog returns the caller's own actions, newest first. It is
// always scoped to the caller: the audit log is a record of what *you*
// approved, not a window onto other people's decisions.
func (s *Service) ListMyAuditLog(ctx context.Context, userID int64, q domain.PageQuery) (domain.Page[AuditEntry], error) {
	limit := q.Normalize().Limit
	rows, err := s.audit.ListForActor(ctx, userID, descendingCursor(q.After), limit+1)
	if err != nil {
		return domain.Page[AuditEntry]{}, domain.Internal(err)
	}
	return newDescendingPage(rows, limit, func(e AuditEntry) int64 { return e.ID }), nil
}

// SubmitReport files a report against a listing. Any authenticated user
// may report; nothing here checks whether they are a subscriber, because
// the listings worth reporting are precisely the ones people have not
// committed to yet.
func (s *Service) SubmitReport(ctx context.Context, userID int64, listingRef, reason string) (ReportView, error) {
	if reason == "" {
		return ReportView{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").
			WithDetails(domain.FieldError{Field: "reason", Reason: "required"})
	}

	listing, err := s.listings.ByRef(ctx, listingRef)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ReportView{}, domain.NotFound(domain.CodeListingNotFound, "listing 不存在")
		}
		return ReportView{}, domain.Internal(err)
	}

	report, err := s.reports.Create(ctx, listing.ID, userID, reason)
	if err != nil {
		return ReportView{}, domain.Internal(err)
	}
	return ReportView{Report: report, Listing: listing}, nil
}

// ListPendingReports returns the open queue. Admin only.
func (s *Service) ListPendingReports(ctx context.Context, userID int64, q domain.PageQuery) (domain.Page[ReportView], error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return domain.Page[ReportView]{}, err
	}

	limit := q.Normalize().Limit
	rows, err := s.reports.ListPending(ctx, descendingCursor(q.After), limit+1)
	if err != nil {
		return domain.Page[ReportView]{}, domain.Internal(err)
	}

	views := make([]ReportView, 0, len(rows))
	for _, report := range rows {
		// A listing that has since vanished still leaves its report in the
		// queue: an unresolvable report is better shown bare than hidden.
		listing, err := s.listings.ByID(ctx, report.ListingID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return domain.Page[ReportView]{}, domain.Internal(err)
		}
		views = append(views, ReportView{Report: report, Listing: listing})
	}
	return newDescendingPage(views, limit, func(v ReportView) int64 { return v.Report.ID }), nil
}

// ResolveReport closes a report. Admin only.
//
// A takedown does two things, in this order: it disables the underlying
// resource, then stops distribution of the listing. Disabling first means
// that if the second step fails the listing is already unusable — the
// failure leaves the safer half done.
func (s *Service) ResolveReport(ctx context.Context, userID, reportID int64, action string) (ReportView, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return ReportView{}, err
	}

	resolution, ok := ParseResolution(action)
	if !ok {
		return ReportView{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").
			WithDetails(domain.FieldError{Field: "action", Reason: "must be one of dismiss, takedown"})
	}

	report, err := s.reports.Get(ctx, reportID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ReportView{}, domain.NotFound(domain.CodeReportNotFound, "举报不存在")
		}
		return ReportView{}, domain.Internal(err)
	}
	if !report.Pending() {
		return ReportView{}, domain.Conflict(domain.CodeReportAlreadyResolved, "该举报已处理")
	}

	listing, err := s.listings.ByID(ctx, report.ListingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ReportView{}, domain.NotFound(domain.CodeListingNotFound, "listing 不存在")
		}
		return ReportView{}, domain.Internal(err)
	}

	if resolution == ResolutionTakedown {
		if err := s.takeDown(ctx, userID, listing); err != nil {
			return ReportView{}, err
		}
	}

	resolved, err := s.reports.Resolve(ctx, reportID, resolution, userID)
	if err != nil {
		return ReportView{}, domain.Internal(err)
	}
	return ReportView{Report: resolved, Listing: listing}, nil
}

func (s *Service) takeDown(ctx context.Context, adminID int64, listing Listing) error {
	if err := s.disabler.Disable(ctx, listing.Kind, listing.ResourceID); err != nil {
		return domain.Internal(err)
	}
	if err := s.listings.Stop(ctx, listing.ID); err != nil {
		return domain.Internal(err)
	}
	// The audit entry records how many people this affected, which is the
	// number a takedown decision has to be answerable for afterwards.
	_ = s.auditLog.Record(ctx, &adminID, ActionTakedown, TargetTypeListing, itoa(listing.ID),
		map[string]any{"listing_ref": listing.Ref, "subscriber_count": listing.SubscriberCount})
	return nil
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	isAdmin, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		// A lookup failure is treated as "not an admin" rather than a 500:
		// this gate should fail closed.
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	if !isAdmin {
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	return nil
}
