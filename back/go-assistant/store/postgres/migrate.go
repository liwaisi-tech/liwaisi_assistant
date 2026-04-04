package postgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5 driver for golang-migrate
	_ "github.com/golang-migrate/migrate/v4/source/file"     // file source for migrations
)

// RunMigrations applies all pending SQL migrations from the given path.
// Uses golang-migrate/v4 with the pgx5 driver. Idempotent — safe to call multiple times.
func RunMigrations(dsn, migrationsPath string) error {
	// The pgx/v5 driver registers as "pgx5". Convert standard postgres:// DSN.
	dbURL := dsn
	if after, ok := strings.CutPrefix(dbURL, "postgres://"); ok {
		dbURL = "pgx5://" + after
	} else if after, ok := strings.CutPrefix(dbURL, "postgresql://"); ok {
		dbURL = "pgx5://" + after
	}

	m, err := migrate.New("file://"+migrationsPath, dbURL)
	if err != nil {
		return fmt.Errorf("postgres: create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: run migrations: %w", err)
	}

	return nil
}
