package api

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// PostgresAPIKeyLookup implements APIKeyLookup against the api_keys table
// (migrations/0008_api_keys.up.sql).
type PostgresAPIKeyLookup struct {
	Queries *store.Queries
}

func NewPostgresAPIKeyLookup(q *store.Queries) *PostgresAPIKeyLookup {
	return &PostgresAPIKeyLookup{Queries: q}
}

func (s *PostgresAPIKeyLookup) GetUserIDByKeyHash(ctx context.Context, hash string) (int64, bool, error) {
	row, err := s.Queries.GetAPIKeyByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.OwnerUserID, true, nil
}

func (s *PostgresAPIKeyLookup) TouchLastUsed(ctx context.Context, hash string) error {
	row, err := s.Queries.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return err
	}
	return s.Queries.TouchAPIKeyLastUsed(ctx, row.ID)
}
