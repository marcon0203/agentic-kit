package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_BlocksAfterLimitAndSetsRetryAfter(t *testing.T) {
	limiter := NewRateLimiter(5, time.Minute, func(*http.Request) string { return "fixed-key" })
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200 (within limit)", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: got status %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After header")
	}
}

func TestRateLimiter_DifferentKeysGetIndependentBuckets(t *testing.T) {
	callerA := true
	limiter := NewRateLimiter(1, time.Minute, func(*http.Request) string {
		if callerA {
			return "a"
		}
		return "b"
	})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("caller a first request: got %d, want 200", w1.Code)
	}

	callerA = false
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("caller b first request: got %d, want 200 (independent bucket)", w2.Code)
	}
}

func TestRemoteAddrKey_StripsPort(t *testing.T) {
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.RemoteAddr = "127.0.0.1:54321"
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "127.0.0.1:9999"

	if RemoteAddrKey(r1) != RemoteAddrKey(r2) {
		t.Fatalf("expected same key for same host different ports, got %q vs %q", RemoteAddrKey(r1), RemoteAddrKey(r2))
	}
	if RemoteAddrKey(r1) != "127.0.0.1" {
		t.Fatalf("expected bare host, got %q", RemoteAddrKey(r1))
	}
}
