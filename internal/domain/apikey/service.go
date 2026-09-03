package apikey

import (
	"context"
	"errors"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// ErrNotFound is returned by Revoke when the key does not exist, is not
// this owner's, or is already revoked — Repository collapses all three
// into one sentinel because none of them is this caller's business to
// tell apart from the others.
var ErrNotFound = errors.New("apikey: not found")

// Repository persists API keys, scoped to the owner on every call — there
// is no cross-user listing or lookup by id in this package, only "my own
// keys".
type Repository interface {
	Create(ctx context.Context, ownerID int64, name, keyHash string) (APIKey, error)
	ListForOwner(ctx context.Context, ownerID int64) ([]APIKey, error)
	// Revoke reports ErrNotFound rather than a bool: RowsAffected()==0
	// covers "wrong owner", "already revoked" and "no such id" alike, and
	// the caller (a 404 either way) has no reason to distinguish them.
	Revoke(ctx context.Context, ownerID, keyID int64) error
}

// Generator mints a new raw key and its hash. A port because the exact
// scheme (length, prefix, encoding) is infrastructure — internal/auth
// already has one (GenerateAPIKey), this just keeps that dependency out
// of the domain package per the layering rule every other context here
// follows.
type Generator interface {
	GenerateAPIKey() (rawKey, hash string, err error)
}

// MaxKeysPerOwner bounds how many live (non-revoked) keys one account can
// hold at once. Not a security control — a leaked key is still just as
// dangerous — but an unbounded row count per user is its own kind of
// footgun (a forgotten script re-creating a key on every run), and a cap
// forces a "revoke the one you don't use" prompt instead.
const MaxKeysPerOwner = 20

// Service is the apikey application service.
type Service struct {
	repo Repository
	gen  Generator
}

func NewService(repo Repository, gen Generator) *Service {
	return &Service{repo: repo, gen: gen}
}

// Create mints a new key for ownerID. The raw value is returned exactly
// once, in Created.RawKey — it is not retrievable through List afterward.
func (s *Service) Create(ctx context.Context, ownerID int64, name string) (Created, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Created{}, domain.Invalid(domain.CodeValidationFailed, "invalid request").
			WithDetails(domain.FieldError{Field: "name", Reason: "required"})
	}

	existing, err := s.repo.ListForOwner(ctx, ownerID)
	if err != nil {
		return Created{}, domain.Internal(err)
	}
	live := 0
	for _, k := range existing {
		if k.Active() {
			live++
		}
	}
	if live >= MaxKeysPerOwner {
		return Created{}, domain.Invalid(domain.CodeValidationFailed, "已达到可创建的 API Key 数量上限，先吊销一个不用的").
			WithDetails(domain.FieldError{Field: "name", Reason: "limit reached"})
	}

	rawKey, hash, err := s.gen.GenerateAPIKey()
	if err != nil {
		return Created{}, domain.Internal(err)
	}
	created, err := s.repo.Create(ctx, ownerID, name, hash)
	if err != nil {
		return Created{}, domain.Internal(err)
	}
	return Created{APIKey: created, RawKey: rawKey}, nil
}

// List returns ownerID's own keys, newest first — never the raw value or
// the hash, only what Repository.ListForOwner already redacts down to.
func (s *Service) List(ctx context.Context, ownerID int64) ([]APIKey, error) {
	keys, err := s.repo.ListForOwner(ctx, ownerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if keys == nil {
		keys = []APIKey{}
	}
	return keys, nil
}

// Revoke disables a key immediately. There is no "un-revoke" — the same
// one-way door GitHub/Stripe use, since a key that has already left this
// process (embedded in a script, pasted into a CI secret) can't be
// un-leaked by flipping a flag back.
func (s *Service) Revoke(ctx context.Context, ownerID, keyID int64) error {
	err := s.repo.Revoke(ctx, ownerID, keyID)
	if errors.Is(err, ErrNotFound) {
		return domain.NotFound(domain.CodeAPIKeyNotFound, "未找到该 API Key")
	}
	if err != nil {
		return domain.Internal(err)
	}
	return nil
}
