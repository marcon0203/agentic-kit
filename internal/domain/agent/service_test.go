package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/agent"
)

// The fakes below are the point of the port/adapter split: exercising every
// Agent business rule needs an in-memory map and no Postgres, no pgx error
// codes and no HTTP recorder.

type fakeRepo struct {
	agents      map[string][]agent.Agent // ref -> versions, newest first
	nextID      int64
	subscribed  map[string]int64
	referencing map[string][]agent.BundleRef
	deleteErr   error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		agents: map[string][]agent.Agent{}, nextID: 1,
		subscribed: map[string]int64{}, referencing: map[string][]agent.BundleRef{},
	}
}

func (f *fakeRepo) ListLatestByOwner(_ context.Context, _ int64, q domain.PageQuery) ([]agent.Agent, error) {
	var out []agent.Agent
	for ref, versions := range f.agents {
		if ref > q.After && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, _ int64, ref string) ([]agent.Agent, error) {
	return f.agents[ref], nil
}

func (f *fakeRepo) Create(_ context.Context, a agent.Agent) (agent.Agent, error) {
	for _, v := range f.agents[a.Ref] {
		if v.Version == a.Version {
			return agent.Agent{}, agent.ErrDuplicateVersion
		}
	}
	a.ID = f.nextID
	a.Status = agent.StatusEnabled
	f.nextID++
	f.agents[a.Ref] = append([]agent.Agent{a}, f.agents[a.Ref]...)
	return a, nil
}

func (f *fakeRepo) DeleteByRef(_ context.Context, _ int64, ref string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.agents, ref)
	return nil
}

func (f *fakeRepo) CountActiveSubscribedVersions(_ context.Context, _ int64, ref string) (int64, error) {
	return f.subscribed[ref], nil
}

func (f *fakeRepo) FindReferencingBundles(_ context.Context, _ int64, ref string) ([]agent.BundleRef, error) {
	return f.referencing[ref], nil
}

func (f *fakeRepo) GetByID(_ context.Context, _ int64, id int64) (agent.Agent, error) {
	for _, versions := range f.agents {
		for _, v := range versions {
			if v.ID == id {
				return v, nil
			}
		}
	}
	return agent.Agent{}, errors.New("not found")
}

// seed stashes an agent directly in the repo (bypassing validation) and
// returns its id — the numeric handle Update/Delete/ListVersions now take.
func (f *fakeRepo) seed(ref, version string) int64 {
	a := agent.Agent{ID: f.nextID, Ref: ref, Version: version, Status: agent.StatusEnabled}
	f.nextID++
	f.agents[ref] = append([]agent.Agent{a}, f.agents[ref]...)
	return a.ID
}

type fakeCatalog struct {
	tools  map[string]agent.RefStatus
	skills map[string]agent.RefStatus
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{tools: map[string]agent.RefStatus{}, skills: map[string]agent.RefStatus{}}
}

func (f *fakeCatalog) ToolStatus(_ context.Context, _ int64, ref string) (agent.RefStatus, error) {
	return f.tools[ref], nil
}

func (f *fakeCatalog) SkillStatus(_ context.Context, _ int64, ref string) (agent.RefStatus, error) {
	return f.skills[ref], nil
}

// fakeValidator lets a rule test choose "schema passes" or "schema fails"
// without dragging a real JSON Schema compiler into the domain test. The
// real validator is wired end-to-end in the transport test instead.
type fakeValidator struct{ errs []domain.FieldError }

func (f fakeValidator) Validate(map[string]any) ([]domain.FieldError, error) { return f.errs, nil }

func newService(repo *fakeRepo, cat *fakeCatalog, v agent.DefinitionValidator) *agent.Service {
	return agent.NewService(repo, cat, v, fakeChannels{})
}

func def(ref, version string) agent.Definition {
	return agent.Definition{
		"agent": ref, "version": version,
		"capabilities": map[string]any{"tools": []any{}, "skills": []any{}},
	}
}

// assertDomainErr fails unless err is a *domain.Error with the given kind
// and business code.
func assertDomainErr(t *testing.T, err error, wantKind domain.Kind, wantCode int) *domain.Error {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected a *domain.Error, got %T: %v", err, err)
	}
	if de.Kind != wantKind {
		t.Fatalf("kind = %v, want %v (err: %v)", de.Kind, wantKind, err)
	}
	if de.Code != wantCode {
		t.Fatalf("code = %d, want %d (err: %v)", de.Code, wantCode, err)
	}
	return de
}

func TestCreate_SchemaFailureIsInvalidWith40001AndDetails(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{
		errs: []domain.FieldError{{Field: "model.provider", Reason: "must be one of ..."}},
	})

	_, err := svc.Create(context.Background(), 1, def("architect", "1.0"))

	de := assertDomainErr(t, err, domain.KindInvalid, domain.CodeAgentSchemaInvalid)
	if len(de.Details) != 1 || de.Details[0].Field != "model.provider" {
		t.Fatalf("schema field errors should reach the caller, got %+v", de.Details)
	}
}

