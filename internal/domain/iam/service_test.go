package iam_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/iam"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeRepo struct {
	byEmail map[string]iam.User
	nextID  int64
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byEmail: map[string]iam.User{}, nextID: 1} }

func (f *fakeRepo) Create(_ context.Context, email, passwordHash, displayName string) (iam.User, error) {
	if _, taken := f.byEmail[email]; taken {
		return iam.User{}, iam.ErrEmailTaken
	}
	u := iam.User{ID: f.nextID, Email: email, PasswordHash: passwordHash, DisplayName: displayName, CreatedAt: time.Now()}
	f.nextID++
	f.byEmail[email] = u
	return u, nil
}

func (f *fakeRepo) ByEmail(_ context.Context, email string) (iam.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return iam.User{}, iam.ErrNotFound
	}
	return u, nil
}

// countingHasher records every verification so a test can assert that a
// sign-in for an unknown address still did the work — the property that
// makes the two paths indistinguishable.
type countingHasher struct {
	verified []string
	hashErr  error
}

const dummy = "hash:__dummy__"

func (h *countingHasher) Hash(password string) (string, error) {
	if h.hashErr != nil {
		return "", h.hashErr
	}
	return "hash:" + password, nil
}

func (h *countingHasher) Verify(password, hash string) (bool, error) {
	h.verified = append(h.verified, hash)
	return "hash:"+password == hash, nil
}

func (h *countingHasher) DummyHash() string { return dummy }

type fakeTokens struct{ err error }

func (f fakeTokens) IssueAccessToken(userID int64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "access-for-user", nil
}

func (f fakeTokens) IssueRefreshToken(userID int64) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "refresh-for-user", nil
}

type harness struct {
	svc    *iam.Service
	repo   *fakeRepo
	hasher *countingHasher
}

func newHarness() *harness {
	h := &harness{repo: newFakeRepo(), hasher: &countingHasher{}}
	h.svc = iam.NewService(h.repo, h.hasher, fakeTokens{})
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

func validRegistration() iam.RegisterCommand {
	return iam.RegisterCommand{Email: "a@example.com", Password: "Passw0rd!123", DisplayName: "A"}
}

// ── Registration ─────────────────────────────────────────────────────

func TestRegister_SignsTheNewUserIn(t *testing.T) {
	h := newHarness()

	session, err := h.svc.Register(context.Background(), validRegistration())
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("expected both tokens: %+v", session)
	}
	if session.User.Email != "a@example.com" || session.User.ID == 0 {
		t.Fatalf("unexpected profile: %+v", session.User)
	}
}

func TestRegister_ReportsEveryFieldProblemAtOnce(t *testing.T) {
	h := newHarness()

	de := mustDomainErr(t, func() error {
		_, err := h.svc.Register(context.Background(), iam.RegisterCommand{Password: "short"})
		return err
	}())
	if de.Code != domain.CodeValidationFailed || len(de.Details) != 3 {
		t.Fatalf("expected email, password and display_name reported together, got %+v", de.Details)
	}
}

func TestRegister_ShortPasswordIsRejected(t *testing.T) {
	h := newHarness()
	cmd := validRegistration()
	cmd.Password = strings.Repeat("x", iam.MinPasswordLength-1)

	de := mustDomainErr(t, func() error { _, err := h.svc.Register(context.Background(), cmd); return err }())
	if len(de.Details) != 1 || de.Details[0].Field != "password" {
		t.Fatalf("expected a password field error, got %+v", de.Details)
	}
}

func TestRegister_DuplicateEmailConflicts(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), validRegistration()); err != nil {
		t.Fatalf("first register: %v", err)
	}

	de := mustDomainErr(t, func() error { _, err := h.svc.Register(context.Background(), validRegistration()); return err }())
	if de.Kind != domain.KindConflict || de.Code != domain.CodeEmailAlreadyRegistered {
		t.Fatalf("expected 409/20005, got kind=%v code=%d", de.Kind, de.Code)
	}
}

func TestRegister_PasswordIsHashedBeforeStorage(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), validRegistration()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if stored := h.repo.byEmail["a@example.com"].PasswordHash; stored == "Passw0rd!123" {
		t.Fatal("the plaintext password reached storage")
	}
}

