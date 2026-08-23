package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/adapter/password"
	"github.com/marcon0203/agentic-kit/internal/auth"
	"github.com/marcon0203/agentic-kit/internal/domain/iam"
)

// The IAM rules — validation, the duplicate-email conflict, and that a
// wrong password and an unregistered address are indistinguishable — are
// tested against the service in internal/domain/iam. This covers
// transport: the AuthResult shape, and that no response carries a hash.

type fakeUserRepo struct {
	byEmail map[string]iam.User
	nextID  int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byEmail: map[string]iam.User{}, nextID: 1}
}

func (f *fakeUserRepo) Create(_ context.Context, email, passwordHash, displayName string) (iam.User, error) {
	if _, exists := f.byEmail[email]; exists {
		return iam.User{}, iam.ErrEmailTaken
	}
	u := iam.User{ID: f.nextID, Email: email, PasswordHash: passwordHash, DisplayName: displayName, CreatedAt: time.Now()}
	f.nextID++
	f.byEmail[email] = u
	return u, nil
}

func (f *fakeUserRepo) ByEmail(_ context.Context, email string) (iam.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return iam.User{}, iam.ErrNotFound
	}
	return u, nil
}

// The real argon2id hasher is used rather than a stub: these tests are
// cheap enough to afford it, and it keeps the register-then-login path
// exercising the same verification production does.
func newAuthHandlersForTest(t *testing.T) *AuthHandlers {
	t.Helper()
	hasher, err := password.NewHasher()
	if err != nil {
		t.Fatalf("hasher: %v", err)
	}
	return NewAuthHandlers(iam.NewService(newFakeUserRepo(), hasher, auth.NewTokenIssuer("test-secret")))
}

func doJSON(t *testing.T, handler http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func decodeAuthResult(t *testing.T, w *httptest.ResponseRecorder) authResultDTO {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var dto authResultDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return dto
}

func TestRegister_ResponseShape(t *testing.T) {
	h := newAuthHandlersForTest(t)

	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register",
		registerRequest{Email: "a@example.com", Password: "Passw0rd!123", DisplayName: "A"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	dto := decodeAuthResult(t, w)
	if dto.AccessToken == "" || dto.RefreshToken == "" {
		t.Fatalf("expected both tokens: %+v", dto)
	}
	// ID is a string on the wire so a large id survives JS's Number.
	if dto.User.ID != "1" || dto.User.Email != "a@example.com" || dto.User.DisplayName != "A" {
		t.Fatalf("unexpected user: %+v", dto.User)
	}
	if dto.User.IsAdmin {
		t.Fatal("a newly registered user must not be an admin")
	}
}

// A password hash must never be serialisable — checked against the raw
// body rather than the DTO, since the DTO having no field is exactly what
// this is confirming.
func TestRegister_ResponseCarriesNoPasswordMaterial(t *testing.T) {
	h := newAuthHandlersForTest(t)

	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register",
		registerRequest{Email: "a@example.com", Password: "Passw0rd!123", DisplayName: "A"})

	body := w.Body.String()
	if bytes.Contains(w.Body.Bytes(), []byte("Passw0rd!123")) {
		t.Fatalf("the password came back in the response: %s", body)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("argon2id")) {
		t.Fatalf("a password hash came back in the response: %s", body)
	}
}

func TestRegister_DuplicateEmailReturns409(t *testing.T) {
	h := newAuthHandlersForTest(t)
	req := registerRequest{Email: "dup@example.com", Password: "Passw0rd!123", DisplayName: "Dup"}

	if w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", req); w.Code != http.StatusCreated {
		t.Fatalf("first register: %d: %s", w.Code, w.Body.String())
	}
	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", req)
	if w.Code != http.StatusConflict || !containsCode(w.Body.String(), ErrEmailAlreadyRegistered) {
		t.Fatalf("expected 409/20005, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_ShortPasswordReturns400WithDetails(t *testing.T) {
	h := newAuthHandlersForTest(t)

	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register",
		registerRequest{Email: "a@example.com", Password: "short", DisplayName: "A"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Details) == 0 {
		t.Fatal("expected field-level details")
	}
}

func TestRegister_MalformedBodyReturns400(t *testing.T) {
	h := newAuthHandlersForTest(t)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString("{not json"))
	w := httptest.NewRecorder()
	h.Register(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestLogin_Success(t *testing.T) {
	h := newAuthHandlersForTest(t)
	doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register",
		registerRequest{Email: "a@example.com", Password: "Passw0rd!123", DisplayName: "A"})

	w := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login",
		loginRequest{Email: "a@example.com", Password: "Passw0rd!123"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if dto := decodeAuthResult(t, w); dto.AccessToken == "" || dto.User.Email != "a@example.com" {
		t.Fatalf("unexpected session: %+v", dto)
	}
}

// The response to a wrong password and to an unregistered address must be
// byte-identical apart from the request id, or the difference is itself an
// answer to "does this account exist".
func TestLogin_FailuresAreIndistinguishable(t *testing.T) {
	h := newAuthHandlersForTest(t)
	doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register",
		registerRequest{Email: "a@example.com", Password: "Passw0rd!123", DisplayName: "A"})

	wrongPassword := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login",
		loginRequest{Email: "a@example.com", Password: "wrong-password"})
	unknownEmail := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login",
		loginRequest{Email: "nobody@example.com", Password: "wrong-password"})

	if wrongPassword.Code != http.StatusUnauthorized || unknownEmail.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for both, got %d and %d", wrongPassword.Code, unknownEmail.Code)
	}

	var a, b Envelope
	_ = json.Unmarshal(wrongPassword.Body.Bytes(), &a)
	_ = json.Unmarshal(unknownEmail.Body.Bytes(), &b)
	if a.Code != b.Code || a.Message != b.Message {
		t.Fatalf("the two failures are distinguishable:\n  wrong password: %d %q\n  unknown email:  %d %q",
			a.Code, a.Message, b.Code, b.Message)
	}
	if a.Code != ErrInvalidCredentials {
		t.Fatalf("expected 20006, got %d", a.Code)
	}
}
