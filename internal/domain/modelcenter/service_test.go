package modelcenter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
)

// ── Fakes ────────────────────────────────────────────────────────────

type storedProvider struct {
	provider   string
	ciphertext string
}

type fakeRepo struct {
	byOwner map[int64][]modelcenter.Provider
	stored  []storedProvider
	nextID  int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byOwner: map[int64][]modelcenter.Provider{}, nextID: 1}
}

func (f *fakeRepo) ListForOwner(_ context.Context, ownerID int64) ([]modelcenter.Provider, error) {
	return f.byOwner[ownerID], nil
}

func (f *fakeRepo) Store(_ context.Context, ownerID int64, provider, ciphertext, baseURL string) (modelcenter.Provider, error) {
	f.stored = append(f.stored, storedProvider{provider: provider, ciphertext: ciphertext})
	p := modelcenter.Provider{ID: f.nextID, OwnerID: ownerID, Name: provider, BaseURL: baseURL, Status: 1}
	f.nextID++
	f.byOwner[ownerID] = append(f.byOwner[ownerID], p)
	return p, nil
}

// markingCipher makes the ciphertext obviously not the plaintext, so a
// test asserting "the key was encrypted before storage" is asserting
// something real without being about AES.
type markingCipher struct{ err error }

func (c markingCipher) Encrypt(plaintext string) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return "enc(" + plaintext + ")", nil
}

type checkCall struct {
	provider string
	apiKey   string
	baseURL  string
}

type fakeChecker struct {
	calls []checkCall
	err   error
}

func (f *fakeChecker) Check(_ context.Context, provider, apiKey, baseURL string) error {
	f.calls = append(f.calls, checkCall{provider: provider, apiKey: apiKey, baseURL: baseURL})
	return f.err
}

type fakeUsage struct {
	tokens   int64
	cost     float64
	runCount int32
	byBundle []modelcenter.UsageBucket
	byDay    []modelcenter.UsageBucket

	sinceSeen time.Time
	userSeen  int64
}

func (f *fakeUsage) Summary(_ context.Context, userID int64, since time.Time) (int64, float64, int32, error) {
	f.userSeen, f.sinceSeen = userID, since
	return f.tokens, f.cost, f.runCount, nil
}

func (f *fakeUsage) BreakdownByBundle(_ context.Context, _ int64, _ time.Time) ([]modelcenter.UsageBucket, error) {
	return f.byBundle, nil
}

func (f *fakeUsage) BreakdownByDay(_ context.Context, _ int64, _ time.Time) ([]modelcenter.UsageBucket, error) {
	return f.byDay, nil
}

type harness struct {
	svc     *modelcenter.Service
	repo    *fakeRepo
	checker *fakeChecker
	usage   *fakeUsage
}

func newHarness() *harness {
	h := &harness{repo: newFakeRepo(), checker: &fakeChecker{}, usage: &fakeUsage{}}
	h.svc = modelcenter.NewService(h.repo, markingCipher{}, h.checker, h.usage)
	return h
}

func mustDomainErr(t *testing.T, err error) *domain.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("expected a domain error, got %T: %v", err, err)
	}
	return de
}

// ── Registration ─────────────────────────────────────────────────────

func TestRegister_RequiresBothFields(t *testing.T) {
	h := newHarness()
	de := mustDomainErr(t, func() error { _, err := h.svc.Register(context.Background(), 1, "", "", ""); return err }())
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 2 {
		t.Fatalf("expected both fields reported at once, got %+v", de.Details)
	}
	if len(h.checker.calls) != 0 {
		t.Fatal("nothing should be sent to the provider before the request is even well-formed")
	}
}

// The key is proven to work before it is stored: a broken key is a
// mistake the user can fix now, while a stored broken key only shows up
// later as a failed run.
func TestRegister_ChecksConnectivityBeforeStoring(t *testing.T) {
	h := newHarness()
	h.checker.err = errors.New("provider rejected the credentials")

	de := mustDomainErr(t, func() error {
		_, err := h.svc.Register(context.Background(), 1, "anthropic", "sk-bogus", "")
		return err
	}())
	if de.Kind != domain.KindUnprocessable || de.Code != domain.CodeProviderCredsInvalid {
		t.Fatalf("expected 422/60002, got kind=%v code=%d", de.Kind, de.Code)
	}
	if len(h.repo.stored) != 0 {
		t.Fatal("a credential that does not authenticate must never be stored")
	}
}

