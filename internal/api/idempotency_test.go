package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIdempotencyStore is an in-memory IdempotencyStore for unit tests —
// the real PostgresIdempotencyStore is exercised indirectly via the
// store package's own tests and manual verification against a live DB.
type fakeIdempotencyStore struct {
	mu    sync.Mutex
	items map[string]struct {
		status int
		body   []byte
	}
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{items: make(map[string]struct {
		status int
		body   []byte
	})}
}

func (s *fakeIdempotencyStore) Get(_ context.Context, key string) (int, []byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[key]
	return v.status, v.body, ok, nil
}

func (s *fakeIdempotencyStore) Put(_ context.Context, key string, _ int64, status int, body []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = struct {
		status int
		body   []byte
	}{status, body}
	return nil
}

func TestIdempotencyMiddleware_ReplaysFirstResponse(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32

	handler := IdempotencyMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"run-1"},"request_id":"r1"}`))
	}))

	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
		r.Header.Set("Idempotency-Key", "key-123")
		return r.WithContext(WithUserID(r.Context(), 42))
	}

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, newReq())
	if w1.Code != http.StatusCreated {
		t.Fatalf("first request: got %d, want 201", w1.Code)
	}

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, newReq())
	if w2.Code != http.StatusCreated {
		t.Fatalf("replayed request: got %d, want 201", w2.Code)
	}
	if w2.Body.String() != w1.Body.String() {
		t.Fatalf("replayed body %q != original %q", w2.Body.String(), w1.Body.String())
	}
	if w2.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("expected Idempotency-Replayed header on the second response")
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler invoked %d times, want exactly 1 (second request must not re-run it)", got)
	}
}

func TestIdempotencyMiddleware_PassesThroughWithoutKeyOrUser(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// No Idempotency-Key header at all.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	r = r.WithContext(WithUserID(r.Context(), 42))
	handler.ServeHTTP(httptest.NewRecorder(), r)

	// Key present but no authenticated user.
	r2 := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	r2.Header.Set("Idempotency-Key", "key-456")
	handler.ServeHTTP(httptest.NewRecorder(), r2)

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("handler invoked %d times, want 2 (both requests should pass through)", got)
	}
}

func TestIdempotencyMiddleware_DoesNotCacheErrors(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
		r.Header.Set("Idempotency-Key", "key-err")
		return r.WithContext(WithUserID(r.Context(), 42))
	}

	handler.ServeHTTP(httptest.NewRecorder(), newReq())
	handler.ServeHTTP(httptest.NewRecorder(), newReq())

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("handler invoked %d times, want 2 (a failed request must be retryable)", got)
	}
}
