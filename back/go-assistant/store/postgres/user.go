package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	var overridesJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, name, picture, created_at, updated_at,
		        onboarding_completed_at, preferred_language, preferred_model, model_overrides,
		        COALESCE(regional_variant, '')
		 FROM users WHERE id = $1`, id,
	).Scan(
		&u.ID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt,
		&u.OnboardingCompletedAt, &u.PreferredLanguage, &u.PreferredModel, &overridesJSON,
		&u.RegionalVariant,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrUserNotFound
		}
		return nil, fmt.Errorf("postgres user get by id %s: %w", id, err)
	}
	if err := json.Unmarshal(overridesJSON, &u.ModelOverrides); err != nil {
		u.ModelOverrides = map[string]string{}
	}
	return u, nil
}

// GetByEmail retrieves a user by email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*persist.UserRecord, error) {
	u := &persist.UserRecord{}
	var overridesJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, name, picture, created_at, updated_at,
		        onboarding_completed_at, preferred_language, preferred_model, model_overrides,
		        COALESCE(regional_variant, '')
		 FROM users WHERE email = $1`, email,
	).Scan(
		&u.ID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt,
		&u.OnboardingCompletedAt, &u.PreferredLanguage, &u.PreferredModel, &overridesJSON,
		&u.RegionalVariant,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrUserNotFound
		}
		return nil, fmt.Errorf("postgres user get by email %s: %w", email, err)
	}
	if err := json.Unmarshal(overridesJSON, &u.ModelOverrides); err != nil {
		u.ModelOverrides = map[string]string{}
	}
	return u, nil
}

// UpdatePreferences persists user preference settings.
func (r *UserRepository) UpdatePreferences(ctx context.Context, userID string, prefs *persist.UserPreferences) error {
	if userID == "" {
		return persist.ErrInvalidInput
	}
	overrides := prefs.ModelOverrides
	if overrides == nil {
		overrides = map[string]string{}
	}
	overridesJSON, err := json.Marshal(overrides)
	if err != nil {
		return fmt.Errorf("postgres user marshal overrides: %w", err)
	}
	// regional_variant is updated only when non-empty so the preferences PUT
	// path does not clobber a value the user set during onboarding.
	var variantParam any
	if prefs.RegionalVariant != "" {
		variantParam = prefs.RegionalVariant
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE users
		 SET preferred_language = $2,
		     preferred_model    = $3,
		     model_overrides    = $4,
		     updated_at         = $5,
		     regional_variant   = COALESCE($6, regional_variant)
		 WHERE id = $1`,
		userID, prefs.PreferredLanguage, prefs.PreferredModel, overridesJSON, time.Now(), variantParam,
	)
	if err != nil {
		return fmt.Errorf("postgres user update preferences: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrUserNotFound
	}
	return nil
}

// CompleteOnboarding marks the user's onboarding as completed.
func (r *UserRepository) CompleteOnboarding(ctx context.Context, userID string) error {
	if userID == "" {
		return persist.ErrInvalidInput
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET onboarding_completed_at = $2, updated_at = $2 WHERE id = $1`,
		userID, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("postgres user complete onboarding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrUserNotFound
	}
	return nil
}