func TestCreate_DisabledToolIsRejectedWith30002(t *testing.T) {
	cat := newFakeCatalog()
	cat.tools["broken-tool"] = agent.RefStatus{Found: true, Enabled: false}
	svc := newService(newFakeRepo(), cat, fakeValidator{})

	d := def("architect", "1.0")
	d["capabilities"].(map[string]any)["tools"] = []any{"broken-tool"}

	_, err := svc.Create(context.Background(), 1, d)

	de := assertDomainErr(t, err, domain.KindInvalid, domain.CodeResourceDisabled)
	if len(de.Details) != 1 || de.Details[0].Field != "capabilities.tools[0]" {
		t.Fatalf("expected the offending index to be named, got %+v", de.Details)
	}
	// Disabled and not-found share a code but must not share wording — the
	// fix differs.
	if got := de.Details[0].Reason; got != `resource "broken-tool" is disabled` {
		t.Fatalf("disabled wording = %q", got)
	}
}

func TestCreate_UnknownToolIsRejectedWithDistinctWording(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})

	d := def("architect", "1.0")
	d["capabilities"].(map[string]any)["tools"] = []any{"does-not-exist"}

	_, err := svc.Create(context.Background(), 1, d)

	de := assertDomainErr(t, err, domain.KindInvalid, domain.CodeResourceDisabled)
	if got := de.Details[0].Reason; got != `resource "does-not-exist" does not exist` {
		t.Fatalf("not-found wording = %q", got)
	}
}

func TestCreate_EnabledCapabilitiesPass(t *testing.T) {
	cat := newFakeCatalog()
	cat.tools["good-tool"] = agent.RefStatus{Found: true, Enabled: true}
	cat.skills["good-skill"] = agent.RefStatus{Found: true, Enabled: true}
	svc := newService(newFakeRepo(), cat, fakeValidator{})

	d := def("architect", "1.0")
	d["capabilities"].(map[string]any)["tools"] = []any{"good-tool"}
	d["capabilities"].(map[string]any)["skills"] = []any{"good-skill"}

	created, err := svc.Create(context.Background(), 1, d)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The ref/version come from the DSL, not from the caller.
	if created.Ref != "architect" || created.Version != "1.0" {
		t.Fatalf("ref/version should be read out of the definition, got %q/%q", created.Ref, created.Version)
	}
}

func TestCreate_DuplicateVersionIsConflict(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})
	ctx := context.Background()

	if _, err := svc.Create(ctx, 1, def("dup", "1.0")); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := svc.Create(ctx, 1, def("dup", "1.0"))

	assertDomainErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

func TestDelete_BlockedBySubscribedVersion(t *testing.T) {
	repo := newFakeRepo()
	repo.subscribed["subscribed-agent"] = 1
	id := repo.seed("subscribed-agent", "1.0")
	svc := newService(repo, newFakeCatalog(), fakeValidator{})

	err := svc.Delete(context.Background(), 1, id)

	assertDomainErr(t, err, domain.KindConflict, domain.CodeSubscribedVersionLocked)
}

func TestDelete_BlockedByReferencingBundleNamesTheBundle(t *testing.T) {
	repo := newFakeRepo()
	repo.referencing["used-agent"] = []agent.BundleRef{{Ref: "web-app-builder", Version: "2.0"}}
	id := repo.seed("used-agent", "1.0")
	svc := newService(repo, newFakeCatalog(), fakeValidator{})

	err := svc.Delete(context.Background(), 1, id)

	de := assertDomainErr(t, err, domain.KindConflict, domain.CodeAgentVersionNotFound)
	if len(de.Details) != 1 || de.Details[0].Reason != "Bundle web-app-builder v2.0 正在引用" {
		t.Fatalf("the refusal must name the referencing Bundle, got %+v", de.Details)
	}
}

// The occupancy checks race a concurrent subscribe, so storage refusing the
// delete has to surface as the same 70005 the pre-check would have given.
func TestDelete_StorageLockWinsTheRace(t *testing.T) {
	repo := newFakeRepo()
	repo.deleteErr = agent.ErrVersionLocked
	id := repo.seed("free-agent", "1.0")
	svc := newService(repo, newFakeCatalog(), fakeValidator{})

	err := svc.Delete(context.Background(), 1, id)

	assertDomainErr(t, err, domain.KindConflict, domain.CodeSubscribedVersionLocked)
}

func TestDelete_SucceedsWhenUnoccupied(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, newFakeCatalog(), fakeValidator{})
	created, err := svc.Create(context.Background(), 1, def("free-agent", "1.0"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := svc.Delete(context.Background(), 1, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, still := repo.agents["free-agent"]; still {
		t.Fatal("expected the agent to be gone")
	}
}

func TestListVersions_UnknownIDIsNotFound(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})

	_, err := svc.ListVersions(context.Background(), 1, 999)

	assertDomainErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}

