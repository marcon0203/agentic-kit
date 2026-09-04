package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marcon0203/agentic-kit/internal/domain/iam"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// UserRepository implements iam.Repository.
type UserRepository struct{ q store.Querier }

func NewUserRepository(q store.Querier) *UserRepository { return &UserRepository{q: q} }

func (r *UserRepository) Create(ctx context.Context, email, passwordHash, displayName string) (iam.User, error) {
	row, err := r.q.CreateUser(ctx, store.CreateUserParams{
		Email: email, PasswordHash: passwordHash, DisplayName: displayName,
	})
	if err != nil {
		// The unique index on email is the authority on "taken", not a
		// prior existence check — that would race two concurrent
		// registrations of the same address.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return iam.User{}, iam.ErrEmailTaken
		}
		return iam.User{}, err
	}
	return toDomainUser(row), nil
}

func (r *UserRepository) CountAdmins(ctx context.Context) (int64, error) {
	return r.q.CountAdminUsers(ctx)
}

func (r *UserRepository) CreateAdmin(ctx context.Context, email, passwordHash, displayName string) (iam.User, error) {
	row, err := r.q.CreateAdminUser(ctx, store.CreateAdminUserParams{
		Email: email, PasswordHash: passwordHash, DisplayName: displayName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return iam.User{}, iam.ErrEmailTaken
		}
		return iam.User{}, err
	}
	return toDomainUser(row), nil
}

func (r *UserRepository) CreateGuest(ctx context.Context, email, passwordHash, displayName string) (iam.User, error) {
	row, err := r.q.CreateGuestUser(ctx, store.CreateGuestUserParams{
		Email: email, PasswordHash: passwordHash, DisplayName: displayName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return iam.User{}, iam.ErrEmailTaken
		}
		return iam.User{}, err
	}
	return toDomainUser(row), nil
}

func (r *UserRepository) ByEmail(ctx context.Context, email string) (iam.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return iam.User{}, iam.ErrNotFound
	}
	if err != nil {
		return iam.User{}, err
	}
	return toDomainUser(row), nil
}

func toDomainUser(row store.User) iam.User {
	return iam.User{
		ID: row.ID, Email: row.Email, PasswordHash: row.PasswordHash,
		DisplayName: row.DisplayName, IsAdmin: row.IsAdmin, IsGuest: row.IsGuest, CreatedAt: row.CreatedAt.Time,
	}
}
