package bundle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
)

type fakeRepo struct {
	bundles    map[string][]bundle.Bundle
	nextID     int64
	subscribed map[string]int64
	deleteErr  error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{bundles: map[string][]bundle.Bundle{}, nextID: 1, subscribed: map[string]int64{}}
}

func (f *fakeRepo) ListLatestByOwner(_ context.Context, _ int64, q domain.PageQuery) ([]bundle.Bundle, error) {
	var out []bundle.Bundle
	for ref, versions := range f.bundles {
		if ref > q.After && len(versions) > 0 {
			out = append(out, versions[0])
		}
	}
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *fakeRepo) Create(_ context.Context, b bundle.Bundle) (bundle.Bundle, error) {
	for _, v := range f.bundles[b.Ref] {
		if v.Version == b.Version {
			return bundle.Bundle{}, bundle.ErrDuplicateVersion
		}
	}
	b.ID, b.Status = f.nextID, bundle.StatusEnabled
	f.nextID++
	f.bundles[b.Ref] = append([]bundle.Bundle{b}, f.bundles[b.Ref]...)
	return b, nil
}

func (f *fakeRepo) DeleteByRef(_ context.Context, _ int64, ref string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.bundles, ref)
	return nil
}

func (f *fakeRepo) CountActiveSubscribedVersions(_ context.Context, _ int64, ref string) (int64, error) {
	return f.subscribed[ref], nil
}

type fakeHandoffs struct{ byRef map[string]bundle.Handoff }

func newFakeHandoffs() *fakeHandoffs { return &fakeHandoffs{byRef: map[string]bundle.Handoff{}} }

func (f *fakeHandoffs) Lookup(_ context.Context, _ int64, ref string) (bundle.Handoff, error) {
	return f.byRef[ref], nil
}

// passValidator stands in for the JSON Schema check, which has its own tests
// in internal/dslschema and is wired end-to-end in the transport test.
type passValidator struct{ errs []domain.FieldError }

func (v passValidator) Validate(map[string]any) ([]domain.FieldError, error) { return v.errs, nil }

func newSvc(repo *fakeRepo, h *fakeHandoffs, v bundle.DefinitionValidator) *bundle.Service {
	return bundle.NewService(repo, h, v)
}

// linearDef is product_manager -> architect -> END.
func linearDef(ref string) bundle.Definition {
	return bundle.Definition{
		"bundle": ref, "version": "1.0",
		"agents": []any{
			map[string]any{"ref": "product_manager"},
			map[string]any{"ref": "architect"},
		},
		"orchestration": map[string]any{
			"mode": "graph", "entry": "product_manager",
			"edges": []any{
				map[string]any{"from": "product_manager", "to": "architect"},
				map[string]any{"from": "architect", "to": "END"},
			},
		},
	}
}

func assertErr(t *testing.T, err error, kind domain.Kind, code int) *domain.Error {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.Error, got %T: %v", err, err)
	}
	if de.Kind != kind || de.Code != code {
		t.Fatalf("got kind=%v code=%d, want kind=%v code=%d (%v)", de.Kind, de.Code, kind, code, err)
	}
	return de
}

func TestCreate_SchemaFailureIs40002(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{
		errs: []domain.FieldError{{Field: "orchestration.mode", Reason: "must be graph"}},
	})

	_, err := svc.Create(context.Background(), 1, linearDef("x"))

	assertErr(t, err, domain.KindInvalid, domain.CodeBundleSchemaInvalid)
}

func TestCreate_Success(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{})

	got, err := svc.Create(context.Background(), 1, linearDef("test-bundle"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Bundle.Ref != "test-bundle" || got.Bundle.Version != "1.0" {
		t.Fatalf("ref/version should come from the DSL, got %q/%q", got.Bundle.Ref, got.Bundle.Version)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("a clean graph should produce no warnings, got %+v", got.Warnings)
	}
}

// An entry naming a node that isn't declared makes the Bundle unrunnable —
// blocking, 422.
func TestCreate_UnknownEntryIsBlocking422(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{})
	def := linearDef("bad-entry")
	def["orchestration"].(map[string]any)["entry"] = "not-a-declared-agent"

	_, err := svc.Create(context.Background(), 1, def)

	de := assertErr(t, err, domain.KindUnprocessable, domain.CodeBundleGraphInvalid)
	if len(de.Details) == 0 {
		t.Fatal("the offending graph issue must reach the caller")
	}
}

// A node whose only incoming edge is its own self-loop can never fire.
func TestCreate_SelfLoopOnlyDeadlockIsBlocking422(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{})
	def := bundle.Definition{
		"bundle": "deadlocked", "version": "1.0",
		"agents": []any{map[string]any{"ref": "a"}, map[string]any{"ref": "b"}},
		"orchestration": map[string]any{
			"mode": "graph", "entry": "a",
			"edges": []any{
				map[string]any{"from": "a", "to": "END"},
				map[string]any{"from": "b", "to": "b"},
			},
		},
	}

	_, err := svc.Create(context.Background(), 1, def)

	assertErr(t, err, domain.KindUnprocessable, domain.CodeBundleGraphInvalid)
}

