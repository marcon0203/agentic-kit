package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

// fakeAuthUserStore is an in-memory AuthUserStore for handler tests — the
// real PostgresAuthUserStore (unique-violation -> ErrEmailTaken mapping in
// particular) is verified against a live Postgres separately.
type fakeAuthUserStore struct {
	byEmail map[string]AuthUser
	nextID  int64
}

func newFakeAuthUserStore() *fakeAuthUserStore {
	return &fakeAuthUserStore{byEmail: map[string]AuthUser{}, nextID: 1}
}

func (s *fakeAuthUserStore) CreateUser(_ context.Context, email, passwordHash, displayName string) (AuthUser, error) {
	if _, exists := s.byEmail[email]; exists {
		return AuthUser{}, ErrEmailTaken
	}
	u := AuthUser{ID: s.nextID, Email: email, PasswordHash: passwordHash, DisplayName: displayName, CreatedAt: time.Now()}
	s.nextID++
	s.byEmail[email] = u
	return u, nil
}

func (s *fakeAuthUserStore) GetUserByEmail(_ context.Context, email string) (AuthUser, bool, error) {
	u, ok := s.byEmail[email]
	return u, ok, nil
}

func newAuthHandlersForTest() *AuthHandlers {
	return NewAuthHandlers(newFakeAuthUserStore(), auth.NewTokenIssuer("test-secret"))
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

func TestRegister_Success(t *testing.T) {
	h := newAuthHandlersForTest()
	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "new@example.com", Password: "password123", DisplayName: "New User",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Code != 0 {
		t.Fatalf("code = %d, want 0: %s", env.Code, w.Body.String())
	}
}

func TestRegister_DuplicateEmailReturns409(t *testing.T) {
	h := newAuthHandlersForTest()
	req := registerRequest{Email: "dup@example.com", Password: "password123", DisplayName: "First"}

	w1 := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", req)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first register: got %d, want 201: %s", w1.Code, w1.Body.String())
	}

	w2 := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", req)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate register: got %d, want 409: %s", w2.Code, w2.Body.String())
	}
	if !containsCode(w2.Body.String(), ErrEmailAlreadyRegistered) {
		t.Fatalf("body should carry ErrEmailAlreadyRegistered: %s", w2.Body.String())
	}
}

func TestRegister_ShortPasswordReturns400WithDetails(t *testing.T) {
	h := newAuthHandlersForTest()
	w := doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "short@example.com", Password: "short", DisplayName: "X",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Details) == 0 {
		t.Fatal("expected field-level details for a too-short password")
	}
}

func TestLogin_Success(t *testing.T) {
	h := newAuthHandlersForTest()
	doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "login@example.com", Password: "password123", DisplayName: "Login User",
	})

	w := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login", loginRequest{
		Email: "login@example.com", Password: "password123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataBytes, _ := json.Marshal(env.Data)
	var result authResultDTO
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("expected both tokens to be set: %+v", result)
	}
	if result.User.Email != "login@example.com" {
		t.Fatalf("user.email = %q, want login@example.com", result.User.Email)
	}
}

func TestLogin_WrongPasswordReturns401(t *testing.T) {
	h := newAuthHandlersForTest()
	doJSON(t, h.Register, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "wrongpw@example.com", Password: "password123", DisplayName: "X",
	})

	w := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login", loginRequest{
		Email: "wrongpw@example.com", Password: "not-the-password",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestLogin_UnknownEmailReturns401NotLeakingExistence(t *testing.T) {
	h := newAuthHandlersForTest()
	w := doJSON(t, h.Login, http.MethodPost, "/api/v1/auth/login", loginRequest{
		Email: "nobody@example.com", Password: "whatever123",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
}
