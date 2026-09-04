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
	// CountAdmins and CreateAdmin back BootstrapSuperAdmin only — every
	// other write in this package creates an ordinary (is_admin=false)
	// account through Create.
	CountAdmins(ctx context.Context) (int64, error)
	CreateAdmin(ctx context.Context, email, passwordHash, displayName string) (User, error)
	// CreateGuest backs CreateGuest only — an is_guest=true account nobody
	// ever signs into by hand (generated email + generated, never-handed-out
	// password).
	CreateGuest(ctx context.Context, email, passwordHash, displayName string) (User, error)
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

// CreateGuest provisions a throwaway account for an anonymous "立即体验"
// visitor and signs it straight in. It exists so the rest of the platform
// — POST /runs, GET /runs/{id}, the event stream, black-box filtering —
// can stay exactly one code path: a guest is a real (if synthetic) userID
// the moment this returns, not a special case those layers need to know
// about. The email/password are generated and never surfaced to anyone;
// nobody is meant to ever log into a guest account by hand.
func (s *Service) CreateGuest(ctx context.Context) (Session, error) {
	suffix, err := randomHex(16)
	if err != nil {
		return Session{}, domain.Internal(err)
	}
	email := "guest-" + suffix + "@guest.invalid"
	password, err := randomPassword(24)
	if err != nil {
		return Session{}, domain.Internal(err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return Session{}, domain.Internal(err)
	}

	user, err := s.repo.CreateGuest(ctx, email, hash, "访客")
	if err != nil {
		return Session{}, domain.Internal(err)
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

// BootstrapSuperAdmin ensures at least one is_admin account exists — every
// admin-only surface (系统配置's 用户管理/角色权限/模型提供商, 运营中心)
// is otherwise unreachable on a fresh install, since no API creates the
// first admin. A no-op once any admin exists (created=false). password is
// the plaintext to hand the operator exactly once — the caller (main.go)
// is responsible for surfacing it (a log line) and never storing it; if
// presetPassword is non-empty it's used as-is (a deploy pinning a known
// bootstrap password via env) instead of generating a random one.
func (s *Service) BootstrapSuperAdmin(ctx context.Context, email, displayName, presetPassword string) (created bool, password string, err error) {
	count, err := s.repo.CountAdmins(ctx)
	if err != nil {
		return false, "", domain.Internal(err)
	}
	if count > 0 {
		return false, "", nil
	}

	password = presetPassword
	if password == "" {
		password, err = randomPassword(20)
		if err != nil {
			return false, "", domain.Internal(err)
		}
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return false, "", domain.Internal(err)
	}
	if _, err := s.repo.CreateAdmin(ctx, email, hash, displayName); err != nil {
		return false, "", domain.Internal(err)
	}
	return true, password, nil
}
