package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// tokenBucket is a lazily-refilled token bucket: no background goroutine,
// tokens are computed from elapsed time on each Allow call.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newTokenBucket(capacity float64, refillRate float64) *tokenBucket {
	return &tokenBucket{tokens: capacity, capacity: capacity, refillRate: refillRate, lastRefill: time.Now()}
}

// allow reports whether a request may proceed, and if not, how long the
// caller should wait before retrying.
func (b *tokenBucket) allow() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now
	b.tokens = min(b.capacity, b.tokens+elapsed*b.refillRate)

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / b.refillRate * float64(time.Second))
	return false, wait
}

// RateLimiter is a per-key token bucket limiter, e.g. 600/min per user
// (architecture doc 架构设计文档 六) or 20/min scoped to run-creation.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	limit   int
	window  time.Duration
	keyFunc func(*http.Request) string
}

// NewRateLimiter builds a limiter allowing `limit` requests per `window`,
// keyed per-request by keyFunc (e.g. authenticated user ID, falling back to
// remote IP before auth is wired in).
func NewRateLimiter(limit int, window time.Duration, keyFunc func(*http.Request) string) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		limit:   limit,
		window:  window,
		keyFunc: keyFunc,
	}
}

func (rl *RateLimiter) bucketFor(key string) *tokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		refillRate := float64(rl.limit) / rl.window.Seconds()
		b = newTokenBucket(float64(rl.limit), refillRate)
		rl.buckets[key] = b
	}
	return b
}

// Middleware returns an http middleware enforcing this limiter. On the
// limit being hit it writes 429 + ErrRateLimited with a Retry-After header,
// per 架构设计文档's rate-limit acceptance criteria.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := rl.keyFunc(r)
		ok, wait := rl.bucketFor(key).allow()
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+1)))
			writeErr(w, r, http.StatusTooManyRequests, ErrRateLimited,
				fmt.Sprintf("rate limit exceeded, retry after %.0fs", wait.Seconds()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RemoteAddrKey is a keyFunc fallback for use before authentication is
// wired into the request context (spec-04). It strips the ephemeral client
// port — r.RemoteAddr includes it, and keying on the full "ip:port" would
// give every request its own bucket, defeating the limiter entirely.
func RemoteAddrKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
