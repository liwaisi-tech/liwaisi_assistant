//go:build ignore

// migrate.go is a standalone DB migration runner for the go-assistant service.
// Run with: go run scripts/migrate.go
package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbPath := os.Getenv("GO_ASSISTANT_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/go-assistant.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Error("failed to enable WAL mode", "error", err)
		os.Exit(1)
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS health_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status TEXT NOT NULL,
			checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			details TEXT
		)`,
	}

	for i, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			slog.Error("migration failed", "migration", i+1, "error", err)
			os.Exit(1)
		}
		fmt.Printf("Migration %d applied successfully.\n", i+1)
	}

	fmt.Println("All migrations completed.")
}
