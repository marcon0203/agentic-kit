package api

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// PostgresIdempotencyStore implements IdempotencyStore against the
// idempotency_keys table (migrations/0007_idempotency_keys.up.sql).
type PostgresIdempotencyStore struct {
	Queries *store.Queries
}

func NewPostgresIdempotencyStore(q *store.Queries) *PostgresIdempotencyStore {
	return &PostgresIdempotencyStore{Queries: q}
}

func (s *PostgresIdempotencyStore) Get(ctx context.Context, key string) (int, []byte, bool, error) {
	row, err := s.Queries.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	return int(row.StatusCode), row.ResponseBody, true, nil
}

func (s *PostgresIdempotencyStore) Put(ctx context.Context, key string, ownerUserID int64, statusCode int, body []byte, ttl time.Duration) error {
	return s.Queries.PutIdempotencyKey(ctx, store.PutIdempotencyKeyParams{
		Key:          key,
		OwnerUserID:  ownerUserID,
		StatusCode:   int32(statusCode),
		ResponseBody: body,
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true},
	})
}
