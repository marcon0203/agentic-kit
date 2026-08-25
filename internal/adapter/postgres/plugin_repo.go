package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// PluginRepository implements plugin.Repository.
type PluginRepository struct{ q store.Querier }

func NewPluginRepository(q store.Querier) *PluginRepository { return &PluginRepository{q: q} }

var _ plugin.Repository = (*PluginRepository)(nil)

func toDomainPlugin(row store.Plugin) plugin.Plugin {
	var manifest map[string]any
	_ = json.Unmarshal(row.Manifest, &manifest)

	var publisherID *int64
	if row.PublisherID.Valid {
		id := row.PublisherID.Int64
		publisherID = &id
	}

	return plugin.Plugin{
		ID: row.ID, PluginID: row.PluginID, Version: row.Version, Manifest: manifest,
		OSSPrefix: row.OssPrefix, PublisherID: publisherID, Signature: row.Signature,
		Visibility: plugin.Visibility(row.Visibility), ReviewStatus: plugin.ReviewStatus(row.ReviewStatus),
		Status: plugin.Status(row.Status), CreatedAt: row.CreatedAt.Time,
	}
}

func toDomainInstallation(row store.PluginInstallation) plugin.Installation {
	var config plugin.Config
	_ = json.Unmarshal(row.Config, &config)
	var granted []string
	_ = json.Unmarshal(row.Granted, &granted)

	return plugin.Installation{
		ID: row.ID, OwnerUserID: row.OwnerUserID, PluginID: row.PluginID, Version: row.Version,
		Resolution: plugin.Resolution(row.Resolution), Config: config, Granted: granted,
		Status: plugin.Status(row.Status), CreatedAt: row.CreatedAt.Time,
	}
}

func (r *PluginRepository) CreateVersion(ctx context.Context, p plugin.Plugin) (plugin.Plugin, error) {
	manifestBytes, err := json.Marshal(p.Manifest)
	if err != nil {
		return plugin.Plugin{}, err
	}
	var publisherID pgtype.Int8
	if p.PublisherID != nil {
		publisherID = pgtype.Int8{Int64: *p.PublisherID, Valid: true}
	}

	row, err := r.q.CreatePlugin(ctx, store.CreatePluginParams{
		PluginID: p.PluginID, Version: p.Version, Manifest: manifestBytes, OssPrefix: p.OSSPrefix,
		PublisherID: publisherID, Signature: p.Signature,
		Visibility: string(p.Visibility), ReviewStatus: string(p.ReviewStatus),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return plugin.Plugin{}, plugin.ErrVersionDuplicate
		}
		return plugin.Plugin{}, err
	}
	return toDomainPlugin(row), nil
}

func (r *PluginRepository) GetVersion(ctx context.Context, pluginID, version string) (plugin.Plugin, error) {
	row, err := r.q.GetPluginVersion(ctx, store.GetPluginVersionParams{PluginID: pluginID, Version: version})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Plugin{}, plugin.ErrNotFound
		}
		return plugin.Plugin{}, err
	}
	return toDomainPlugin(row), nil
}

func (r *PluginRepository) GetLatestVersion(ctx context.Context, pluginID string) (plugin.Plugin, error) {
	row, err := r.q.GetLatestPluginVersion(ctx, pluginID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Plugin{}, plugin.ErrNotFound
		}
		return plugin.Plugin{}, err
	}
	return toDomainPlugin(row), nil
}

func (r *PluginRepository) ListVersions(ctx context.Context, pluginID string) ([]plugin.Plugin, error) {
	rows, err := r.q.ListPluginVersions(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Plugin, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPlugin(row))
	}
	return out, nil
}

func (r *PluginRepository) ListMarket(ctx context.Context) ([]plugin.Plugin, error) {
	rows, err := r.q.ListMarketPlugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Plugin, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPlugin(row))
	}
	return out, nil
}

func (r *PluginRepository) ListByPublisher(ctx context.Context, publisherID int64) ([]plugin.Plugin, error) {
	rows, err := r.q.ListPluginsByPublisher(ctx, pgtype.Int8{Int64: publisherID, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Plugin, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPlugin(row))
	}
	return out, nil
}

func (r *PluginRepository) SetVisibility(ctx context.Context, id int64, visibility plugin.Visibility) (plugin.Plugin, error) {
	row, err := r.q.SetPluginVisibility(ctx, store.SetPluginVisibilityParams{ID: id, Visibility: string(visibility)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Plugin{}, plugin.ErrNotFound
		}
		return plugin.Plugin{}, err
	}
	return toDomainPlugin(row), nil
}

