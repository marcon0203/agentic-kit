package iam

import (
	"context"
	"errors"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Port sentinels.
var (
	ErrEmailTaken = errors.New("iam: email already registered")
	ErrNotFound   = errors.New("iam: no such user")
)

// Repository persists accounts.
type Repository interface {
	Create(ctx context.Context, email, passwordHash, displayName string) (User, error)
	ByEmail(ctx context.Context, email string) (User, error)
}

// PasswordHasher hashes and verifies passwords. A port because which KDF
// is used is infrastructure; that passwords are never stored in the clear
// is not.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) (bool, error)
	// DummyHash is a valid hash of a value nobody will ever enter. Sign-in
	// verifies against it when no such account exists, so a request for an
	// unregistered address costs the same as a wrong password.
	DummyHash() string
}

// TokenIssuer mints the access and refresh tokens a session carries.
type TokenIssuer interface {
	IssueAccessToken(userID int64) (string, error)
	IssueRefreshToken(userID int64) (string, error)
}

// Service is the IAM application service.
type Service struct {
	repo   Repository
	hasher PasswordHasher
	tokens TokenIssuer
}

func NewService(repo Repository, hasher PasswordHasher, tokens TokenIssuer) *Service {
	return &Service{repo: repo, hasher: hasher, tokens: tokens}
}

// RegisterCommand creates an account.
type RegisterCommand struct {
	Email       string
	Password    string
	DisplayName string
}

// Register creates an account and signs the new user straight in.
func (s *Service) Register(ctx context.Context, cmd RegisterCommand) (Session, error) {
	var errs []domain.FieldError
	if cmd.Email == "" {
		errs = append(errs, domain.FieldError{Field: "email", Reason: "required"})
	}
	if len(cmd.Password) < MinPasswordLength {
		errs = append(errs, domain.FieldError{Field: "password", Reason: "must be at least 8 characters"})
	}
	if cmd.DisplayName == "" {
		errs = append(errs, domain.FieldError{Field: "display_name", Reason: "required"})
	}
	if len(errs) > 0 {
		return Session{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(errs...)
	}

	hash, err := s.hasher.Hash(cmd.Password)
	if err != nil {
		return Session{}, domain.Internal(err)
	}

	user, err := s.repo.Create(ctx, cmd.Email, hash, cmd.DisplayName)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return Session{}, domain.Conflict(domain.CodeEmailAlreadyRegistered, "email already registered")
		}
		return Session{}, domain.Internal(err)
	}
	return s.session(user)
}

// Login authenticates a user.
//
// The work is deliberately the same whether or not the account exists: a
// missing user is verified against a dummy hash, and both failures return
// one indistinguishable response. An early return for an unknown email
// would be an enumeration oracle — measurably faster, and enough to
// confirm who has an account here.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	user, err := s.repo.ByEmail(ctx, email)
	found := true
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return Session{}, domain.Internal(err)
		}
		found = false
	}

	hash := user.PasswordHash
	if !found {
		hash = s.hasher.DummyHash()
	}
	ok, err := s.hasher.Verify(password, hash)
	if err != nil {
		return Session{}, domain.Internal(err)
	}
	if !found || !ok {
		return Session{}, domain.Unauthorized(domain.CodeInvalidCredentials, "invalid email or password")
	}
	return s.session(user)
}

func (s *Service) session(user User) (Session, error) {
	access, err := s.tokens.IssueAccessToken(user.ID)
	if err != nil {
		return Session{}, domain.Internal(err)
	}
	refresh, err := s.tokens.IssueRefreshToken(user.ID)
	if err != nil {
		return Session{}, domain.Internal(err)
	}
	return Session{AccessToken: access, RefreshToken: refresh, User: user.Profile()}, nil
}
