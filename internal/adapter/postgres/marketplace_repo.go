package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// translateNotFound folds pgx's no-rows signal into the port's sentinel, so
// the marketplace service can reason about "absent" without importing pgx.
func translateNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return marketplace.ErrNotFound
	}
	return err
}

// ── Listings ─────────────────────────────────────────────────────────

type ListingRepository struct{ q store.Querier }

func NewListingRepository(q store.Querier) *ListingRepository { return &ListingRepository{q: q} }

var _ marketplace.ListingRepository = (*ListingRepository)(nil)

func toDomainListing(row store.MarketplaceListing) marketplace.Listing {
	return marketplace.Listing{
		ID:              row.ID,
		AuthorID:        row.AuthorUserID,
		Kind:            marketplace.Kind(row.ResourceType),
		ResourceID:      row.ResourceID,
		Ref:             row.ListingRef,
		Version:         row.Version,
		Visibility:      row.Visibility,
		Changelog:       row.Changelog.String,
		Distribution:    marketplace.Distribution(row.Distribution),
		SubscriberCount: row.SubscriberCount,
		RunCount:        row.RunCount,
		PublishedAt:     row.PublishedAt.Time,
	}
}

func (r *ListingRepository) Create(ctx context.Context, l marketplace.Listing) (marketplace.Listing, error) {
	changelog := pgtype.Text{}
	if l.Changelog != "" {
		changelog = pgtype.Text{String: l.Changelog, Valid: true}
	}
	row, err := r.q.CreateListing(ctx, store.CreateListingParams{
		AuthorUserID: l.AuthorID,
		ResourceType: string(l.Kind),
		ResourceID:   l.ResourceID,
		ListingRef:   l.Ref,
		Version:      l.Version,
		Changelog:    changelog,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return marketplace.Listing{}, marketplace.ErrDuplicate
		}
		return marketplace.Listing{}, err
	}
	return toDomainListing(row), nil
}

func (r *ListingRepository) GetByID(ctx context.Context, id int64) (marketplace.Listing, error) {
	row, err := r.q.GetListingByID(ctx, id)
	if err != nil {
		return marketplace.Listing{}, translateNotFound(err)
	}
	return toDomainListing(row), nil
}

func (r *ListingRepository) GetLatestPublishedByRef(ctx context.Context, ref string) (marketplace.Listing, error) {
	row, err := r.q.GetListingByListingRefLatestPublished(ctx, ref)
	if err != nil {
		return marketplace.Listing{}, translateNotFound(err)
	}
	return toDomainListing(row), nil
}

func (r *ListingRepository) GetByRefAndVersion(ctx context.Context, ref, version string) (marketplace.Listing, error) {
	row, err := r.q.GetListingByListingRefAndVersion(ctx, store.GetListingByListingRefAndVersionParams{
		ListingRef: ref, Version: version,
	})
	if err != nil {
		return marketplace.Listing{}, translateNotFound(err)
	}
	return toDomainListing(row), nil
}

// browseRow is the shared shape of the four per-kind browse queries. Each
// selects a dedicated, display-safe column list that does not include
// `definition` at all — the blackbox rule is enforced in SQL rather than by
// filtering after the fact.
type browseRow struct {
	ID              int64
	ListingRef      string
	ResourceType    string
	Version         string
	Visibility      string
	AuthorUserID    int64
	SubscriberCount int32
	RunCount        int64
	PublishedAt     pgtype.Timestamptz
	DisplayMeta     []byte
}

func (b browseRow) toDomain() (marketplace.Listing, error) {
	var meta marketplace.DisplayMeta
	if len(b.DisplayMeta) > 0 {
		if err := json.Unmarshal(b.DisplayMeta, &meta); err != nil {
			return marketplace.Listing{}, err
		}
	}
	return marketplace.Listing{
		ID:              b.ID,
		AuthorID:        b.AuthorUserID,
		Kind:            marketplace.Kind(b.ResourceType),
		Ref:             b.ListingRef,
		Version:         b.Version,
		Visibility:      b.Visibility,
		Distribution:    marketplace.DistributionPublished, // these queries only return distributed rows
		SubscriberCount: b.SubscriberCount,
		RunCount:        b.RunCount,
		DisplayMeta:     meta,
		PublishedAt:     b.PublishedAt.Time,
	}, nil
}

