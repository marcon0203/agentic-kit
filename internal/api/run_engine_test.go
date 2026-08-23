package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// fakeRunEngineQuerier is an in-memory store.Querier covering exactly what
// RunEngine.ResolveBundle / CheckDependencies call.
type fakeRunEngineQuerier struct {
	store.Querier

	bundles  []store.Bundle
	agents   []store.Agent
	listings []store.MarketplaceListing
	subs     []store.Subscription

	toolStatus  map[string]int16
	skillStatus map[string]int16
}

func newFakeRunEngineQuerier() *fakeRunEngineQuerier {
	return &fakeRunEngineQuerier{
		toolStatus:  map[string]int16{},
		skillStatus: map[string]int16{},
	}
}

func (f *fakeRunEngineQuerier) GetBundleForOwner(_ context.Context, arg store.GetBundleForOwnerParams) (store.Bundle, error) {
	for _, b := range f.bundles {
		if b.OwnerUserID == arg.OwnerUserID && b.BundleRef == arg.BundleRef && b.Version == arg.Version {
			return b, nil
		}
	}
	return store.Bundle{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetBundleLatestByRef(_ context.Context, arg store.GetBundleLatestByRefParams) (store.Bundle, error) {
	for i := len(f.bundles) - 1; i >= 0; i-- {
		b := f.bundles[i]
		if b.OwnerUserID == arg.OwnerUserID && b.BundleRef == arg.BundleRef && b.Status == 1 {
			return b, nil
		}
	}
	return store.Bundle{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetBundleByID(_ context.Context, id int64) (store.Bundle, error) {
	for _, b := range f.bundles {
		if b.ID == id {
			return b, nil
		}
	}
	return store.Bundle{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetSubscriptionForSubscriberByListingRef(_ context.Context, arg store.GetSubscriptionForSubscriberByListingRefParams) (store.Subscription, error) {
	for _, l := range f.listings {
		if l.ListingRef != arg.ListingRef {
			continue
		}
		for _, s := range f.subs {
			if s.SubscriberID == arg.SubscriberID && s.ListingID == l.ID {
				return s, nil
			}
		}
	}
	return store.Subscription{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetListingByID(_ context.Context, id int64) (store.MarketplaceListing, error) {
	for _, l := range f.listings {
		if l.ID == id {
			return l, nil
		}
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetAgentForOwner(_ context.Context, arg store.GetAgentForOwnerParams) (store.Agent, error) {
	for _, a := range f.agents {
		if a.OwnerUserID == arg.OwnerUserID && a.AgentRef == arg.AgentRef && a.Version == arg.Version {
			return a, nil
		}
	}
	return store.Agent{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetAgentLatestByRef(_ context.Context, arg store.GetAgentLatestByRefParams) (store.Agent, error) {
	for i := len(f.agents) - 1; i >= 0; i-- {
		a := f.agents[i]
		if a.OwnerUserID == arg.OwnerUserID && a.AgentRef == arg.AgentRef {
			return a, nil
		}
	}
	return store.Agent{}, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetToolLatestStatusByRef(_ context.Context, arg store.GetToolLatestStatusByRefParams) (int16, error) {
	if s, ok := f.toolStatus["tool:"+arg.Ref]; ok {
		return s, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetMCPServerLatestStatusByRef(_ context.Context, arg store.GetMCPServerLatestStatusByRefParams) (int16, error) {
	if s, ok := f.toolStatus["mcp:"+arg.Ref]; ok {
		return s, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetKnowledgeBaseLatestStatusByRef(_ context.Context, arg store.GetKnowledgeBaseLatestStatusByRefParams) (int16, error) {
	if s, ok := f.toolStatus["kb:"+arg.Ref]; ok {
		return s, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeRunEngineQuerier) GetSkillLatestStatusByRef(_ context.Context, arg store.GetSkillLatestStatusByRefParams) (int16, error) {
	if s, ok := f.skillStatus[arg.Ref]; ok {
		return s, nil
	}
	return 0, pgx.ErrNoRows
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestRunEngine_ResolveBundle_Owned(t *testing.T) {
	f := newFakeRunEngineQuerier()
	f.bundles = append(f.bundles, store.Bundle{ID: 1, OwnerUserID: 10, BundleRef: "b1", Version: "v1", Status: 1})
	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))

	got, err := e.ResolveBundle(context.Background(), 10, "b1", "")
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if got.Row.ID != 1 || got.OwnerUserID != 10 || got.ViaListingID != nil {
		t.Fatalf("unexpected resolved bundle: %+v", got)
	}
}

func TestRunEngine_ResolveBundle_SubscribedListing(t *testing.T) {
	f := newFakeRunEngineQuerier()
	f.bundles = append(f.bundles, store.Bundle{ID: 5, OwnerUserID: 20, BundleRef: "b1", Version: "v1", Status: 1})
	f.listings = append(f.listings, store.MarketplaceListing{ID: 100, AuthorUserID: 20, ResourceType: "bundle", ResourceID: 5, ListingRef: "listing-1", Version: "v1"})
	f.subs = append(f.subs, store.Subscription{ID: 1, SubscriberID: 30, ListingID: 100})
	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))

	got, err := e.ResolveBundle(context.Background(), 30, "listing-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if got.Row.ID != 5 || got.OwnerUserID != 20 || got.ViaListingID == nil || *got.ViaListingID != 100 {
		t.Fatalf("unexpected resolved bundle: %+v", got)
	}
}

func TestRunEngine_ResolveBundle_Unsubscribed(t *testing.T) {
	f := newFakeRunEngineQuerier()
	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))

	_, err := e.ResolveBundle(context.Background(), 30, "listing-1", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Status != 403 || err.Code != ErrNotSubscribed {
		t.Fatalf("unexpected error: %+v", err)
	}
}

func TestRunEngine_CheckDependencies_MissingAgent(t *testing.T) {
	f := newFakeRunEngineQuerier()
	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))

	bundleDef := map[string]any{"agents": []any{map[string]any{"ref": "missing-agent"}}}
	err := e.CheckDependencies(context.Background(), 1, bundleDef, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Status != 422 || err.Code != ErrAgentVersionNotFound {
		t.Fatalf("unexpected error: %+v", err)
	}
	if want := "该 Bundle 引用的部分资源当前不可用"; err.Message != want {
		t.Fatalf("expected sanitized message %q, got %q", want, err.Message)
	}
}

func TestRunEngine_CheckDependencies_DisabledTool(t *testing.T) {
	f := newFakeRunEngineQuerier()
	f.agents = append(f.agents, store.Agent{
		ID: 1, OwnerUserID: 1, AgentRef: "a1", Version: "v1",
		Definition: mustJSON(t, map[string]any{"capabilities": map[string]any{"tools": []string{"t1"}}}),
	})
	f.toolStatus["tool:t1"] = 0 // disabled

	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))
	bundleDef := map[string]any{"agents": []any{map[string]any{"ref": "a1"}}}
	err := e.CheckDependencies(context.Background(), 1, bundleDef, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Status != 422 || err.Code != ErrResourceDisabled {
		t.Fatalf("unexpected error: %+v", err)
	}
	// Sanitized: must not name the resource ref.
	if want := "该 Bundle 引用的部分资源当前不可用"; err.Message != want {
		t.Fatalf("expected sanitized message %q, got %q", want, err.Message)
	}
}

func TestRunEngine_CheckDependencies_ProviderNotConfigured(t *testing.T) {
	f := newFakeRunEngineQuerier()
	f.agents = append(f.agents, store.Agent{
		ID: 1, OwnerUserID: 1, AgentRef: "a1", Version: "v1",
		Definition: mustJSON(t, map[string]any{"model": map[string]any{"provider": "openai"}}),
	})

	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))
	bundleDef := map[string]any{"agents": []any{map[string]any{"ref": "a1"}}}
	err := e.CheckDependencies(context.Background(), 1, bundleDef, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Status != 422 || err.Code != ErrProviderNotConfigured {
		t.Fatalf("unexpected error: %+v", err)
	}
}

func TestRunEngine_CheckDependencies_OK(t *testing.T) {
	f := newFakeRunEngineQuerier()
	f.agents = append(f.agents, store.Agent{
		ID: 1, OwnerUserID: 1, AgentRef: "a1", Version: "v1",
		Definition: mustJSON(t, map[string]any{
			"capabilities": map[string]any{"tools": []string{"t1"}, "skills": []string{"s1"}},
			"model":        map[string]any{"provider": "openai"},
		}),
	})
	f.toolStatus["tool:t1"] = 1
	f.skillStatus["s1"] = 1

	e := NewRunEngine(f, nil, newGateRegistry(), NewResourceRefChecker(f))
	bundleDef := map[string]any{"agents": []any{map[string]any{"ref": "a1"}}}
	err := e.CheckDependencies(context.Background(), 1, bundleDef, map[string]string{"openai": "sk-x"})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
}

func TestSanitizeRunError(t *testing.T) {
	if got := sanitizeRunError(errGatewayAllProvidersFailedStub()); got != "所有模型 Provider 均不可用" {
		t.Fatalf("unexpected sanitized message: %q", got)
	}
}

type stubErr struct{ msg string }

func (e stubErr) Error() string { return e.msg }

func errGatewayAllProvidersFailedStub() error {
	return stubErr{msg: "modelgateway: all providers failed: primary and fallback both errored"}
}