func TestRegister_HashFailureStoresNothing(t *testing.T) {
	h := newHarness()
	h.hasher.hashErr = errors.New("kdf unavailable")

	de := mustDomainErr(t, func() error { _, err := h.svc.Register(context.Background(), validRegistration()); return err }())
	if de.Kind != domain.KindInternal {
		t.Fatalf("expected an internal error, got kind=%v", de.Kind)
	}
	if len(h.repo.byEmail) != 0 {
		t.Fatal("no account may be created when hashing failed")
	}
}

// ── Sign-in ──────────────────────────────────────────────────────────

func TestLogin_Succeeds(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), validRegistration()); err != nil {
		t.Fatalf("register: %v", err)
	}

	session, err := h.svc.Login(context.Background(), "a@example.com", "Passw0rd!123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if session.AccessToken == "" || session.User.Email != "a@example.com" {
		t.Fatalf("unexpected session: %+v", session)
	}
}

// The two ways to fail must be indistinguishable from outside: same code,
// same message, same kind.
func TestLogin_WrongPasswordAndUnknownEmailAreTheSameResponse(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Register(context.Background(), validRegistration()); err != nil {
		t.Fatalf("register: %v", err)
	}

	wrongPassword := mustDomainErr(t, func() error {
		_, err := h.svc.Login(context.Background(), "a@example.com", "not-the-password")
		return err
	}())
	unknownEmail := mustDomainErr(t, func() error {
		_, err := h.svc.Login(context.Background(), "nobody@example.com", "not-the-password")
		return err
	}())

	if wrongPassword.Code != domain.CodeInvalidCredentials || wrongPassword.Kind != domain.KindUnauthorized {
		t.Fatalf("wrong password: expected 401/20006, got kind=%v code=%d", wrongPassword.Kind, wrongPassword.Code)
	}
	if unknownEmail.Code != wrongPassword.Code || unknownEmail.Message != wrongPassword.Message || unknownEmail.Kind != wrongPassword.Kind {
		t.Fatalf("the two failures are distinguishable:\n  wrong password: %+v\n  unknown email:  %+v", wrongPassword, unknownEmail)
	}
}

// An early return for an unknown address would be measurably faster than a
// wrong password, which is enough to enumerate who has an account. So the
// KDF runs either way, against a dummy hash when there is no user.
func TestLogin_UnknownEmailStillVerifiesAgainstADummyHash(t *testing.T) {
	h := newHarness()

	if _, err := h.svc.Login(context.Background(), "nobody@example.com", "whatever"); err == nil {
		t.Fatal("expected the sign-in to fail")
	}
	if len(h.hasher.verified) != 1 {
		t.Fatalf("expected exactly one verification, got %d", len(h.hasher.verified))
	}
	if h.hasher.verified[0] != dummy {
		t.Fatalf("verified against %q, want the dummy hash", h.hasher.verified[0])
	}
}

func TestLogin_StorageFailureIsNotACredentialError(t *testing.T) {
	h := newHarness()
	h.svc = iam.NewService(errorRepo{}, h.hasher, fakeTokens{})

	de := mustDomainErr(t, func() error {
		_, err := h.svc.Login(context.Background(), "a@example.com", "Passw0rd!123")
		return err
	}())
	if de.Kind != domain.KindInternal {
		t.Fatalf("a storage failure must not be reported as bad credentials, got kind=%v", de.Kind)
	}
}

type errorRepo struct{}

func (errorRepo) Create(context.Context, string, string, string) (iam.User, error) {
	return iam.User{}, errors.New("storage down")
}

func (errorRepo) ByEmail(context.Context, string) (iam.User, error) {
	return iam.User{}, errors.New("storage down")
}

// ── Profile ──────────────────────────────────────────────────────────

// Profile is what every response describes a user with, and it has no
// password-hash field — so a hash cannot be serialised by forgetting to
// strip it.
func TestProfile_CarriesNoPasswordHash(t *testing.T) {
	user := iam.User{ID: 1, Email: "a@example.com", PasswordHash: "hash:secret", DisplayName: "A", IsAdmin: true}
	profile := user.Profile()

	if profile.Email != "a@example.com" || !profile.IsAdmin {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	// The compiler enforces the rest: Profile has no field to hold it.
}

func TestSession_TokenIssueFailureIsInternal(t *testing.T) {
	h := newHarness()
	h.svc = iam.NewService(h.repo, h.hasher, fakeTokens{err: errors.New("signing key missing")})

	de := mustDomainErr(t, func() error { _, err := h.svc.Register(context.Background(), validRegistration()); return err }())
	if de.Kind != domain.KindInternal {
		t.Fatalf("expected an internal error, got kind=%v", de.Kind)
	}
}
