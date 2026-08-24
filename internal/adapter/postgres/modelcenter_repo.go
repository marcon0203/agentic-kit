package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ModelProviderRepository implements modelcenter.Repository.
type ModelProviderRepository struct{ q store.Querier }

func NewModelProviderRepository(q store.Querier) *ModelProviderRepository {
	return &ModelProviderRepository{q: q}
}

// ListForOwner reads through a query whose SELECT list excludes the
// credentials column outright, so there is no ciphertext in flight to
// leak by accident.
func (r *ModelProviderRepository) ListForOwner(ctx context.Context, ownerID int64) ([]modelcenter.Provider, error) {
	rows, err := r.q.ListModelProvidersForOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]modelcenter.Provider, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcenter.Provider{
			ID: row.ID, OwnerID: ownerID, Name: row.Provider, BaseURL: row.BaseUrl.String, Status: row.Status, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

func (r *ModelProviderRepository) Store(ctx context.Context, ownerID int64, provider, ciphertext, baseURL string) (modelcenter.Provider, error) {
	row, err := r.q.CreateModelProvider(ctx, store.CreateModelProviderParams{
		OwnerUserID: ownerID, Provider: provider, Credentials: []byte(ciphertext),
		BaseUrl: pgtype.Text{String: baseURL, Valid: baseURL != ""},
	})
	if err != nil {
		return modelcenter.Provider{}, err
	}
	return modelcenter.Provider{
		ID: row.ID, OwnerID: ownerID, Name: row.Provider, BaseURL: row.BaseUrl.String, Status: row.Status, CreatedAt: row.CreatedAt.Time,
	}, nil
}

// ── Usage ────────────────────────────────────────────────────────────

// UsageRepository implements modelcenter.UsageReader. Every query is
// filtered on triggered_by, so a report can only ever describe the caller.
type UsageRepository struct{ q store.Querier }

func NewUsageRepository(q store.Querier) *UsageRepository { return &UsageRepository{q: q} }

func (r *UsageRepository) Summary(ctx context.Context, userID int64, since time.Time) (int64, float64, int32, error) {
	row, err := r.q.GetUsageSummaryForUser(ctx, store.GetUsageSummaryForUserParams{
		TriggeredBy: userID, CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return row.TotalTokens, numericFloat(row.TotalCostUsd), int32(row.RunCount), nil
}

func (r *UsageRepository) BreakdownByBundle(ctx context.Context, userID int64, since time.Time) ([]modelcenter.UsageBucket, error) {
	rows, err := r.q.GetUsageBreakdownByBundleForUser(ctx, store.GetUsageBreakdownByBundleForUserParams{
		TriggeredBy: userID, CreatedAt: timestamptz(since),
	})
	if err != nil {
		return nil, err
	}
	out := make([]modelcenter.UsageBucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcenter.UsageBucket{
			Key: row.Key, Tokens: row.Tokens, CostUSD: numericFloat(row.CostUsd), RunCount: int32(row.RunCount),
		})
	}
	return out, nil
}

func (r *UsageRepository) BreakdownByDay(ctx context.Context, userID int64, since time.Time) ([]modelcenter.UsageBucket, error) {
	rows, err := r.q.GetUsageBreakdownByDayForUser(ctx, store.GetUsageBreakdownByDayForUserParams{
		TriggeredBy: userID, CreatedAt: timestamptz(since),
	})
	if err != nil {
		return nil, err
	}
	out := make([]modelcenter.UsageBucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelcenter.UsageBucket{
			Key: row.Key, Tokens: row.Tokens, CostUSD: numericFloat(row.CostUsd), RunCount: int32(row.RunCount),
		})
	}
	return out, nil
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Valid: true, Time: t}
}
