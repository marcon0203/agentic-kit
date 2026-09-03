package apikey_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/apikey"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeRepo struct {
	byOwner map[int64][]apikey.APIKey
	nextID  int64
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byOwner: map[int64][]apikey.APIKey{}, nextID: 1} }

func (f *fakeRepo) Create(_ context.Context, ownerID int64, name, _ string) (apikey.APIKey, error) {
	k := apikey.APIKey{ID: f.nextID, Name: name, CreatedAt: time.Now()}
	f.nextID++
	f.byOwner[ownerID] = append(f.byOwner[ownerID], k)
	return k, nil
}

func (f *fakeRepo) ListForOwner(_ context.Context, ownerID int64) ([]apikey.APIKey, error) {
	return append([]apikey.APIKey(nil), f.byOwner[ownerID]...), nil
}

func (f *fakeRepo) Revoke(_ context.Context, ownerID, keyID int64) error {
	keys := f.byOwner[ownerID]
	for i, k := range keys {
		if k.ID == keyID {
			if !k.Active() {
				return apikey.ErrNotFound
			}
			now := time.Now()
			keys[i].RevokedAt = &now
			return nil
		}
	}
	return apikey.ErrNotFound
}

type fakeGenerator struct{ calls int }

func (f *fakeGenerator) GenerateAPIKey() (string, string, error) {
	f.calls++
	return fmt.Sprintf("raw-%d", f.calls), fmt.Sprintf("hash-%d", f.calls), nil
}

func mustDomainErr(t *testing.T, err error) *domain.Error {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("expected a domain error, got %T: %v", err, err)
	}
	return de
}

// ── Tests ────────────────────────────────────────────────────────────

// 创建成功时，原文密钥只在这一次响应里出现——这是整个 apikey 包存在的理由:
// 之后任何一次 List 都不该、也不能把它读回来。
func TestCreate_ReturnsRawKeyExactlyOnce(t *testing.T) {
	repo, gen := newFakeRepo(), &fakeGenerator{}
	svc := apikey.NewService(repo, gen)

	created, err := svc.Create(context.Background(), 1, "我的 CI 脚本")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.RawKey != "raw-1" {
		t.Fatalf("expected the generator's raw key to flow through, got %q", created.RawKey)
	}
	if created.Name != "我的 CI 脚本" {
		t.Fatalf("name not stored: %+v", created.APIKey)
	}

	list, err := svc.List(context.Background(), 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected the created key to show up in List: %+v", list)
	}
}

func TestCreate_RequiresAName(t *testing.T) {
	svc := apikey.NewService(newFakeRepo(), &fakeGenerator{})
	_, err := svc.Create(context.Background(), 1, "   ")
	de := mustDomainErr(t, err)
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 1 || de.Details[0].Field != "name" {
		t.Fatalf("expected a name field error, got %+v", de)
	}
}

// 上限只挡"活着"的 key：吊销过的不该占着这个名额，否则一个脚本反复轮换
// key 用几次就会把自己锁死。
func TestCreate_LimitCountsOnlyActiveKeys(t *testing.T) {
	repo, gen := newFakeRepo(), &fakeGenerator{}
	svc := apikey.NewService(repo, gen)
	ctx := context.Background()

	for i := 0; i < apikey.MaxKeysPerOwner; i++ {
		if _, err := svc.Create(ctx, 1, fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := svc.Create(ctx, 1, "one too many"); err == nil {
		t.Fatal("expected the limit to reject a key beyond MaxKeysPerOwner")
	}

	list, _ := svc.List(ctx, 1)
	if err := svc.Revoke(ctx, 1, list[0].ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Create(ctx, 1, "now there's room"); err != nil {
		t.Fatalf("expected room after revoking one, got: %v", err)
	}
}

// 一个账号的上限不该被另一个账号的 key 数量绊到。
func TestCreate_LimitIsPerOwner(t *testing.T) {
	repo, gen := newFakeRepo(), &fakeGenerator{}
	svc := apikey.NewService(repo, gen)
	ctx := context.Background()

	for i := 0; i < apikey.MaxKeysPerOwner; i++ {
		if _, err := svc.Create(ctx, 1, fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("create for owner 1: %v", err)
		}
	}
	if _, err := svc.Create(ctx, 2, "first key for a different owner"); err != nil {
		t.Fatalf("owner 2 should not be blocked by owner 1's count: %v", err)
	}
}

// Revoke 把"不存在""不是你的""已经吊销过"都收敛成同一个 404——调用方没理由
// 区分这三者。
func TestRevoke_NotFoundCoversWrongOwnerAndAlreadyRevoked(t *testing.T) {
	repo, gen := newFakeRepo(), &fakeGenerator{}
	svc := apikey.NewService(repo, gen)
	ctx := context.Background()

	created, err := svc.Create(ctx, 1, "mine")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Revoke(ctx, 2, created.ID); err == nil {
		t.Fatal("expected revoking someone else's key to fail")
	} else if de := mustDomainErr(t, err); de.Kind != domain.KindNotFound || de.Code != domain.CodeAPIKeyNotFound {
		t.Fatalf("wrong-owner revoke: kind=%v code=%d", de.Kind, de.Code)
	}

	if err := svc.Revoke(ctx, 1, created.ID); err != nil {
		t.Fatalf("first revoke should succeed: %v", err)
	}
	if err := svc.Revoke(ctx, 1, created.ID); err == nil {
		t.Fatal("expected revoking an already-revoked key to fail")
	} else if de := mustDomainErr(t, err); de.Kind != domain.KindNotFound {
		t.Fatalf("double revoke: kind=%v", de.Kind)
	}

	list, _ := svc.List(ctx, 1)
	if len(list) != 1 || list[0].Active() {
		t.Fatalf("expected the key to be listed but inactive after revoke: %+v", list)
	}
}

func TestRevoke_UnknownID(t *testing.T) {
	svc := apikey.NewService(newFakeRepo(), &fakeGenerator{})
	err := svc.Revoke(context.Background(), 1, 999)
	if err == nil {
		t.Fatal("expected an error for a nonexistent id")
	}
	de := mustDomainErr(t, err)
	if de.Kind != domain.KindNotFound {
		t.Fatalf("expected not-found, got kind=%v", de.Kind)
	}
}
