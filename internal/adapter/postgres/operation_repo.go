package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/domain/operation"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ReportRepository implements operation.ReportRepository.
type ReportRepository struct{ q store.Querier }

func NewReportRepository(q store.Querier) *ReportRepository { return &ReportRepository{q: q} }

func (r *ReportRepository) Create(ctx context.Context, listingID, reporterUserID int64, reason string) (operation.Report, error) {
	row, err := r.q.CreateReport(ctx, store.CreateReportParams{
		ListingID: listingID, ReporterUserID: reporterUserID, Reason: reason,
	})
	if err != nil {
		return operation.Report{}, err
	}
	return toDomainReport(row), nil
}

func (r *ReportRepository) Get(ctx context.Context, id int64) (operation.Report, error) {
	row, err := r.q.GetReportByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return operation.Report{}, operation.ErrNotFound
	}
	if err != nil {
		return operation.Report{}, err
	}
	return toDomainReport(row), nil
}

func (r *ReportRepository) ListPending(ctx context.Context, beforeID int64, limit int) ([]operation.Report, error) {
	rows, err := r.q.ListPendingReportsPage(ctx, store.ListPendingReportsPageParams{ID: beforeID, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]operation.Report, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainReport(row))
	}
	return out, nil
}

func (r *ReportRepository) Resolve(ctx context.Context, id int64, resolution operation.Resolution, resolvedBy int64) (operation.Report, error) {
	row, err := r.q.ResolveReport(ctx, store.ResolveReportParams{
		ID: id, Resolution: pgtype.Text{Valid: true, String: string(resolution)},
		ResolvedBy: pgtype.Int8{Valid: true, Int64: resolvedBy},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return operation.Report{}, operation.ErrNotFound
	}
	if err != nil {
		return operation.Report{}, err
	}
	return toDomainReport(row), nil
}

func toDomainReport(row store.Report) operation.Report {
	out := operation.Report{
		ID: row.ID, ListingID: row.ListingID, ReporterUserID: row.ReporterUserID,
		Reason: row.Reason, Status: operation.ReportStatus(row.Status), CreatedAt: row.CreatedAt.Time,
	}
	if row.Resolution.Valid {
		res := operation.Resolution(row.Resolution.String)
		out.Resolution = &res
	}
	if row.ResolvedAt.Valid {
		t := row.ResolvedAt.Time
		out.ResolvedAt = &t
	}
	return out
}

// ── Audit reading ────────────────────────────────────────────────────

// AuditLogReader implements operation.AuditReader.
type AuditLogReader struct{ q store.Querier }

func NewAuditLogReader(q store.Querier) *AuditLogReader { return &AuditLogReader{q: q} }

func (r *AuditLogReader) ListForActor(ctx context.Context, actorUserID, beforeID int64, limit int) ([]operation.AuditEntry, error) {
	rows, err := r.q.ListAuditLogsForActorPage(ctx, store.ListAuditLogsForActorPageParams{
		ActorUserID: pgtype.Int8{Valid: true, Int64: actorUserID}, ID: beforeID, Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]operation.AuditEntry, 0, len(rows))
	for _, row := range rows {
		detail := row.Detail
		if detail == nil {
			detail = []byte("null")
		}
		out = append(out, operation.AuditEntry{
			ID: row.ID, Action: row.Action, TargetType: row.TargetType,
			TargetID: row.TargetID, Detail: detail, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

// ── Listings ─────────────────────────────────────────────────────────

// ModerationListings implements operation.ListingDirectory.
type ModerationListings struct{ q store.Querier }

func NewModerationListings(q store.Querier) *ModerationListings { return &ModerationListings{q: q} }

func (d *ModerationListings) ByRef(ctx context.Context, ref string) (operation.Listing, error) {
	row, err := d.q.GetListingByListingRefLatestPublished(ctx, ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return operation.Listing{}, operation.ErrNotFound
	}
	if err != nil {
		return operation.Listing{}, err
	}
	return toModerationListing(row), nil
}

func (d *ModerationListings) ByID(ctx context.Context, id int64) (operation.Listing, error) {
	row, err := d.q.GetListingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return operation.Listing{}, operation.ErrNotFound
	}
	if err != nil {
		return operation.Listing{}, err
	}
	return toModerationListing(row), nil
}

// Stop sets distribution to 3 — taken down, which is distinct from the
// author's own "stopped" (2) so the two reasons a listing left the
// marketplace stay tellable apart.
func (d *ModerationListings) Stop(ctx context.Context, id int64) error {
	return d.q.SetListingDistribution(ctx, store.SetListingDistributionParams{
		ID: id, Distribution: int16(marketplace.DistributionTakenDown),
	})
}

func toModerationListing(row store.MarketplaceListing) operation.Listing {
	return operation.Listing{
		ID: row.ID, Ref: row.ListingRef, Kind: row.ResourceType,
		ResourceID: row.ResourceID, SubscriberCount: row.SubscriberCount,
	}
}

// ── Takedown ─────────────────────────────────────────────────────────

// ResourceDisabler implements operation.ResourceDisabler. Which table a
// listing's resource lives in is a storage fact, so the fan-out is here.
type ResourceDisabler struct{ q store.Querier }

func NewResourceDisabler(q store.Querier) *ResourceDisabler { return &ResourceDisabler{q: q} }

func (d *ResourceDisabler) Disable(ctx context.Context, kind string, resourceID int64) error {
	switch marketplace.Kind(kind) {
	case marketplace.KindAgent:
		return d.q.SetAgentStatusByID(ctx, store.SetAgentStatusByIDParams{ID: resourceID, Status: 0})
	case marketplace.KindBundle:
		return d.q.SetBundleStatusByID(ctx, store.SetBundleStatusByIDParams{ID: resourceID, Status: 0})
	case marketplace.KindSkill:
		return d.q.SetSkillStatusByID(ctx, store.SetSkillStatusByIDParams{ID: resourceID, Status: 0})
	case marketplace.KindMCP:
		return d.q.SetMCPServerStatusByID(ctx, store.SetMCPServerStatusByIDParams{ID: resourceID, Status: 0})
	default:
		return nil
	}
}

// ── Admins ───────────────────────────────────────────────────────────

// AdminDirectory implements operation.AdminDirectory.
type AdminDirectory struct{ q store.Querier }

func NewAdminDirectory(q store.Querier) *AdminDirectory { return &AdminDirectory{q: q} }

func (d *AdminDirectory) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	user, err := d.q.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user.IsAdmin, nil
}