func (r *PluginRepository) ListPendingReview(ctx context.Context) ([]plugin.Plugin, error) {
	rows, err := r.q.ListPendingReviewPlugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Plugin, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPlugin(row))
	}
	return out, nil
}

func (r *PluginRepository) SetReviewStatus(ctx context.Context, id int64, status plugin.ReviewStatus) (plugin.Plugin, error) {
	row, err := r.q.SetPluginReviewStatus(ctx, store.SetPluginReviewStatusParams{ID: id, ReviewStatus: string(status)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Plugin{}, plugin.ErrNotFound
		}
		return plugin.Plugin{}, err
	}
	return toDomainPlugin(row), nil
}

func (r *PluginRepository) CreateInstallation(ctx context.Context, in plugin.Installation) (plugin.Installation, error) {
	configBytes, err := json.Marshal(in.Config)
	if err != nil {
		return plugin.Installation{}, err
	}
	granted := in.Granted
	if granted == nil {
		granted = []string{}
	}
	grantedBytes, err := json.Marshal(granted)
	if err != nil {
		return plugin.Installation{}, err
	}

	row, err := r.q.CreatePluginInstallation(ctx, store.CreatePluginInstallationParams{
		OwnerUserID: in.OwnerUserID, PluginID: in.PluginID, Version: in.Version,
		Resolution: string(in.Resolution), Config: configBytes, Granted: grantedBytes,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return plugin.Installation{}, plugin.ErrInstallationExist
		}
		return plugin.Installation{}, err
	}
	return toDomainInstallation(row), nil
}

func (r *PluginRepository) GetInstallation(ctx context.Context, ownerUserID int64, pluginID string) (plugin.Installation, error) {
	row, err := r.q.GetPluginInstallation(ctx, store.GetPluginInstallationParams{OwnerUserID: ownerUserID, PluginID: pluginID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Installation{}, plugin.ErrNotFound
		}
		return plugin.Installation{}, err
	}
	return toDomainInstallation(row), nil
}

func (r *PluginRepository) ListInstallations(ctx context.Context, ownerUserID int64) ([]plugin.Installation, error) {
	rows, err := r.q.ListPluginInstallations(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Installation, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainInstallation(row))
	}
	return out, nil
}

func (r *PluginRepository) UpdateInstallation(ctx context.Context, in plugin.Installation) (plugin.Installation, error) {
	configBytes, err := json.Marshal(in.Config)
	if err != nil {
		return plugin.Installation{}, err
	}
	granted := in.Granted
	if granted == nil {
		granted = []string{}
	}
	grantedBytes, err := json.Marshal(granted)
	if err != nil {
		return plugin.Installation{}, err
	}

	row, err := r.q.UpdatePluginInstallation(ctx, store.UpdatePluginInstallationParams{
		OwnerUserID: in.OwnerUserID, PluginID: in.PluginID, Version: in.Version,
		Resolution: string(in.Resolution), Config: configBytes, Granted: grantedBytes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plugin.Installation{}, plugin.ErrNotFound
		}
		return plugin.Installation{}, err
	}
	return toDomainInstallation(row), nil
}

func (r *PluginRepository) DeleteInstallation(ctx context.Context, ownerUserID int64, pluginID string) error {
	rows, err := r.q.DeletePluginInstallation(ctx, store.DeletePluginInstallationParams{OwnerUserID: ownerUserID, PluginID: pluginID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return plugin.ErrNotFound
	}
	return nil
}

// PluginPublisherKeys implements plugin.PublisherKeys.
type PluginPublisherKeys struct{ q store.Querier }

func NewPluginPublisherKeys(q store.Querier) *PluginPublisherKeys { return &PluginPublisherKeys{q: q} }

var _ plugin.PublisherKeys = (*PluginPublisherKeys)(nil)

func (r *PluginPublisherKeys) Get(ctx context.Context, userID int64) ([]byte, error) {
	row, err := r.q.GetPublisherKey(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, plugin.ErrNoSigningKey
		}
		return nil, err
	}
	return row.PublicKey, nil
}

func (r *PluginPublisherKeys) Upsert(ctx context.Context, userID int64, publicKey []byte) error {
	_, err := r.q.UpsertPublisherKey(ctx, store.UpsertPublisherKeyParams{UserID: userID, PublicKey: publicKey})
	return err
}
