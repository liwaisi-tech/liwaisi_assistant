// Package sqlite provides SQLite persistence infrastructure for the go-assistant service.
package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/XSAM/otelsql"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

// NewConnection opens an instrumented SQLite database connection at the given path
// with WAL mode enabled and OTel query tracing + connection pool metrics.
func NewConnection(dbPath string) (*sql.DB, error) {
	db, err := otelsql.Open("sqlite", dbPath,
		otelsql.WithAttributes(semconv.DBSystemNameSQLite),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			Ping:           true,
			RowsNext:       false,
			DisableErrSkip: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("opening instrumented sqlite database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if _, err := otelsql.RegisterDBStatsMetrics(db,
		otelsql.WithAttributes(semconv.DBSystemNameSQLite),
	); err != nil {
		db.Close()
		return nil, fmt.Errorf("registering db stats metrics: %w", err)
	}

	return db, nil
}
