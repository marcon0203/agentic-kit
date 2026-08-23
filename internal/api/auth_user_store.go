package api

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// AuthUser is the subset of a users row the auth handlers need, decoupled
// from store.User so this package doesn't leak pgtype across the boundary.
type AuthUser struct {
	ID           int64
	Email        string
	PasswordHash string
	DisplayName  string
	IsAdmin      bool
	CreatedAt    time.Time
}

// ErrEmailTaken is returned by AuthUserStore.CreateUser when the email is
// already registered (maps to 409, per spec-04's acceptance checklist).
var ErrEmailTaken = errors.New("auth: email already registered")

// AuthUserStore is the persistence dependency of the register/login
// handlers.
type AuthUserStore interface {
	CreateUser(ctx context.Context, email, passwordHash, displayName string) (AuthUser, error)
	GetUserByEmail(ctx context.Context, email string) (AuthUser, bool, error)
}

// PostgresAuthUserStore implements AuthUserStore against the users table.
type PostgresAuthUserStore struct {
	Queries *store.Queries
}

func NewPostgresAuthUserStore(q *store.Queries) *PostgresAuthUserStore {
	return &PostgresAuthUserStore{Queries: q}
}

func (s *PostgresAuthUserStore) CreateUser(ctx context.Context, email, passwordHash, displayName string) (AuthUser, error) {
	row, err := s.Queries.CreateUser(ctx, store.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return AuthUser{}, ErrEmailTaken
		}
		return AuthUser{}, err
	}
	return toAuthUser(row), nil
}

func (s *PostgresAuthUserStore) GetUserByEmail(ctx context.Context, email string) (AuthUser, bool, error) {
	row, err := s.Queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, false, nil
	}
	if err != nil {
		return AuthUser{}, false, err
	}
	return toAuthUser(row), true, nil
}

func toAuthUser(row store.User) AuthUser {
	return AuthUser{
		ID:           row.ID,
		Email:        row.Email,
		PasswordHash: row.PasswordHash,
		DisplayName:  row.DisplayName,
		IsAdmin:      row.IsAdmin,
		CreatedAt:    row.CreatedAt.Time,
	}
}
