package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

type fakeUsageQuerier struct {
	store.Querier
	summaries       map[int64]store.GetUsageSummaryForUserRow
	bundleBreakdown map[int64][]store.GetUsageBreakdownByBundleForUserRow
	dayBreakdown    map[int64][]store.GetUsageBreakdownByDayForUserRow
}

func newFakeUsageQuerier() *fakeUsageQuerier {
	return &fakeUsageQuerier{
		summaries:       map[int64]store.GetUsageSummaryForUserRow{},
		bundleBreakdown: map[int64][]store.GetUsageBreakdownByBundleForUserRow{},
		dayBreakdown:    map[int64][]store.GetUsageBreakdownByDayForUserRow{},
	}
}

func numeric(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(fmt.Sprintf("%.6f", f))
	return n
}

func (f *fakeUsageQuerier) GetUsageSummaryForUser(_ context.Context, arg store.GetUsageSummaryForUserParams) (store.GetUsageSummaryForUserRow, error) {
	return f.summaries[arg.TriggeredBy], nil
}

func (f *fakeUsageQuerier) GetUsageBreakdownByBundleForUser(_ context.Context, arg store.GetUsageBreakdownByBundleForUserParams) ([]store.GetUsageBreakdownByBundleForUserRow, error) {
	return f.bundleBreakdown[arg.TriggeredBy], nil
}

func (f *fakeUsageQuerier) GetUsageBreakdownByDayForUser(_ context.Context, arg store.GetUsageBreakdownByDayForUserParams) ([]store.GetUsageBreakdownByDayForUserRow, error) {
	return f.dayBreakdown[arg.TriggeredBy], nil
}

func TestGetMyUsage_DefaultsToMonthAndBundleBreakdown(t *testing.T) {
	f := newFakeUsageQuerier()
	f.summaries[1] = store.GetUsageSummaryForUserRow{TotalTokens: 1500, TotalCostUsd: numeric(1.25), RunCount: 3}
	f.bundleBreakdown[1] = []store.GetUsageBreakdownByBundleForUserRow{
		{Key: "web-app-builder", Tokens: 1000, CostUsd: numeric(0.8), RunCount: 2},
		{Key: "other-bundle", Tokens: 500, CostUsd: numeric(0.45), RunCount: 1},
	}
	h := &UsageHandlers{Queries: f, now: func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) }}

	rr := doModelProviderRequest(h.GetMyUsage, 1, http.MethodGet, "/usage/me", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data usageSummaryDTO `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Period != "2026-08" {
		t.Fatalf("expected period 2026-08, got %q", env.Data.Period)
	}
	if env.Data.TotalTokens != 1500 || env.Data.RunCount != 3 {
		t.Fatalf("unexpected summary: %+v", env.Data)
	}
	if len(env.Data.Breakdown) != 2 || env.Data.Breakdown[0].Key != "web-app-builder" {
		t.Fatalf("unexpected breakdown: %+v", env.Data.Breakdown)
	}
}

func TestGetMyUsage_GroupByDay(t *testing.T) {
	f := newFakeUsageQuerier()
	f.dayBreakdown[1] = []store.GetUsageBreakdownByDayForUserRow{
		{Key: "2026-08-23", Tokens: 200, CostUsd: numeric(0.1), RunCount: 1},
	}
	h := &UsageHandlers{Queries: f, now: time.Now}

	rr := doModelProviderRequest(h.GetMyUsage, 1, http.MethodGet, "/usage/me?group_by=day", nil)
	var env struct {
		Data usageSummaryDTO `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Breakdown) != 1 || env.Data.Breakdown[0].Key != "2026-08-23" {
		t.Fatalf("unexpected breakdown: %+v", env.Data.Breakdown)
	}
}

func TestGetMyUsage_InvalidPeriod_Returns400(t *testing.T) {
	f := newFakeUsageQuerier()
	h := &UsageHandlers{Queries: f, now: time.Now}

	rr := doModelProviderRequest(h.GetMyUsage, 1, http.MethodGet, "/usage/me?period=year", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetMyUsage_NoRuns_ReturnsEmptyNotNull(t *testing.T) {
	f := newFakeUsageQuerier()
	h := &UsageHandlers{Queries: f, now: time.Now}

	rr := doModelProviderRequest(h.GetMyUsage, 1, http.MethodGet, "/usage/me", nil)
	if !bytesContainsEmptyBreakdown(rr.Body.String()) {
		t.Fatalf("expected breakdown: [] not null, got %s", rr.Body.String())
	}
}

func bytesContainsEmptyBreakdown(body string) bool {
	var env struct {
		Data struct {
			Breakdown []usageBreakdownDTO `json:"breakdown"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return false
	}
	return env.Data.Breakdown != nil && len(env.Data.Breakdown) == 0
}

func TestPeriodWindow_WeekIsMondayAnchored(t *testing.T) {
	// 2026-08-23 is a Sunday.
	since, label := periodWindow(time.Date(2026, 8, 23, 15, 0, 0, 0, time.UTC), "week")
	if since.Time.Format("2006-01-02") != "2026-08-17" {
		t.Fatalf("expected week to start on Monday 2026-08-17, got %s", since.Time.Format("2006-01-02"))
	}
	if label != "2026-08-17" {
		t.Fatalf("unexpected label: %s", label)
	}
}
