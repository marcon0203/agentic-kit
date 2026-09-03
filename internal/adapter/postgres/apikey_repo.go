package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/apikey"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// APIKeyRepository implements apikey.Repository against the api_keys
// table — a thin wrapper since every query it needs already existed
// (migration 0008 was written for AuthMiddleware's lookup path; this is
// the write side that path never got).
type APIKeyRepository struct{ q store.Querier }

func NewAPIKeyRepository(q store.Querier) *APIKeyRepository { return &APIKeyRepository{q: q} }

var _ apikey.Repository = (*APIKeyRepository)(nil)

func (r *APIKeyRepository) Create(ctx context.Context, ownerID int64, name, keyHash string) (apikey.APIKey, error) {
	row, err := r.q.CreateAPIKey(ctx, store.CreateAPIKeyParams{OwnerUserID: ownerID, Name: name, KeyHash: keyHash})
	if err != nil {
		return apikey.APIKey{}, err
	}
	return apikey.APIKey{
		ID: row.ID, Name: row.Name,
		LastUsedAt: optionalTime(row.LastUsedAt), RevokedAt: optionalTime(row.RevokedAt),
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

func (r *APIKeyRepository) ListForOwner(ctx context.Context, ownerID int64) ([]apikey.APIKey, error) {
	rows, err := r.q.ListAPIKeysForOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]apikey.APIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, apikey.APIKey{
			ID: row.ID, Name: row.Name,
			LastUsedAt: optionalTime(row.LastUsedAt), RevokedAt: optionalTime(row.RevokedAt),
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

func (r *APIKeyRepository) Revoke(ctx context.Context, ownerID, keyID int64) error {
	n, err := r.q.RevokeAPIKey(ctx, store.RevokeAPIKeyParams{ID: keyID, OwnerUserID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apikey.ErrNotFound
	}
	return nil
}

func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
