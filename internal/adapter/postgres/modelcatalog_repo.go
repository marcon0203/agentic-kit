package postgres

import (
	"context"
	"encoding/json"
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

func toDomainProvider(id int64, key, displayName string, icon, baseURL, template pgtype.Text, status int16, createdAt pgtype.Timestamptz, hasCredential bool) modelcatalog.Provider {
	return modelcatalog.Provider{
		ID: id, Key: key, DisplayName: displayName,
		Icon: icon.String, BaseURL: baseURL.String, Template: template.String,
		Status: status, CreatedAt: createdAt.Time, HasCredential: hasCredential,
	}
}

// paramsToDB 把参数取值编码成 JSONB。nil 和空 map 都落成 '{}'——列是 NOT
// NULL，存 NULL 只会在读侧多一个分支。
func paramsToDB(params map[string]any) []byte {
	if len(params) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func paramsFromDB(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func toDomainModel(row store.CatalogModel) modelcatalog.Model {
	return modelcatalog.Model{
		ID: row.ID, ProviderID: row.ProviderID, Model: row.Model, DisplayName: row.DisplayName,
		Description: row.Description, Modality: modelcatalog.Modality(row.Modality),
		Featured: row.Featured, Status: row.Status, CreatedAt: row.CreatedAt.Time,
		Params: paramsFromDB(row.Params),
	}
}

func (r *ModelCatalogRepository) CreateProvider(ctx context.Context, p modelcatalog.NewProvider) (modelcatalog.Provider, error) {
	row, err := r.q.CreateCatalogProvider(ctx, store.CreateCatalogProviderParams{
		ProviderKey: p.Key, DisplayName: p.DisplayName,
		Icon:       pgtype.Text{String: p.Icon, Valid: p.Icon != ""},
		BaseUrl:    pgtype.Text{String: p.BaseURL, Valid: p.BaseURL != ""},
		Template:   pgtype.Text{String: p.Template, Valid: p.Template != ""},
		Descriptor: p.Descriptor,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return modelcatalog.Provider{}, modelcatalog.ErrDuplicate
		}
		return modelcatalog.Provider{}, err
	}
	return toDomainProvider(row.ID, row.ProviderKey, row.DisplayName, row.Icon, row.BaseUrl, row.Template, row.Status, row.CreatedAt, row.HasCredential), nil
}

func (r *ModelCatalogRepository) ListProviders(ctx context.Context) ([]modelcatalog.Provider, error) {
	rows, err := r.q.ListCatalogProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.Provider, 0, len(rows))
	for _, row := range rows {
		p := toDomainProvider(row.ID, row.ProviderKey, row.DisplayName, row.Icon, row.BaseUrl, row.Template, row.Status, row.CreatedAt, row.HasCredential)
		p.Descriptor = row.Descriptor
		out = append(out, p)
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
	p := toDomainProvider(row.ID, row.ProviderKey, row.DisplayName, row.Icon, row.BaseUrl, row.Template, row.Status, row.CreatedAt, row.HasCredential)
	p.Descriptor = row.Descriptor
	return p, nil
}

func (r *ModelCatalogRepository) SetProviderStatus(ctx context.Context, id int64, status int16) error {
	return r.q.SetCatalogProviderStatus(ctx, store.SetCatalogProviderStatusParams{ID: id, Status: status})
}

// SetProviderCredential registers/updates the org-wide default credential
// for a provider. encryptedKey is a *string so a nil means "leave the
// stored key untouched" (admin only changed base_url in the edit dialog,
// left the api_key field blank because it's already set) — see
// SetCatalogProviderCredential's COALESCE.
func (r *ModelCatalogRepository) SetProviderCredential(ctx context.Context, id int64, encryptedKey *string, baseURL string) error {
	var keyParam pgtype.Text
	if encryptedKey != nil {
		keyParam = pgtype.Text{String: *encryptedKey, Valid: true}
	}
	return r.q.SetCatalogProviderCredential(ctx, store.SetCatalogProviderCredentialParams{
		ID:           id,
		BaseUrl:      pgtype.Text{String: baseURL, Valid: baseURL != ""},
		EncryptedKey: keyParam,
	})
}

func (r *ModelCatalogRepository) DeleteProvider(ctx context.Context, id int64) error {
	return r.q.DeleteCatalogProvider(ctx, id)
}

func (r *ModelCatalogRepository) CreateModel(ctx context.Context, in modelcatalog.NewModel) (modelcatalog.Model, error) {
	row, err := r.q.CreateCatalogModel(ctx, store.CreateCatalogModelParams{
		ProviderID: in.ProviderID, Model: in.Model, DisplayName: in.DisplayName,
		Description: in.Description, Modality: string(in.Modality), Featured: in.Featured,
		Params: paramsToDB(in.Params),
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

func (r *ModelCatalogRepository) GetModel(ctx context.Context, id int64) (modelcatalog.Model, error) {
	row, err := r.q.GetCatalogModel(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return modelcatalog.Model{}, modelcatalog.ErrNotFound
		}
		return modelcatalog.Model{}, err
	}
	return toDomainModel(row), nil
}

func (r *ModelCatalogRepository) SetModelStatus(ctx context.Context, id int64, status int16) error {
	return r.q.SetCatalogModelStatus(ctx, store.SetCatalogModelStatusParams{ID: id, Status: status})
}

func (r *ModelCatalogRepository) UpdateModelParams(ctx context.Context, id int64, params map[string]any) error {
	return r.q.UpdateCatalogModelParams(ctx, store.UpdateCatalogModelParamsParams{ID: id, Params: paramsToDB(params)})
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

// ListChannelDescriptors 返回全部启用中的提供商的渠道描述符快照，供
// modelgateway 重建渠道注册表。停用的不出现——停用就该立刻调不通。
func (r *ModelCatalogRepository) ListChannelDescriptors(ctx context.Context) ([]modelcatalog.ChannelDescriptor, error) {
	rows, err := r.q.ListEnabledChannelDescriptors(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.ChannelDescriptor, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcatalog.ChannelDescriptor{Key: row.ProviderKey, Descriptor: row.Descriptor})
	}
	return out, nil
}

// ListChannelModelParams 返回启用中提供商下每个模型的请求参数取值，和
// ListChannelDescriptors 在同一次注册表重建里使用。
func (r *ModelCatalogRepository) ListChannelModelParams(ctx context.Context) ([]modelcatalog.ChannelModelParams, error) {
	rows, err := r.q.ListCatalogModelParams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]modelcatalog.ChannelModelParams, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcatalog.ChannelModelParams{
			ProviderKey: row.ProviderKey, Model: row.Model, Params: paramsFromDB(row.Params),
		})
	}
	return out, nil
}
