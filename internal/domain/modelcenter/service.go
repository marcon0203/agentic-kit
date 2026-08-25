package modelcenter

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Repository persists provider credentials. Store takes the ciphertext,
// never the plaintext key: encryption happens before this port is reached,
// so no implementation is ever handed a secret in the clear.
type Repository interface {
	ListForOwner(ctx context.Context, ownerID int64) ([]Provider, error)
	Store(ctx context.Context, ownerID int64, provider, ciphertext, baseURL string) (Provider, error)
}

// CredentialCipher encrypts a credential for storage.
type CredentialCipher interface {
	Encrypt(plaintext string) (string, error)
}

// ConnectivityChecker proves a credential actually authenticates against
// the provider it claims. ErrUnknownProvider means the name isn't one this
// platform can reach at all.
type ConnectivityChecker interface {
	Check(ctx context.Context, provider, apiKey, baseURL string) error
}

// ErrUnknownProvider is the port contract for an unrecognised provider
// name — distinct from "the credential was rejected", because the two are
// a different kind of mistake and get a different response.
var ErrUnknownProvider = errors.New("modelcenter: unknown provider")

// UsageReader reads accumulated usage for one user.
type UsageReader interface {
	Summary(ctx context.Context, userID int64, since time.Time) (int64, float64, int32, error)
	BreakdownByBundle(ctx context.Context, userID int64, since time.Time) ([]UsageBucket, error)
	BreakdownByDay(ctx context.Context, userID int64, since time.Time) ([]UsageBucket, error)
}

// Service is the 模型中心 application service.
type Service struct {
	repo   Repository
	cipher CredentialCipher
	check  ConnectivityChecker
	usage  UsageReader
	now    func() time.Time
}

func NewService(repo Repository, cipher CredentialCipher, check ConnectivityChecker, usage UsageReader) *Service {
	return &Service{repo: repo, cipher: cipher, check: check, usage: usage, now: time.Now}
}

// List returns the caller's registered providers.
func (s *Service) List(ctx context.Context, ownerID int64) ([]Provider, error) {
	rows, err := s.repo.ListForOwner(ctx, ownerID)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if rows == nil {
		rows = []Provider{}
	}
	return rows, nil
}

// Register validates a credential against the real provider before storing
// it (spec-09's "Provider 接入时的连通性校验"), then encrypts it.
//
// Checking first is the point: a key that does not work is a
// configuration mistake the user can fix now, while a stored broken key
// only surfaces later as a failed run, where the cause is much harder to
// see. The plaintext lives no longer than this call — it is never logged
// and never returned.
func (s *Service) Register(ctx context.Context, ownerID int64, provider, apiKey, baseURL string) (Provider, error) {
	var errs []domain.FieldError
	if provider == "" {
		errs = append(errs, domain.FieldError{Field: "provider", Reason: "required"})
	}
	if apiKey == "" {
		errs = append(errs, domain.FieldError{Field: "api_key", Reason: "required"})
	}
	// "custom" has no documented endpoint of its own — unlike every named
	// provider, there is nothing to default to, so the caller must supply
	// one.
	if provider == "custom" && baseURL == "" {
		errs = append(errs, domain.FieldError{Field: "base_url", Reason: "required for custom provider"})
	}
	if len(errs) > 0 {
		return Provider{}, domain.Invalid(domain.CodeValidationFailed, "invalid request").WithDetails(errs...)
	}

	if err := s.check.Check(ctx, provider, apiKey, baseURL); err != nil {
		if errors.Is(err, ErrUnknownProvider) {
			return Provider{}, domain.Invalid(domain.CodeValidationFailed, "invalid request").
				WithDetails(domain.FieldError{Field: "provider", Reason: "must be one of " + strings.Join(KnownProviders(), ", ")})
		}
		// The checker's message describes the rejection, not the key —
		// it is written to be safe to show.
		return Provider{}, domain.Unprocessable(domain.CodeProviderCredsInvalid, "凭证无效："+err.Error())
	}

	ciphertext, err := s.cipher.Encrypt(apiKey)
	if err != nil {
		return Provider{}, domain.Internal(err)
	}

	stored, err := s.repo.Store(ctx, ownerID, provider, ciphertext, baseURL)
	if err != nil {
		return Provider{}, domain.Internal(err)
	}
	return stored, nil
}

// UsageQuery selects a usage report.
type UsageQuery struct {
	Period  string
	GroupBy string
}

// Usage reports what the caller has spent.
//
// Always scoped to the caller's own triggered runs — spec-09's "黑盒资源的
// 用量算订阅者的": someone running another author's published Bundle pays
// for it themselves, so the usage lands on the person who started the run
// rather than on whoever wrote it.
func (s *Service) Usage(ctx context.Context, userID int64, q UsageQuery) (UsageReport, error) {
	period, ok := ParsePeriod(q.Period)
	if !ok {
		return UsageReport{}, domain.Invalid(domain.CodeValidationFailed, "invalid period")
	}
	groupBy, ok := ParseGroupBy(q.GroupBy)
	if !ok {
		return UsageReport{}, domain.Invalid(domain.CodeValidationFailed, "invalid group_by")
	}

	window := WindowFor(s.now(), period)

	tokens, cost, runCount, err := s.usage.Summary(ctx, userID, window.Since)
	if err != nil {
		return UsageReport{}, domain.Internal(err)
	}

	var breakdown []UsageBucket
	if groupBy == GroupByBundle {
		breakdown, err = s.usage.BreakdownByBundle(ctx, userID, window.Since)
	} else {
		breakdown, err = s.usage.BreakdownByDay(ctx, userID, window.Since)
	}
	if err != nil {
		return UsageReport{}, domain.Internal(err)
	}
	if breakdown == nil {
		breakdown = []UsageBucket{}
	}

	return UsageReport{
		Period: window.Label, TotalTokens: tokens, TotalCostUSD: cost,
		RunCount: runCount, Breakdown: breakdown,
	}, nil
}
