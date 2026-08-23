package api

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// UsageHandlers implements GET /usage/me per api/openapi.yaml and
// spec-09-model-gateway.md. Usage is always scoped to the requesting
// user's own `bundle_runs.triggered_by` — spec-09: "黑盒资源的用量算订阅
// 者的", a subscriber running someone else's published Bundle is billed
// against their own usage, not the author's.
type UsageHandlers struct {
	Queries store.Querier
	// now is overridable in tests so period-boundary math is deterministic.
	now func() time.Time
}

func NewUsageHandlers(q store.Querier) *UsageHandlers {
	return &UsageHandlers{Queries: q, now: time.Now}
}

type usageBreakdownDTO struct {
	Key      string  `json:"key"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
	RunCount int32   `json:"run_count"`
}

type usageSummaryDTO struct {
	Period       string              `json:"period"`
	TotalTokens  int64               `json:"total_tokens"`
	TotalCostUSD float64             `json:"total_cost_usd"`
	RunCount     int32               `json:"run_count"`
	Breakdown    []usageBreakdownDTO `json:"breakdown"`
}

func numericToFloat64(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// periodWindow returns the query cutoff (usage since this instant, UTC)
// and the human-readable period label the response echoes back.
func periodWindow(now time.Time, period string) (pgtype.Timestamptz, string) {
	now = now.UTC()
	var since time.Time
	var label string
	switch period {
	case "day":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		label = since.Format("2006-01-02")
	case "week":
		// Monday-anchored week, matching ISO week convention.
		offset := (int(now.Weekday()) + 6) % 7
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -offset)
		label = since.Format("2006-01-02")
	default: // "month"
		since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		label = since.Format("2006-01")
	}
	return pgtype.Timestamptz{Valid: true, Time: since}, label
}

// GetMyUsage handles GET /usage/me.
func (h *UsageHandlers) GetMyUsage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "month"
	}
	if period != "day" && period != "week" && period != "month" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid period")
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "bundle"
	}
	if groupBy != "bundle" && groupBy != "day" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid group_by")
		return
	}

	since, label := periodWindow(h.now(), period)

	summary, err := h.Queries.GetUsageSummaryForUser(r.Context(), store.GetUsageSummaryForUserParams{TriggeredBy: userID, CreatedAt: since})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	var breakdown []usageBreakdownDTO
	if groupBy == "bundle" {
		rows, err := h.Queries.GetUsageBreakdownByBundleForUser(r.Context(), store.GetUsageBreakdownByBundleForUserParams{TriggeredBy: userID, CreatedAt: since})
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		for _, row := range rows {
			breakdown = append(breakdown, usageBreakdownDTO{Key: row.Key, Tokens: row.Tokens, CostUSD: numericToFloat64(row.CostUsd), RunCount: int32(row.RunCount)})
		}
	} else {
		rows, err := h.Queries.GetUsageBreakdownByDayForUser(r.Context(), store.GetUsageBreakdownByDayForUserParams{TriggeredBy: userID, CreatedAt: since})
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		for _, row := range rows {
			breakdown = append(breakdown, usageBreakdownDTO{Key: row.Key, Tokens: row.Tokens, CostUSD: numericToFloat64(row.CostUsd), RunCount: int32(row.RunCount)})
		}
	}
	if breakdown == nil {
		breakdown = []usageBreakdownDTO{}
	}

	writeJSON(w, r, http.StatusOK, usageSummaryDTO{
		Period: label, TotalTokens: summary.TotalTokens, TotalCostUSD: numericToFloat64(summary.TotalCostUsd),
		RunCount: int32(summary.RunCount), Breakdown: breakdown,
	})
}
