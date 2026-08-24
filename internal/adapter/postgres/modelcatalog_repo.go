package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcatalog"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ModelCatalogRepository implements modelcatalog.Repository.
type ModelCatalogRepository struct{ q store.Querier }

func NewModelCatalogRepository(q store.Querier) *ModelCatalogRepository {
	return &ModelCatalogRepository{q: q}
}

var _ modelcatalog.Repository = (*ModelCatalogRepository)(nil)

func toDomainProvider(row store.CatalogProvider) modelcatalog.Provider {
	return modelcatalog.Provider{
		ID: row.ID, Key: row.ProviderKey, DisplayName: row.DisplayName,
		Icon: row.Icon.String, BaseURL: row.BaseUrl.String,
		Status: row.Status, CreatedAt: row.CreatedAt.Time,
	}
}

func toDomainModel(row store.CatalogModel) modelcatalog.Model {
	return modelcatalog.Model{
		ID: row.ID, ProviderID: row.ProviderID, Model: row.Model, DisplayName: row.DisplayName,
		Description: row.Description, Modality: modelcatalog.Modality(row.Modality),
		Featured: row.Featured, Status: row.Status, CreatedAt: row.CreatedAt.Time,
	}
}

func (r *ModelCatalogRepository) CreateProvider(ctx context.Context, key, displayName, icon, baseURL string) (modelcatalog.Provider, error) {
	row, err := r.q.CreateCatalogProvider(ctx, store.CreateCatalogProviderParams{
		ProviderKey: key, DisplayName: displayName,
		Icon:    pgtype.Text{String: icon, Valid: icon != ""},
		BaseUrl: pgtype.Text{String: baseURL, Valid: baseURL != ""},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return modelcatalog.Provider{}, modelcatalog.ErrDuplicate
		}
		return modelcatalog.Provider{}, err
	}
	return toDomainProvider(row), nil
}

func (r *ModelCatalogRepository) ListProviders(ctx context.Context) ([]modelcatalog.Provider, error) {
	rows, err := r.q.ListCatalogProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.Provider, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainProvider(row))
	}
	return out, nil
}

func (r *ModelCatalogRepository) GetProvider(ctx context.Context, id int64) (modelcatalog.Provider, error) {
	row, err := r.q.GetCatalogProvider(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return modelcatalog.Provider{}, modelcatalog.ErrNotFound
		}
		return modelcatalog.Provider{}, err
	}
	return toDomainProvider(row), nil
}

func (r *ModelCatalogRepository) SetProviderStatus(ctx context.Context, id int64, status int16) error {
	return r.q.SetCatalogProviderStatus(ctx, store.SetCatalogProviderStatusParams{ID: id, Status: status})
}

func (r *ModelCatalogRepository) DeleteProvider(ctx context.Context, id int64) error {
	return r.q.DeleteCatalogProvider(ctx, id)
}

func (r *ModelCatalogRepository) CreateModel(ctx context.Context, providerID int64, model, displayName, description string, modality modelcatalog.Modality, featured bool) (modelcatalog.Model, error) {
	row, err := r.q.CreateCatalogModel(ctx, store.CreateCatalogModelParams{
		ProviderID: providerID, Model: model, DisplayName: displayName,
		Description: description, Modality: string(modality), Featured: featured,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return modelcatalog.Model{}, modelcatalog.ErrDuplicate
		}
		return modelcatalog.Model{}, err
	}
	return toDomainModel(row), nil
}

func (r *ModelCatalogRepository) ListModelsForProvider(ctx context.Context, providerID int64) ([]modelcatalog.Model, error) {
	rows, err := r.q.ListCatalogModelsForProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.Model, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainModel(row))
	}
	return out, nil
}

func (r *ModelCatalogRepository) SetModelStatus(ctx context.Context, id int64, status int16) error {
	return r.q.SetCatalogModelStatus(ctx, store.SetCatalogModelStatusParams{ID: id, Status: status})
}

func (r *ModelCatalogRepository) DeleteModel(ctx context.Context, id int64) error {
	return r.q.DeleteCatalogModel(ctx, id)
}

func (r *ModelCatalogRepository) ListPublic(ctx context.Context) ([]modelcatalog.CatalogEntry, error) {
	rows, err := r.q.ListCatalogModelsPublic(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.CatalogEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcatalog.CatalogEntry{
			Model: row.Model, DisplayName: row.DisplayName, Description: row.Description,
			Modality: modelcatalog.Modality(row.Modality), Featured: row.Featured,
			ProviderKey: row.ProviderKey, ProviderDisplayName: row.ProviderDisplayName,
			ProviderIcon: row.ProviderIcon.String,
		})
	}
	return out, nil
}