// The counterpart: a self-loop that is a legitimate conditional retry has a
// real entry path, so it must save.
func TestCreate_ConditionalRetrySelfLoopSaves(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{})
	def := bundle.Definition{
		"bundle": "retry", "version": "1.0",
		"agents": []any{map[string]any{"ref": "a"}, map[string]any{"ref": "b"}},
		"orchestration": map[string]any{
			"mode": "graph", "entry": "a",
			"edges": []any{
				map[string]any{"from": "a", "to": "b"},
				map[string]any{"from": "b", "to": "END", "condition": "shared_state.ok == true"},
				map[string]any{"from": "b", "to": "b", "condition": "shared_state.ok == false"},
			},
		},
	}

	if _, err := svc.Create(context.Background(), 1, def); err != nil {
		t.Fatalf("a conditional retry loop must not block the save: %v", err)
	}
}

// spec-07 point 5: the two DSLs can be maintained by different people, so
// drift is reported and the save still succeeds.
func TestCreate_HandoffDriftWarnsButSaves(t *testing.T) {
	h := newFakeHandoffs()
	h.byRef["architect"] = bundle.Handoff{AcceptsInputFrom: []string{"someone_else"}}
	svc := newSvc(newFakeRepo(), h, passValidator{})

	got, err := svc.Create(context.Background(), 1, linearDef("drift"))
	if err != nil {
		t.Fatalf("handoff drift must not block the save: %v", err)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("expected exactly one drift warning, got %+v", got.Warnings)
	}
	if got.Warnings[0].Field != "orchestration.edges[0]" {
		t.Fatalf("the warning should point at the offending edge, got %q", got.Warnings[0].Field)
	}
}

// An empty declaration means "unspecified", not "accepts nothing".
func TestCreate_EmptyHandoffDeclarationIsNotDrift(t *testing.T) {
	h := newFakeHandoffs()
	h.byRef["architect"] = bundle.Handoff{}
	svc := newSvc(newFakeRepo(), h, passValidator{})

	got, err := svc.Create(context.Background(), 1, linearDef("no-decl"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("an undeclared handoff must not warn, got %+v", got.Warnings)
	}
}

// A matching declaration is silent — proves the check isn't warning on
// everything.
func TestCreate_MatchingHandoffIsSilent(t *testing.T) {
	h := newFakeHandoffs()
	h.byRef["architect"] = bundle.Handoff{AcceptsInputFrom: []string{"product_manager"}}
	h.byRef["product_manager"] = bundle.Handoff{ProducesOutputTo: []string{"architect"}}
	svc := newSvc(newFakeRepo(), h, passValidator{})

	got, err := svc.Create(context.Background(), 1, linearDef("aligned"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("aligned declarations must not warn, got %+v", got.Warnings)
	}
}

// An alias binds a node name to an agent ref, so drift must be judged
// against the ref, not the node.
func TestCreate_HandoffDriftFollowsAliases(t *testing.T) {
	h := newFakeHandoffs()
	h.byRef["architect"] = bundle.Handoff{AcceptsInputFrom: []string{"product_manager"}}
	svc := newSvc(newFakeRepo(), h, passValidator{})

	def := bundle.Definition{
		"bundle": "aliased", "version": "1.0",
		"agents": []any{
			map[string]any{"ref": "product_manager"},
			map[string]any{"ref": "architect", "alias": "arch_2"},
		},
		"orchestration": map[string]any{
			"mode": "graph", "entry": "product_manager",
			"edges": []any{
				map[string]any{"from": "product_manager", "to": "arch_2"},
				map[string]any{"from": "arch_2", "to": "END"},
			},
		},
	}

	got, err := svc.Create(context.Background(), 1, def)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("the alias resolves to architect, which does accept product_manager: %+v", got.Warnings)
	}
}

func TestCreate_DuplicateVersionIsConflict(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeHandoffs(), passValidator{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, linearDef("dup")); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := svc.Create(ctx, 1, linearDef("dup"))

	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

func TestDelete_BlockedBySubscribedVersion(t *testing.T) {
	repo := newFakeRepo()
	repo.subscribed["subscribed-bundle"] = 1
	svc := newSvc(repo, newFakeHandoffs(), passValidator{})

	err := svc.Delete(context.Background(), 1, "subscribed-bundle")

	assertErr(t, err, domain.KindConflict, domain.CodeSubscribedVersionLocked)
}

func TestDelete_StorageLockWinsTheRace(t *testing.T) {
	repo := newFakeRepo()
	repo.deleteErr = bundle.ErrVersionLocked
	svc := newSvc(repo, newFakeHandoffs(), passValidator{})

	err := svc.Delete(context.Background(), 1, "free")

	assertErr(t, err, domain.KindConflict, domain.CodeSubscribedVersionLocked)
}

func TestDelete_SucceedsWhenUnoccupied(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, newFakeHandoffs(), passValidator{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, linearDef("free")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := svc.Delete(ctx, 1, "free"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, still := repo.bundles["free"]; still {
		t.Fatal("expected the bundle to be gone")
	}
}
