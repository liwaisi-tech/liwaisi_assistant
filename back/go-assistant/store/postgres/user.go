package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// UserRepository implements persist.UserRepository using PostgreSQL.
type UserRepository struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.UserRepository = (*UserRepository)(nil)

// NewUserRepository creates a new Postgres-backed user repository.
func NewUserRepository(pool pgxPool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Upsert creates a new user or updates an existing one by ID (Google sub).
// On conflict (same ID), email, name, picture, and updated_at are refreshed.
func (r *UserRepository) Upsert(ctx context.Context, user *persist.UserRecord) error {
	if user.ID == "" {
		return persist.ErrInvalidInput
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, name, picture, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		   email = EXCLUDED.email,
		   name = EXCLUDED.name,
		   picture = EXCLUDED.picture,
		   updated_at = EXCLUDED.updated_at`,
		user.ID, user.Email, user.Name, user.Picture, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres user upsert: %w", err)
	}
	return nil
}

// GetByID retrieves a user by ID (Google sub).
func (r *UserRepository) GetByID(ctx context.Context, id string) (*persist.UserRecord, error) {
	u := &persist.UserRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, name, picture, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrUserNotFound
		}
		return nil, fmt.Errorf("postgres user get by id %s: %w", id, err)
	}
	return u, nil
}

// GetByEmail retrieves a user by email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*persist.UserRecord, error) {
	u := &persist.UserRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, name, picture, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrUserNotFound
		}
		return nil, fmt.Errorf("postgres user get by email %s: %w", email, err)
	}
	return u, nil
}
