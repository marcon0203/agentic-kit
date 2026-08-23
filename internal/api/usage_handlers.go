package api

import (
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
)

// UsageHandlers is the HTTP transport for GET /usage/me (spec-09).
type UsageHandlers struct {
	svc *modelcenter.Service
}

func NewUsageHandlers(svc *modelcenter.Service) *UsageHandlers { return &UsageHandlers{svc: svc} }

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

// GetMyUsage handles GET /usage/me.
func (h *UsageHandlers) GetMyUsage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	report, err := h.svc.Usage(r.Context(), userID, modelcenter.UsageQuery{
		Period: r.URL.Query().Get("period"), GroupBy: r.URL.Query().Get("group_by"),
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	breakdown := make([]usageBreakdownDTO, 0, len(report.Breakdown))
	for _, b := range report.Breakdown {
		breakdown = append(breakdown, usageBreakdownDTO{Key: b.Key, Tokens: b.Tokens, CostUSD: b.CostUSD, RunCount: b.RunCount})
	}
	writeJSON(w, r, http.StatusOK, usageSummaryDTO{
		Period: report.Period, TotalTokens: report.TotalTokens, TotalCostUSD: report.TotalCostUSD,
		RunCount: report.RunCount, Breakdown: breakdown,
	})
}
