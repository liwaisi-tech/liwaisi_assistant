package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// PersonalityStore implements persist.PersonalityRepository using PostgreSQL.
type PersonalityStore struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.PersonalityRepository = (*PersonalityStore)(nil)

// NewPersonalityStore creates a new PersonalityStore.
func NewPersonalityStore(pool pgxPool) *PersonalityStore {
	return &PersonalityStore{pool: pool}
}

// Get retrieves the personality for a user. Returns nil, nil if not found.
func (s *PersonalityStore) Get(ctx context.Context, userID string) (*persist.PersonalityRecord, error) {
	rec := &persist.PersonalityRecord{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, principles, hierarchy, tensions, version, created_at, updated_at
		 FROM personalities WHERE user_id = $1`, userID,
	).Scan(&rec.UserID, &rec.Principles, &rec.Hierarchy, &rec.Tensions, &rec.Version, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres personality get %s: %w", userID, err)
	}
	return rec, nil
}

// Save upserts a personality record (INSERT ON CONFLICT UPDATE).
func (s *PersonalityStore) Save(ctx context.Context, rec *persist.PersonalityRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO personalities (user_id, principles, hierarchy, tensions, version, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		   principles = $2, hierarchy = $3, tensions = $4, version = $5, updated_at = NOW()`,
		rec.UserID, rec.Principles, rec.Hierarchy, rec.Tensions, rec.Version,
	)
	if err != nil {
		return fmt.Errorf("postgres personality save %s: %w", rec.UserID, err)
	}
	return nil
}

// Delete removes the personality for a user. Returns nil if not found.
func (s *PersonalityStore) Delete(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM personalities WHERE user_id = $1", userID)
	if err != nil {
		return fmt.Errorf("postgres personality delete %s: %w", userID, err)
	}
	return nil
}
