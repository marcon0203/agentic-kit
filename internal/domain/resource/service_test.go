package resource_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

type fakeRepo struct {
	byKind     map[resource.Kind]map[int64]resource.Resource
	nextID     int64
	refs       map[string]bool // "kind/ownerID/ref" -> taken
	references map[string][]resource.AgentReference
	health     map[int64]resource.Health
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byKind: map[resource.Kind]map[int64]resource.Resource{}, nextID: 1,
		refs: map[string]bool{}, references: map[string][]resource.AgentReference{},
		health: map[int64]resource.Health{},
	}
}

func refKey(kind resource.Kind, ownerID int64, ref string) string {
	return string(kind) + "/" + itoa(ownerID) + "/" + ref
}

func itoa(i int64) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func (f *fakeRepo) Create(_ context.Context, r resource.Resource) (resource.Resource, error) {
	k := refKey(r.Kind, r.OwnerID, r.Ref)
	if f.refs[k] {
		return resource.Resource{}, resource.ErrDuplicate
	}
	f.refs[k] = true
	r.ID = f.nextID
	f.nextID++
	if f.byKind[r.Kind] == nil {
		f.byKind[r.Kind] = map[int64]resource.Resource{}
	}
	f.byKind[r.Kind][r.ID] = r
	return r, nil
}

// CreateBatch simulates a real transaction: it validates every ref is free
// before writing any of them, so a duplicate anywhere in the batch leaves
// the repo completely unchanged — the same all-or-nothing guarantee the
// real Postgres implementation gets from wrapping the inserts in a tx.
func (f *fakeRepo) CreateBatch(_ context.Context, resources []resource.Resource) ([]resource.Resource, error) {
	for _, r := range resources {
		if f.refs[refKey(r.Kind, r.OwnerID, r.Ref)] {
			return nil, resource.ErrDuplicate
		}
	}
	out := make([]resource.Resource, len(resources))
	for i, r := range resources {
		f.refs[refKey(r.Kind, r.OwnerID, r.Ref)] = true
		r.ID = f.nextID
		f.nextID++
		if f.byKind[r.Kind] == nil {
			f.byKind[r.Kind] = map[int64]resource.Resource{}
		}
		f.byKind[r.Kind][r.ID] = r
		out[i] = r
	}
	return out, nil
}

func (f *fakeRepo) GetByID(_ context.Context, kind resource.Kind, id, ownerID int64) (resource.Resource, error) {
	r, ok := f.byKind[kind][id]
	if !ok || r.OwnerID != ownerID {
		return resource.Resource{}, resource.ErrNotFound
	}
	return r, nil
}