func TestListVersions_ReturnsNewestFirst(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, newFakeCatalog(), fakeValidator{})
	ctx := context.Background()
	var firstID int64
	for _, v := range []string{"1.0", "2.0"} {
		created, err := svc.Create(ctx, 1, def("pm", v))
		if err != nil {
			t.Fatalf("seed %s: %v", v, err)
		}
		if v == "1.0" {
			firstID = created.ID
		}
	}

	versions, err := svc.ListVersions(ctx, 1, firstID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != "2.0" {
		t.Fatalf("expected newest first, got %+v", versions)
	}
}

// Limit is clamped in the service, so a transport that forgets to validate
// can't push an unbounded limit through to a repository.
func TestList_ClampsLimit(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, newFakeCatalog(), fakeValidator{})
	ctx := context.Background()
	for _, ref := range []string{"a", "b", "c"} {
		if _, err := svc.Create(ctx, 1, def(ref, "1.0")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	page, err := svc.List(ctx, 1, domain.PageQuery{Limit: 10_000})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 3 || page.HasMore {
		t.Fatalf("got %d items hasMore=%v", len(page.Items), page.HasMore)
	}
}

func TestList_ReportsHasMoreAndNextCursor(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, newFakeCatalog(), fakeValidator{})
	ctx := context.Background()
	for _, ref := range []string{"a", "b", "c"} {
		if _, err := svc.Create(ctx, 1, def(ref, "1.0")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	page, err := svc.List(ctx, 1, domain.PageQuery{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("got %d items hasMore=%v, want 2/true", len(page.Items), page.HasMore)
	}
	if page.NextCursor != page.Items[1].Ref {
		t.Fatalf("next cursor = %q, want the last item's ref %q", page.NextCursor, page.Items[1].Ref)
	}
}

// Schema 只能管住 model.provider 的形状，管不了"这个渠道存不存在"——渠道
// 是管理员运行时建出来的。没有这道校验，写错一个 provider 名要等到真的跑
// 一次才会以 "no client configured" 暴露，而那时候 Agent 已经存下来、接进
// Bundle 了。
func modelDef(model map[string]any) agent.Definition {
	d := def("a", "1.0")
	d["model"] = model
	return d
}

func TestCreate_RejectsUnregisteredModelProvider(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})

	_, err := svc.Create(context.Background(), 1, modelDef(map[string]any{"provider": "not-registered", "name": "x"}))
	derr := assertDomainErr(t, err, domain.KindInvalid, domain.CodeAgentSchemaInvalid)
	if len(derr.Details) == 0 || derr.Details[0].Field != "model.provider" {
		t.Fatalf("字段错误应指向 model.provider: %+v", derr.Details)
	}
	// 报错要说清楚有哪些可选，否则用户不知道该填什么。
	if !strings.Contains(derr.Details[0].Reason, "deepseek") {
		t.Errorf("报错里应列出当前可用的渠道: %s", derr.Details[0].Reason)
	}
}

func TestCreate_RejectsUnregisteredFallbackProvider(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})

	_, err := svc.Create(context.Background(), 1, modelDef(map[string]any{
		"provider": "deepseek", "name": "deepseek-chat",
		"fallback": []any{"volcengine/doubao", "gone/some-model"},
	}))
	derr := assertDomainErr(t, err, domain.KindInvalid, domain.CodeAgentSchemaInvalid)
	if len(derr.Details) != 1 || derr.Details[0].Field != "model.fallback[1]" {
		t.Fatalf("只有第二个降级项该报错: %+v", derr.Details)
	}
}

func TestCreate_AcceptsRegisteredProviders(t *testing.T) {
	svc := newService(newFakeRepo(), newFakeCatalog(), fakeValidator{})
	_, err := svc.Create(context.Background(), 1, modelDef(map[string]any{
		"provider": "deepseek", "name": "deepseek-chat",
		"fallback": []any{"volcengine/doubao"},
	}))
	if err != nil {
		t.Fatalf("已登记的渠道该能通过: %v", err)
	}
}

// 一个渠道都没登记时不拦：拦下来只会让新装的实例连一个 Agent 都建不了，而
// 报错还指不到该去哪配。
func TestCreate_SkipsProviderCheckWhenNoChannelsRegistered(t *testing.T) {
	svc := agent.NewService(newFakeRepo(), newFakeCatalog(), fakeValidator{}, emptyChannels{})
	if _, err := svc.Create(context.Background(), 1, modelDef(map[string]any{"provider": "anything", "name": "x"})); err != nil {
		t.Fatalf("没有任何渠道时不该拦: %v", err)
	}
}

// fakeChannels 模拟已登记的模型渠道。
type fakeChannels struct{}

func (fakeChannels) ProviderNames() []string { return []string{"deepseek", "volcengine"} }

type emptyChannels struct{}

func (emptyChannels) ProviderNames() []string { return nil }
