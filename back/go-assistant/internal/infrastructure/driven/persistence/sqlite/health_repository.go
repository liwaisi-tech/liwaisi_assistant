package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
)

// healthRepository implements the output.HealthRepository port using SQLite.
type healthRepository struct {
	db *sql.DB
}

// NewHealthRepository creates a new HealthRepository backed by the given SQLite connection.
func NewHealthRepository(db *sql.DB) output.HealthRepository {
	return &healthRepository{db: db}
}

// CheckHealth verifies connectivity to the SQLite database by pinging it.
func (r *healthRepository) CheckHealth(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite health check failed: %w", err)
	}
	return nil
}