// "This provider does not exist" is a different mistake from "this key was
// rejected", and gets the field-level 400 a typo deserves.
func TestRegister_UnknownProviderIsAFieldError(t *testing.T) {
	h := newHarness()
	h.checker.err = modelcenter.ErrUnknownProvider

	de := mustDomainErr(t, func() error {
		_, err := h.svc.Register(context.Background(), 1, "wormhole", "sk-x", "")
		return err
	}())
	if de.Kind != domain.KindInvalid || de.Code != domain.CodeValidationFailed {
		t.Fatalf("expected 400/10001, got kind=%v code=%d", de.Kind, de.Code)
	}
	if len(de.Details) != 1 || de.Details[0].Field != "provider" {
		t.Fatalf("expected a provider field error, got %+v", de.Details)
	}
}

func TestRegister_StoresCiphertextNotPlaintext(t *testing.T) {
	h := newHarness()

	created, err := h.svc.Register(context.Background(), 42, "anthropic", "sk-ant-realkey", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(h.repo.stored) != 1 {
		t.Fatalf("expected exactly one stored credential, got %+v", h.repo.stored)
	}
	if h.repo.stored[0].ciphertext == "sk-ant-realkey" {
		t.Fatal("the plaintext key reached storage")
	}
	if h.repo.stored[0].ciphertext != "enc(sk-ant-realkey)" {
		t.Fatalf("unexpected ciphertext: %q", h.repo.stored[0].ciphertext)
	}
	if created.Name != "anthropic" || created.OwnerID != 42 {
		t.Fatalf("unexpected provider: %+v", created)
	}
}

// "custom" has no documented endpoint of its own — the caller must supply
// one, or registration fails before ever reaching the connectivity check.
func TestRegister_CustomProviderRequiresBaseURL(t *testing.T) {
	h := newHarness()
	de := mustDomainErr(t, func() error {
		_, err := h.svc.Register(context.Background(), 1, "custom", "sk-x", "")
		return err
	}())
	if de.Code != domain.CodeValidationFailed {
		t.Fatalf("expected 10001, got %d", de.Code)
	}
	if len(de.Details) != 1 || de.Details[0].Field != "base_url" {
		t.Fatalf("expected a base_url field error, got %+v", de.Details)
	}
	if len(h.checker.calls) != 0 {
		t.Fatal("nothing should be sent to the provider before the request is even well-formed")
	}
}

func TestRegister_CustomProviderWithBaseURLSucceeds(t *testing.T) {
	h := newHarness()
	created, err := h.svc.Register(context.Background(), 1, "custom", "sk-x", "https://my-proxy.example.com/v1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if created.BaseURL != "https://my-proxy.example.com/v1" {
		t.Fatalf("unexpected base_url: %+v", created)
	}
	if len(h.checker.calls) != 1 || h.checker.calls[0].baseURL != "https://my-proxy.example.com/v1" {
		t.Fatalf("expected the base_url to reach the connectivity checker, got %+v", h.checker.calls)
	}
}

func TestRegister_EncryptionFailureDoesNotStore(t *testing.T) {
	h := newHarness()
	h.svc = modelcenter.NewService(h.repo, markingCipher{err: errors.New("key unavailable")}, h.checker, h.usage)

	de := mustDomainErr(t, func() error {
		_, err := h.svc.Register(context.Background(), 1, "anthropic", "sk-x", "")
		return err
	}())
	if de.Kind != domain.KindInternal {
		t.Fatalf("expected an internal error, got kind=%v", de.Kind)
	}
	if len(h.repo.stored) != 0 {
		t.Fatal("nothing may be stored when encryption failed")
	}
}

// modelcenter.Provider has no credential field at all, which is what makes
// "credentials never appear in a response" structural rather than a rule
// each read path has to remember.
func TestProvider_CarriesNoCredential(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), 1, "anthropic", "sk-ant-secret", ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	providers, err := h.svc.List(context.Background(), 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected one provider, got %d", len(providers))
	}
	// A compile-time fact stated as a runtime one: the only strings on the
	// type are the provider name and formatted ids.
	if providers[0].Name != "anthropic" {
		t.Fatalf("unexpected provider: %+v", providers[0])
	}
}

func TestList_ScopedToOwnerAndNeverNil(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), 1, "anthropic", "sk-a", ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	mine, err := h.svc.List(context.Background(), 1)
	if err != nil || len(mine) != 1 {
		t.Fatalf("owner should see their own provider: %v %+v", err, mine)
	}
	theirs, err := h.svc.List(context.Background(), 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(theirs) != 0 {
		t.Fatalf("another user must not see it: %+v", theirs)
	}
	if theirs == nil {
		t.Fatal("an empty list must serialise as [] rather than null")
	}
}

// ── Usage windows ────────────────────────────────────────────────────

func TestWindowFor_MonthStartsAtTheFirst(t *testing.T) {
	now := time.Date(2026, 8, 23, 15, 4, 5, 0, time.UTC)
	w := modelcenter.WindowFor(now, modelcenter.PeriodMonth)
	if !w.Since.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) || w.Label != "2026-08" {
		t.Fatalf("month window = %+v", w)
	}
}

