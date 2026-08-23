package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
)

// The model-centre rules — check the credential before storing it, never
// store plaintext, whose usage a report describes, how a period is sliced
// — are tested against the service in internal/domain/modelcenter. What is
// left here is transport: the DTO shapes, and the fact that no response
// body can carry a credential.

type stubProviderRepo struct {
	providers []modelcenter.Provider
	stored    []string
	nextID    int64
}

func (s *stubProviderRepo) ListForOwner(_ context.Context, ownerID int64) ([]modelcenter.Provider, error) {
	var out []modelcenter.Provider
	for _, p := range s.providers {
		if p.OwnerID == ownerID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *stubProviderRepo) Store(_ context.Context, ownerID int64, provider, ciphertext string) (modelcenter.Provider, error) {
	s.stored = append(s.stored, ciphertext)
	s.nextID++
	p := modelcenter.Provider{ID: s.nextID, OwnerID: ownerID, Name: provider, Status: 1, CreatedAt: time.Now()}
	s.providers = append(s.providers, p)
	return p, nil
}

type stubCipher struct{}

func (stubCipher) Encrypt(plaintext string) (string, error) { return "enc(" + plaintext + ")", nil }

type stubChecker struct{ err error }

func (s stubChecker) Check(context.Context, string, string) error { return s.err }

type stubUsageReader struct {
	tokens   int64
	cost     float64
	runCount int32
	byBundle []modelcenter.UsageBucket
	byDay    []modelcenter.UsageBucket
}

func (s *stubUsageReader) Summary(context.Context, int64, time.Time) (int64, float64, int32, error) {
	return s.tokens, s.cost, s.runCount, nil
}

func (s *stubUsageReader) BreakdownByBundle(context.Context, int64, time.Time) ([]modelcenter.UsageBucket, error) {
	return s.byBundle, nil
}

func (s *stubUsageReader) BreakdownByDay(context.Context, int64, time.Time) ([]modelcenter.UsageBucket, error) {
	return s.byDay, nil
}

type modelCenterFixture struct {
	providers *ModelProviderHandlers
	usage     *UsageHandlers
	repo      *stubProviderRepo
	reader    *stubUsageReader
}

func newModelCenterFixture(checkErr error) *modelCenterFixture {
	repo := &stubProviderRepo{}
	reader := &stubUsageReader{}
	svc := modelcenter.NewService(repo, stubCipher{}, stubChecker{err: checkErr}, reader)
	return &modelCenterFixture{
		providers: NewModelProviderHandlers(svc), usage: NewUsageHandlers(svc),
		repo: repo, reader: reader,
	}
}

func doModelCenterRequest(h http.HandlerFunc, userID int64, method, path string, body any) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req = req.WithContext(WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestModelProviderCreate_ResponseShape(t *testing.T) {
	f := newModelCenterFixture(nil)

	w := doModelCenterRequest(f.providers.Create, 1, http.MethodPost, "/model-providers",
		createModelProviderRequest{Provider: "anthropic", APIKey: "sk-ant-secret"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var dto modelProviderDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if dto.Provider != "anthropic" || dto.Status != 1 || dto.ID == "" {
		t.Fatalf("unexpected provider: %+v", dto)
	}
}

// The response is checked for both the plaintext key and the ciphertext:
// echoing back either would be a leak, and the ciphertext is the easier
// one to return by accident.
func TestModelProviderCreate_ResponseCarriesNoCredential(t *testing.T) {
	f := newModelCenterFixture(nil)

	w := doModelCenterRequest(f.providers.Create, 1, http.MethodPost, "/model-providers",
		createModelProviderRequest{Provider: "anthropic", APIKey: "sk-ant-must-not-appear"})

	body := w.Body.String()
	if strings.Contains(body, "sk-ant-must-not-appear") {
		t.Fatalf("plaintext key leaked into the response: %s", body)
	}
	for _, ciphertext := range f.repo.stored {
		if strings.Contains(body, ciphertext) {
			t.Fatalf("ciphertext leaked into the response: %s", body)
		}
	}
}

func TestModelProviderCreate_InvalidCredentialsReturns422(t *testing.T) {
	f := newModelCenterFixture(errors.New("provider rejected the credentials"))

	w := doModelCenterRequest(f.providers.Create, 1, http.MethodPost, "/model-providers",
		createModelProviderRequest{Provider: "anthropic", APIKey: "sk-bad"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrProviderCredsInvalid) {
		t.Fatalf("body should carry ErrProviderCredsInvalid: %s", w.Body.String())
	}
}

func TestModelProviderCreate_UnknownProviderReturns400(t *testing.T) {
	f := newModelCenterFixture(modelcenter.ErrUnknownProvider)

	w := doModelCenterRequest(f.providers.Create, 1, http.MethodPost, "/model-providers",
		createModelProviderRequest{Provider: "wormhole", APIKey: "sk-x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Details) == 0 {
		t.Fatal("expected a field-level detail naming provider")
	}
}

func TestModelProviderCreate_MalformedBodyReturns400(t *testing.T) {
	f := newModelCenterFixture(nil)

	req := httptest.NewRequest(http.MethodPost, "/model-providers", strings.NewReader("{not json"))
	req = req.WithContext(WithUserID(req.Context(), 1))
	w := httptest.NewRecorder()
	f.providers.Create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestModelProviderList_ScopedToOwnerAndNeverNull(t *testing.T) {
	f := newModelCenterFixture(nil)
	doModelCenterRequest(f.providers.Create, 1, http.MethodPost, "/model-providers",
		createModelProviderRequest{Provider: "anthropic", APIKey: "sk-a"})

	w := doModelCenterRequest(f.providers.List, 1, http.MethodGet, "/model-providers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var items []modelProviderDTO
	_ = json.Unmarshal(dataBytes, &items)
	if len(items) != 1 || items[0].Provider != "anthropic" {
		t.Fatalf("unexpected list: %+v", items)
	}

	other := doModelCenterRequest(f.providers.List, 999, http.MethodGet, "/model-providers", nil)
	if strings.Contains(other.Body.String(), "anthropic") {
		t.Fatalf("another user saw this provider: %s", other.Body.String())
	}
	if strings.Contains(other.Body.String(), "null") {
		t.Fatalf("an empty list must serialise as []: %s", other.Body.String())
	}
}

func TestGetMyUsage_ResponseShape(t *testing.T) {
	f := newModelCenterFixture(nil)
	f.reader.tokens, f.reader.cost, f.reader.runCount = 1200, 0.42, 3
	f.reader.byBundle = []modelcenter.UsageBucket{{Key: "content-pipeline", Tokens: 1200, CostUSD: 0.42, RunCount: 3}}

	w := doModelCenterRequest(f.usage.GetMyUsage, 5, http.MethodGet, "/usage/me", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	dataBytes, _ := json.Marshal(env.Data)
	var dto usageSummaryDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if dto.TotalTokens != 1200 || dto.TotalCostUSD != 0.42 || dto.RunCount != 3 {
		t.Fatalf("unexpected totals: %+v", dto)
	}
	if len(dto.Breakdown) != 1 || dto.Breakdown[0].Key != "content-pipeline" {
		t.Fatalf("unexpected breakdown: %+v", dto.Breakdown)
	}
}

func TestGetMyUsage_EmptyBreakdownIsNotNull(t *testing.T) {
	f := newModelCenterFixture(nil)

	w := doModelCenterRequest(f.usage.GetMyUsage, 5, http.MethodGet, "/usage/me", nil)
	if !strings.Contains(w.Body.String(), `"breakdown":[]`) {
		t.Fatalf("breakdown must serialise as [] rather than null: %s", w.Body.String())
	}
}

func TestGetMyUsage_InvalidPeriodReturns400(t *testing.T) {
	f := newModelCenterFixture(nil)

	w := doModelCenterRequest(f.usage.GetMyUsage, 5, http.MethodGet, "/usage/me?period=fortnight", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestModelCenterHandlers_RequireAuthenticatedUser(t *testing.T) {
	f := newModelCenterFixture(nil)
	for name, handler := range map[string]http.HandlerFunc{
		"list": f.providers.List, "create": f.providers.Create, "usage": f.usage.GetMyUsage,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/model-providers", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a user: status = %d, want 401", name, w.Code)
		}
	}
}
