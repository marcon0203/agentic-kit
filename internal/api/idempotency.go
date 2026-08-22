package api

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// IdempotencyTTL matches 架构设计文档 6.6: a repeated request with the same
// key inside this window returns the first response instead of re-running
// the (possibly token-expensive) operation.
const IdempotencyTTL = 24 * time.Hour

// IdempotencyStore persists the first response for a given key so a retried
// request can be answered without re-executing the handler. Backed by
// internal/store's idempotency_keys table in production; swappable for
// tests.
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (statusCode int, body []byte, found bool, err error)
	Put(ctx context.Context, key string, ownerUserID int64, statusCode int, body []byte, ttl time.Duration) error
}

// IdempotencyMiddleware short-circuits a repeated POST carrying the same
// Idempotency-Key header from the same user within IdempotencyMiddleware's
// TTL, replaying the first response verbatim. Requests with no
// authenticated user or no header pass through unchanged — idempotency
// only makes sense for the authenticated create endpoints that use it.
func IdempotencyMiddleware(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			userID, hasUser := UserIDFromContext(r.Context())
			if key == "" || !hasUser || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			if status, body, found, err := store.Get(r.Context(), key); err == nil && found {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(status)
				_, _ = w.Write(body)
				return
			}

			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK, body: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)

			// Only cache genuine successes: a retried request that failed
			// (e.g. transient 500) should be free to try again for real.
			if rec.status >= 200 && rec.status < 300 {
				_ = store.Put(r.Context(), key, userID, rec.status, rec.body.Bytes(), IdempotencyTTL)
			}
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