func TestWindowFor_DayStartsAtMidnightUTC(t *testing.T) {
	now := time.Date(2026, 8, 23, 15, 4, 5, 0, time.UTC)
	w := modelcenter.WindowFor(now, modelcenter.PeriodDay)
	if !w.Since.Equal(time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)) || w.Label != "2026-08-23" {
		t.Fatalf("day window = %+v", w)
	}
}

// Go's Weekday has Sunday=0, so a Monday-anchored week needs the shift —
// without it a Sunday would start a week of its own.
func TestWindowFor_WeekIsMondayAnchored(t *testing.T) {
	cases := map[string]time.Time{
		"Monday": time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC),
		"Sunday": time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC),
	}
	wantMonday := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	for name, now := range cases {
		w := modelcenter.WindowFor(now, modelcenter.PeriodWeek)
		if !w.Since.Equal(wantMonday) {
			t.Fatalf("%s: week started at %v, want %v", name, w.Since, wantMonday)
		}
	}
}

func TestWindowFor_ConvertsToUTCBeforeSlicing(t *testing.T) {
	// 00:30 on the 24th in UTC+8 is still the 23rd in UTC, and the report
	// is a UTC one.
	tz := time.FixedZone("UTC+8", 8*3600)
	w := modelcenter.WindowFor(time.Date(2026, 8, 24, 0, 30, 0, 0, tz), modelcenter.PeriodDay)
	if w.Label != "2026-08-23" {
		t.Fatalf("label = %q, want the UTC day", w.Label)
	}
}

// ── Usage reports ────────────────────────────────────────────────────

func TestUsage_DefaultsToMonthAndBundleBreakdown(t *testing.T) {
	h := newHarness()
	h.usage.tokens, h.usage.cost, h.usage.runCount = 1200, 0.42, 3
	h.usage.byBundle = []modelcenter.UsageBucket{{Key: "content-pipeline", Tokens: 1200, CostUSD: 0.42, RunCount: 3}}
	h.usage.byDay = []modelcenter.UsageBucket{{Key: "2026-08-23"}}

	report, err := h.svc.Usage(context.Background(), 5, modelcenter.UsageQuery{})
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if len(report.Breakdown) != 1 || report.Breakdown[0].Key != "content-pipeline" {
		t.Fatalf("expected the bundle breakdown by default, got %+v", report.Breakdown)
	}
	if report.TotalTokens != 1200 || report.TotalCostUSD != 0.42 || report.RunCount != 3 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if len(report.Period) != len("2026-08") {
		t.Fatalf("expected a month label by default, got %q", report.Period)
	}
}

func TestUsage_GroupByDay(t *testing.T) {
	h := newHarness()
	h.usage.byDay = []modelcenter.UsageBucket{{Key: "2026-08-23", Tokens: 40}}

	report, err := h.svc.Usage(context.Background(), 5, modelcenter.UsageQuery{GroupBy: "day"})
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if len(report.Breakdown) != 1 || report.Breakdown[0].Key != "2026-08-23" {
		t.Fatalf("expected the day breakdown, got %+v", report.Breakdown)
	}
}

// A subscriber running someone else's Bundle pays for it themselves, so
// the report is always keyed on who triggered the run.
func TestUsage_IsAlwaysScopedToTheCaller(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Usage(context.Background(), 77, modelcenter.UsageQuery{}); err != nil {
		t.Fatalf("usage: %v", err)
	}
	if h.usage.userSeen != 77 {
		t.Fatalf("usage was read for user %d, want the caller", h.usage.userSeen)
	}
}

func TestUsage_RejectsUnknownPeriodAndGrouping(t *testing.T) {
	h := newHarness()
	if de := mustDomainErr(t, func() error {
		_, err := h.svc.Usage(context.Background(), 5, modelcenter.UsageQuery{Period: "fortnight"})
		return err
	}()); de.Code != domain.CodeValidationFailed {
		t.Fatalf("period: expected 10001, got %d", de.Code)
	}
	if de := mustDomainErr(t, func() error {
		_, err := h.svc.Usage(context.Background(), 5, modelcenter.UsageQuery{GroupBy: "agent"})
		return err
	}()); de.Code != domain.CodeValidationFailed {
		t.Fatalf("group_by: expected 10001, got %d", de.Code)
	}
}

func TestUsage_EmptyBreakdownIsAnEmptySlice(t *testing.T) {
	h := newHarness()
	report, err := h.svc.Usage(context.Background(), 5, modelcenter.UsageQuery{})
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if report.Breakdown == nil {
		t.Fatal("breakdown must serialise as [] rather than null")
	}
}
