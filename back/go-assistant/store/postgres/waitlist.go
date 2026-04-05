package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// WaitlistRepository implements persist.WaitlistRepository using PostgreSQL.
type WaitlistRepository struct {
	pool pgxPool
}

var _ persist.WaitlistRepository = (*WaitlistRepository)(nil)

// NewWaitlistRepository creates a new Postgres-backed waitlist repository.
func NewWaitlistRepository(pool pgxPool) *WaitlistRepository {
	return &WaitlistRepository{pool: pool}
}

// Add inserts an email into the waitlist. On duplicate, does nothing (idempotent).
func (r *WaitlistRepository) Add(ctx context.Context, email, source string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return persist.ErrInvalidInput
	}
	if source == "" {
		source = "landing_page"
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO waitlist (email, source) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING`,
		email, source,
	)
	if err != nil {
		return fmt.Errorf("postgres waitlist add: %w", err)
	}
	return nil
}
