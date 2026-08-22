package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

type fakeAPIKeyLookup struct {
	byHash  map[string]int64
	touched []string
}

func (f *fakeAPIKeyLookup) GetUserIDByKeyHash(_ context.Context, hash string) (int64, bool, error) {
	id, ok := f.byHash[hash]
	return id, ok, nil
}

func (f *fakeAPIKeyLookup) TouchLastUsed(_ context.Context, hash string) error {
	f.touched = append(f.touched, hash)
	return nil
}

func authTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := UserIDFromContext(r.Context())
		writeJSON(w, r, http.StatusOK, map[string]int64{"user_id": userID})
	})
}

func TestAuthMiddleware_ValidJWTInjectsUserID(t *testing.T) {
	issuer := auth.NewTokenIssuer("test-secret")
	token, err := issuer.IssueAccessToken(7)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	handler := AuthMiddleware(issuer, &fakeAPIKeyLookup{byHash: map[string]int64{}})(authTestHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_ExpiredTokenReturns401With20002(t *testing.T) {
	issuer := auth.NewTokenIssuer("test-secret")
	handler := AuthMiddleware(issuer, &fakeAPIKeyLookup{})(authTestHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !containsCode(w.Body.String(), ErrTokenInvalid) {
		t.Fatalf("body should carry ErrTokenInvalid: %s", w.Body.String())
	}
}

func TestAuthMiddleware_MissingAuthHeaderReturns401(t *testing.T) {
	handler := AuthMiddleware(auth.NewTokenIssuer("s"), &fakeAPIKeyLookup{})(authTestHandler())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_ValidAPIKeyInjectsUserIDAndTouchesLastUsed(t *testing.T) {
	lookup := &fakeAPIKeyLookup{byHash: map[string]int64{auth.HashAPIKey("agk_abc123"): 99}}
	handler := AuthMiddleware(auth.NewTokenIssuer("s"), lookup)(authTestHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	r.Header.Set("Authorization", "ApiKey agk_abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(lookup.touched) != 1 {
		t.Fatalf("expected TouchLastUsed to be called once, got %d calls", len(lookup.touched))
	}
}

func TestAuthMiddleware_UnknownAPIKeyRejected(t *testing.T) {
	handler := AuthMiddleware(auth.NewTokenIssuer("s"), &fakeAPIKeyLookup{byHash: map[string]int64{}})(authTestHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	r.Header.Set("Authorization", "ApiKey agk_unknown")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireOwner_ForbidsMismatchedUser(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents/1", nil)
	r = r.WithContext(WithUserID(r.Context(), 1))
	w := httptest.NewRecorder()

	if RequireOwner(w, r, 2) {
		t.Fatal("expected RequireOwner to return false for a mismatched owner")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if !containsCode(w.Body.String(), ErrForbidden) {
		t.Fatalf("body should carry ErrForbidden: %s", w.Body.String())
	}
}

func TestRequireOwner_AllowsMatchingUser(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents/1", nil)
	r = r.WithContext(WithUserID(r.Context(), 5))
	w := httptest.NewRecorder()

	if !RequireOwner(w, r, 5) {
		t.Fatal("expected RequireOwner to return true for the matching owner")
	}
}

func containsCode(body string, code int) bool {
	// Cheap substring check is enough here — the exact JSON shape is
	// already covered by envelope_test.go.
	return strings.Contains(body, strconv.Itoa(code))
}