func (r *ListingRepository) ListPublishedPage(ctx context.Context, kind marketplace.Kind, afterID int64, limit int32) ([]marketplace.Listing, error) {
	var rows []browseRow

	switch kind {
	case marketplace.KindAgent:
		got, err := r.q.ListPublishedAgentListingsPage(ctx, store.ListPublishedAgentListingsPageParams{ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, x := range got {
			rows = append(rows, browseRow{x.ID, x.ListingRef, x.ResourceType, x.Version, x.Visibility, x.AuthorUserID, x.SubscriberCount, x.RunCount, x.PublishedAt, x.DisplayMeta})
		}
	case marketplace.KindBundle:
		got, err := r.q.ListPublishedBundleListingsPage(ctx, store.ListPublishedBundleListingsPageParams{ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, x := range got {
			rows = append(rows, browseRow{x.ID, x.ListingRef, x.ResourceType, x.Version, x.Visibility, x.AuthorUserID, x.SubscriberCount, x.RunCount, x.PublishedAt, x.DisplayMeta})
		}
	case marketplace.KindSkill:
		got, err := r.q.ListPublishedSkillListingsPage(ctx, store.ListPublishedSkillListingsPageParams{ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, x := range got {
			rows = append(rows, browseRow{x.ID, x.ListingRef, x.ResourceType, x.Version, x.Visibility, x.AuthorUserID, x.SubscriberCount, x.RunCount, x.PublishedAt, x.DisplayMeta})
		}
	case marketplace.KindMCP:
		got, err := r.q.ListPublishedMCPServerListingsPage(ctx, store.ListPublishedMCPServerListingsPageParams{ID: afterID, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, x := range got {
			rows = append(rows, browseRow{x.ID, x.ListingRef, x.ResourceType, x.Version, x.Visibility, x.AuthorUserID, x.SubscriberCount, x.RunCount, x.PublishedAt, x.DisplayMeta})
		}
	default:
		return nil, nil
	}

	out := make([]marketplace.Listing, 0, len(rows))
	for _, row := range rows {
		l, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (r *ListingRepository) ListVersionHistory(ctx context.Context, ref string) ([]marketplace.VersionHistoryEntry, error) {
	rows, err := r.q.ListListingVersionHistory(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := make([]marketplace.VersionHistoryEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, marketplace.VersionHistoryEntry{
			Version: row.Version, Changelog: row.Changelog.String, PublishedAt: row.PublishedAt.Time,
		})
	}
	return out, nil
}

func (r *ListingRepository) SetDistribution(ctx context.Context, id int64, d marketplace.Distribution) error {
	return r.q.SetListingDistribution(ctx, store.SetListingDistributionParams{ID: id, Distribution: int16(d)})
}

func (r *ListingRepository) IncrementSubscriberCount(ctx context.Context, id int64) error {
	return r.q.IncrementListingSubscriberCount(ctx, id)
}

func (r *ListingRepository) DecrementSubscriberCount(ctx context.Context, id int64) error {
	return r.q.DecrementListingSubscriberCount(ctx, id)
}

// ── Subscriptions ────────────────────────────────────────────────────

type SubscriptionRepository struct{ q store.Querier }

func NewSubscriptionRepository(q store.Querier) *SubscriptionRepository {
	return &SubscriptionRepository{q: q}
}

var _ marketplace.SubscriptionRepository = (*SubscriptionRepository)(nil)

func toDomainSubscription(row store.Subscription) marketplace.Subscription {
	return marketplace.Subscription{
		ID:           row.ID,
		SubscriberID: row.SubscriberID,
		ListingID:    row.ListingID,
		LocalAlias:   row.LocalAlias.String,
		CreatedAt:    row.CreatedAt.Time,
	}
}

func (r *SubscriptionRepository) Create(ctx context.Context, subscriberID, listingID int64) (marketplace.Subscription, error) {
	row, err := r.q.CreateSubscription(ctx, store.CreateSubscriptionParams{SubscriberID: subscriberID, ListingID: listingID})
	if err != nil {
		if isUniqueViolation(err) {
			return marketplace.Subscription{}, marketplace.ErrDuplicate
		}
		return marketplace.Subscription{}, err
	}
	return toDomainSubscription(row), nil
}

func (r *SubscriptionRepository) GetByIDForSubscriber(ctx context.Context, id, subscriberID int64) (marketplace.Subscription, error) {
	row, err := r.q.GetSubscriptionByIDForSubscriber(ctx, store.GetSubscriptionByIDForSubscriberParams{ID: id, SubscriberID: subscriberID})
	if err != nil {
		return marketplace.Subscription{}, translateNotFound(err)
	}
	return toDomainSubscription(row), nil
}

func (r *SubscriptionRepository) GetByListingAndSubscriber(ctx context.Context, listingID, subscriberID int64) (marketplace.Subscription, error) {
	row, err := r.q.GetSubscriptionByListingAndSubscriber(ctx, store.GetSubscriptionByListingAndSubscriberParams{
		SubscriberID: subscriberID, ListingID: listingID,
	})
	if err != nil {
		return marketplace.Subscription{}, translateNotFound(err)
	}
	return toDomainSubscription(row), nil
}

func (r *SubscriptionRepository) GetByRefForSubscriber(ctx context.Context, ref string, subscriberID int64) (marketplace.Subscription, error) {
	row, err := r.q.GetSubscriptionForSubscriberByListingRef(ctx, store.GetSubscriptionForSubscriberByListingRefParams{
		SubscriberID: subscriberID, ListingRef: ref,
	})
	if err != nil {
		return marketplace.Subscription{}, translateNotFound(err)
	}
	return toDomainSubscription(row), nil
}

func (r *SubscriptionRepository) ListForSubscriberPage(ctx context.Context, subscriberID, afterID int64, limit int32) ([]marketplace.Subscription, error) {
	rows, err := r.q.ListSubscriptionsForUserPage(ctx, store.ListSubscriptionsForUserPageParams{
		SubscriberID: subscriberID, ID: afterID, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]marketplace.Subscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainSubscription(row))
	}
	return out, nil
}

func (r *SubscriptionRepository) Delete(ctx context.Context, id, subscriberID int64) error {
	return r.q.DeleteSubscription(ctx, store.DeleteSubscriptionParams{ID: id, SubscriberID: subscriberID})
}

func (r *SubscriptionRepository) Repoint(ctx context.Context, id, subscriberID, listingID int64) (marketplace.Subscription, error) {
	row, err := r.q.UpdateSubscriptionListing(ctx, store.UpdateSubscriptionListingParams{
		ID: id, SubscriberID: subscriberID, ListingID: listingID,
	})
	if err != nil {
		return marketplace.Subscription{}, translateNotFound(err)
	}
	return toDomainSubscription(row), nil
}

// ── User directory ───────────────────────────────────────────────────

type UserDirectory struct{ q store.Querier }

func NewUserDirectory(q store.Querier) *UserDirectory { return &UserDirectory{q: q} }

var _ marketplace.UserDirectory = (*UserDirectory)(nil)

func (d *UserDirectory) Lookup(ctx context.Context, userID int64) (marketplace.Author, error) {
	row, err := d.q.GetUserByID(ctx, userID)
	if err != nil {
		return marketplace.Author{}, translateNotFound(err)
	}
	return marketplace.Author{
		ID: row.ID, Email: row.Email, DisplayName: row.DisplayName,
		IsAdmin: row.IsAdmin, CreatedAt: row.CreatedAt.Time,
	}, nil
}
