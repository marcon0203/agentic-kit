package modelgateway

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// modelPrice is USD per 1,000 tokens, input and output priced separately
// since most providers charge output at a multiple of input. Each
// provider's pricing table lives on its ProviderDefinition in registry.go
// now, not in a second map here — a model absent from it prices at $0,
// cost tracking degrading gracefully rather than guessing (the model
// itself, from spec-09's usage table, is always visible even when cost
// isn't).
type modelPrice struct {
	InputPer1K  float64
	OutputPer1K float64
}

// EstimateCost computes USD cost for a completion from its token counts.
func EstimateCost(provider, model string, inputTokens, outputTokens int64) float64 {
	def, ok := providerByName(provider)
	if !ok {
		return 0
	}
	price, ok := def.Pricing[model]
	if !ok {
		return 0
	}
	return float64(inputTokens)/1000*price.InputPer1K + float64(outputTokens)/1000*price.OutputPer1K
}

// RecordUsage accumulates tokens and cost onto a bundle_runs row —
// spec-09's "累加写入 bundle_runs.total_tokens / cost_usd". It's a thin
// wrapper so the orchestrator (task 10/11), the Gateway's only intended
// caller of this function, doesn't need to know pgtype.Numeric's
// construction quirks.
func RecordUsage(ctx context.Context, q store.Querier, runID string, tokens int64, costUSD float64) error {
	var cost pgtype.Numeric
	if err := cost.Scan(fmt.Sprintf("%.6f", costUSD)); err != nil {
		return fmt.Errorf("modelgateway: encode cost: %w", err)
	}
	return q.UpdateBundleRunUsage(ctx, store.UpdateBundleRunUsageParams{
		ID: runID, TotalTokens: tokens, CostUsd: cost,
	})
}
