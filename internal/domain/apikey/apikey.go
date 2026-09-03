// Package apikey is 系统配置 → API Key 管理: the personal access tokens a
// user mints for themselves to call this platform's own HTTP API as
// something other than a logged-in browser — a CI job, a script, or a
// third party's server integrating a published app (架构设计文档 6.5's
// ApiKeyAuth security scheme, running in parallel with JWT).
//
// The run path itself needs nothing new for this: POST /runs already
// accepts either auth scheme via AuthMiddleware, and a subscriber can
// already run a published Bundle listing by its ref (run.BundleResolver).
// What was missing was simply a way for a user to mint one of these keys
// at all — the api_keys table and its lookup existed, but nothing let a
// caller create a row in it.
package apikey

import "time"

// APIKey is one issued key, as every read describes it: never the raw
// key (shown once at creation, see Created) and never even the hash —
// those stay behind Repository.
type APIKey struct {
	ID         int64
	Name       string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

func (k APIKey) Active() bool { return k.RevokedAt == nil }

// Created is the one-time response to a successful Create: the raw key
// exists only in this value and is never recoverable afterwards — the
// same model GitHub/Stripe personal access tokens use.
type Created struct {
	APIKey
	RawKey string
}