func (f *fakeRepo) ListPage(_ context.Context, kind resource.Kind, ownerID, afterID int64, limit int32) ([]resource.Resource, error) {
	var out []resource.Resource
	for id := int64(1); id < f.nextID; id++ {
		r, ok := f.byKind[kind][id]
		if !ok || r.OwnerID != ownerID || r.ID <= afterID {
			continue
		}
		out = append(out, r)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, r resource.Resource) (resource.Resource, error) {
	f.byKind[r.Kind][r.ID] = r
	return r, nil
}

func (f *fakeRepo) FindReferencingAgents(_ context.Context, kind resource.Kind, _ int64, ref string) ([]resource.AgentReference, error) {
	return f.references[string(kind)+"/"+ref], nil
}

func (f *fakeRepo) SetHealth(_ context.Context, id int64, h resource.Health) error {
	f.health[id] = h
	return nil
}

// reverseCipher is a visible stand-in for AES: reversing a string is
// obviously not the real algorithm, which keeps these tests about *which*
// fields get encrypted rather than about crypto.
type reverseCipher struct{ failOn string }

func (c reverseCipher) Encrypt(s string) (string, error) {
	if c.failOn != "" && s == c.failOn {
		return "", errors.New("boom")
	}
	return reverse(s), nil
}
func (c reverseCipher) Decrypt(s string) (string, error) { return reverse(s), nil }

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

type stubProbe struct{ verdict resource.Health }

func (p stubProbe) Check(context.Context, resource.Config) resource.Health { return p.verdict }

func newSvc(repo *fakeRepo, probe resource.HealthProbe) *resource.Service {
	return resource.NewService(repo, reverseCipher{}, probe, true)
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

// ── Credential rules ─────────────────────────────────────────────────

func TestIsCredentialKey_CoversCommonShapes(t *testing.T) {
	for _, key := range []string{"api_key", "apiKey", "API_KEY", "auth_token", "token", "secret", "password", "my_credential", "private_key"} {
		if !resource.IsCredentialKey(key) {
			t.Errorf("%q should be treated as a credential", key)
		}
	}
	for _, key := range []string{"endpoint", "timeout_seconds", "region", "model"} {
		if resource.IsCredentialKey(key) {
			t.Errorf("%q must not be treated as a credential", key)
		}
	}
}

// spec-05: credentials are *omitted*, not masked and not returned as
// ciphertext.
func TestRedact_RemovesCredentialsEntirely(t *testing.T) {
	cfg := resource.Config{"endpoint": "https://x", "api_key": "sk-secret", "timeout_seconds": 30}

	got := cfg.Redact()

	if _, present := got["api_key"]; present {
		t.Fatalf("credential must be absent, not masked: %+v", got)
	}
	if got["endpoint"] != "https://x" || got["timeout_seconds"] != 30 {
		t.Fatalf("non-credential fields must survive: %+v", got)
	}
}

func TestCreate_EncryptsOnlyCredentialFields(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "tool", Ref: "my-tool",
		Config: resource.Config{"endpoint": "https://api.example.com", "api_key": "sk-live-123"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	stored := repo.byKind[resource.KindTool][1]
	if stored.Config["endpoint"] != "https://api.example.com" {
		t.Fatalf("non-credential field must be stored as-is, got %v", stored.Config["endpoint"])
	}
	if stored.Config["api_key"] != reverse("sk-live-123") {
		t.Fatalf("credential must be stored encrypted, got %v", stored.Config["api_key"])
	}
}

// The response must never carry the credential — plaintext or ciphertext.
func TestCreate_ResponseNeverCarriesTheCredential(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	created, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "tool", Ref: "my-tool",
		Config: resource.Config{"endpoint": "https://x", "api_key": "sk-live-123"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, present := created.Config["api_key"]; present {
		t.Fatalf("the returned config must omit credentials: %+v", created.Config)
	}
	for _, v := range created.Config {
		if s, ok := v.(string); ok && strings.Contains(s, "sk-live") {
			t.Fatalf("the secret leaked into another field: %+v", created.Config)
		}
	}
}

func TestCreate_NonStringCredentialIsRejected(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "tool", Ref: "my-tool",
		Config: resource.Config{"api_key": 12345},
	})

	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestDecryptCredentials_RoundTrips(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, resource.CreateCommand{
		Kind: "tool", Ref: "my-tool",
		Config: resource.Config{"endpoint": "https://x", "api_key": "sk-live-123"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	decrypted, err := svc.DecryptCredentials(repo.byKind[resource.KindTool][1].Config)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted["api_key"] != "sk-live-123" {
		t.Fatalf("round trip failed, got %v", decrypted["api_key"])
	}
	if decrypted["endpoint"] != "https://x" {
		t.Fatalf("non-credential field must pass through, got %v", decrypted["endpoint"])
	}
}

// A "headers" list's values are treated as credentials unconditionally —
// unlike every other field, IsCredentialKey's name-based guess can't reach
// inside a []any of {key,value} objects, and a custom header name is
// unpredictable (spec-05a).
func TestRedact_HeaderListValuesRemovedKeysKept(t *testing.T) {
	cfg := resource.Config{"headers": []any{
		map[string]any{"key": "Authorization", "value": "Bearer sk-secret"},
		map[string]any{"key": "X-Custom", "value": "also-secret"},
	}}

	got := cfg.Redact()

	headers, ok := got["headers"].([]any)
	if !ok || len(headers) != 2 {
		t.Fatalf("expected 2 header entries to survive, got %+v", got["headers"])
	}
	for _, h := range headers {
		m := h.(map[string]any)
		if _, present := m["value"]; present {
			t.Fatalf("header value must be absent, not masked: %+v", m)
		}
		if m["key"] == "" {
			t.Fatalf("header key must survive redaction: %+v", m)
		}
	}
}

func TestCreate_HeaderListValuesEncryptedKeysPlain(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "mcp", Ref: "my-mcp",
		Config: resource.Config{
			"endpoint": "https://mcp.example.com",
			"headers":  []any{map[string]any{"key": "Authorization", "value": "Bearer sk-live-123"}},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	stored := repo.byKind[resource.KindMCP][1]
	headers := stored.Config["headers"].([]any)
	h := headers[0].(map[string]any)
	if h["key"] != "Authorization" {
		t.Fatalf("header key must be stored as-is, got %v", h["key"])
	}
	if h["value"] == "Bearer sk-live-123" {
		t.Fatalf("header value must be stored encrypted, got %v", h["value"])
	}
	if h["value"] != reverse("Bearer sk-live-123") {
		t.Fatalf("expected the stub cipher's ciphertext, got %v", h["value"])
	}
}

func TestDecryptCredentials_HeaderListRoundTrips(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, resource.CreateCommand{
		Kind: "mcp", Ref: "my-mcp",
		Config: resource.Config{"headers": []any{map[string]any{"key": "Authorization", "value": "Bearer sk-live-123"}}},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	decrypted, err := svc.DecryptCredentials(repo.byKind[resource.KindMCP][1].Config)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	headers := decrypted["headers"].([]any)
	h := headers[0].(map[string]any)
	if h["value"] != "Bearer sk-live-123" {
		t.Fatalf("round trip failed, got %v", h["value"])
	}
}

// ── Validation & lifecycle ───────────────────────────────────────────

func TestCreate_InvalidRefAndTypeReportBothFields(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "not-a-kind", Ref: "Not A Ref", Config: resource.Config{},
	})

	de := assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
	fields := map[string]bool{}
	for _, d := range de.Details {
		fields[d.Field] = true
	}
	if !fields["type"] || !fields["ref"] {
		t.Fatalf("both problems should be reported at once, got %+v", de.Details)
	}
}

func TestCreate_MissingConfigIsRejected(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{Kind: "tool", Ref: "my-tool"})

	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestCreate_DuplicateRefIsConflict(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})
	ctx := context.Background()
	cmd := resource.CreateCommand{Kind: "tool", Ref: "dup", Config: resource.Config{}}
	if _, err := svc.Create(ctx, 1, cmd); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := svc.Create(ctx, 1, cmd)

	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

// spec-05: a failed probe still saves, so the owner can come back and fix
// the endpoint — it only records health=unhealthy.
func TestCreate_FailedMCPProbeStillSaves(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{verdict: resource.HealthUnhealthy})

	created, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "mcp", Ref: "dead-server", Config: resource.Config{"endpoint": "https://nope"},
	})
	if err != nil {
		t.Fatalf("an unreachable MCP server must still save: %v", err)
	}
	if created.Health != resource.HealthUnhealthy {
		t.Fatalf("health = %q, want unhealthy", created.Health)
	}
	if repo.health[created.ID] != resource.HealthUnhealthy {
		t.Fatal("the probe verdict should be persisted")
	}
}

// Only MCP servers are probed.
func TestCreate_NonMCPKindIsNotProbed(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{verdict: resource.HealthUnhealthy})

	created, err := svc.Create(context.Background(), 1, resource.CreateCommand{
		Kind: "tool", Ref: "a-tool", Config: resource.Config{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Health != resource.HealthUnknown {
		t.Fatalf("a tool should have no health verdict, got %q", created.Health)
	}
	if len(repo.health) != 0 {
		t.Fatal("only MCP servers should have health written")
	}
}

func TestUpdate_WrongOwnerIsNotFound(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	created, _ := svc.Create(ctx, 1, resource.CreateCommand{Kind: "tool", Ref: "mine", Config: resource.Config{}})

	_, err := svc.Update(ctx, 999, resource.KindTool, created.ID, resource.UpdateCommand{})

	assertErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}

// PATCH semantics: a nil field is left alone.
func TestUpdate_OnlyPatchesProvidedFields(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	created, _ := svc.Create(ctx, 1, resource.CreateCommand{
		Kind: "tool", Ref: "mine", DisplayName: "Original",
		Config: resource.Config{"endpoint": "https://x", "api_key": "sk-1"},
	})

	disabled := resource.StatusDisabled
	updated, err := svc.Update(ctx, 1, resource.KindTool, created.ID, resource.UpdateCommand{Status: &disabled})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != resource.StatusDisabled {
		t.Fatalf("status should have changed, got %v", updated.Status)
	}
	if updated.DisplayName != "Original" {
		t.Fatalf("display name should be untouched, got %q", updated.DisplayName)
	}
	// The untouched credential must remain encrypted at rest, not
	// re-encrypted or dropped.
	if repo.byKind[resource.KindTool][created.ID].Config["api_key"] != reverse("sk-1") {
		t.Fatalf("stored credential changed on an unrelated patch: %v", repo.byKind[resource.KindTool][created.ID].Config["api_key"])
	}
}

func TestDeleteCheck_ReportsReferences(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	created, _ := svc.Create(ctx, 1, resource.CreateCommand{Kind: "tool", Ref: "used", Config: resource.Config{}})
	repo.references["tool/used"] = []resource.AgentReference{{AgentRef: "architect", Version: "1.0"}}

	check, err := svc.DeleteCheck(ctx, 1, resource.KindTool, created.ID)
	if err != nil {
		t.Fatalf("delete check: %v", err)
	}
	if check.Deletable {
		t.Fatal("a referenced resource is not deletable")
	}
	if len(check.ReferencedBy) != 1 || check.ReferencedBy[0].AgentRef != "architect" {
		t.Fatalf("the holder must be named, got %+v", check.ReferencedBy)
	}
}

func TestDeleteCheck_DeletableWhenUnreferenced(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	created, _ := svc.Create(ctx, 1, resource.CreateCommand{Kind: "tool", Ref: "free", Config: resource.Config{}})

	check, err := svc.DeleteCheck(ctx, 1, resource.KindTool, created.ID)
	if err != nil {
		t.Fatalf("delete check: %v", err)
	}
	if !check.Deletable || len(check.ReferencedBy) != 0 {
		t.Fatalf("expected deletable with no holders, got %+v", check)
	}
}

func TestList_RejectsUnknownType(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.List(context.Background(), 1, resource.ListQuery{Kind: "nope"})

	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestList_FiltersByKindAndRedacts(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, resource.CreateCommand{Kind: "tool", Ref: "t1", Config: resource.Config{"api_key": "sk-1"}}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	if _, err := svc.Create(ctx, 1, resource.CreateCommand{Kind: "skill", Ref: "s1", Config: resource.Config{}}); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	page, err := svc.List(ctx, 1, resource.ListQuery{Kind: "tool"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Kind != resource.KindTool {
		t.Fatalf("expected only tools, got %+v", page.Items)
	}
	if _, present := page.Items[0].Config["api_key"]; present {
		t.Fatalf("list must redact credentials: %+v", page.Items[0].Config)
	}
	if page.NextCursor == "" {
		t.Fatal("a single-kind page should carry a resumable cursor")
	}
}

// KB_ENABLED gates knowledge_base registration itself, not just the
// /kb/... sub-routes — a knowledge_base resource nothing can ingest into
// or search would just be a dead config record.
func TestCreate_RejectsKnowledgeBaseKindWhenDisabled(t *testing.T) {
	repo := newFakeRepo()
	svc := resource.NewService(repo, reverseCipher{}, stubProbe{}, false)

	_, err := svc.Create(context.Background(), 1, resource.CreateCommand{Kind: "knowledge_base", Ref: "kb-1", Config: resource.Config{}})
	de := assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
	if len(de.Details) == 0 || de.Details[0].Field != "type" {
		t.Fatalf("expected the type field to carry the disabled-KB reason, got %+v", de.Details)
	}
}

// The unfiltered list merges a first page per kind — a deliberate V1
// simplification, so it deliberately has no single resumable cursor.
func TestList_MergedAcrossKindsHasNoCursor(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	ctx := context.Background()
	for _, k := range []string{"tool", "skill", "mcp", "knowledge_base"} {
		if _, err := svc.Create(ctx, 1, resource.CreateCommand{Kind: k, Ref: k + "-1", Config: resource.Config{}}); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	page, err := svc.List(ctx, 1, resource.ListQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 4 {
		t.Fatalf("expected one of each kind, got %d", len(page.Items))
	}
	if page.NextCursor != "" {
		t.Fatalf("a merged page has no single keyset to resume from, got %q", page.NextCursor)
	}
}
