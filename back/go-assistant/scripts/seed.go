//go:build ignore

// seed.go inserts sample data into the go-assistant database.
// Run with: go run scripts/seed.go
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

	seeds := []struct {
		status  string
		details string
	}{
		{"UP", "Initial health check - all systems operational"},
		{"UP", "Scheduled health check - database responsive"},
		{"DEGRADED", "Scheduled health check - high latency detected"},
		{"UP", "Scheduled health check - recovered from degraded state"},
	}

	stmt, err := db.Prepare("INSERT INTO health_checks (status, details) VALUES (?, ?)")
	if err != nil {
		slog.Error("failed to prepare statement", "error", err)
		os.Exit(1)
	}
	defer stmt.Close()

	for i, s := range seeds {
		if _, err := stmt.Exec(s.status, s.details); err != nil {
			slog.Error("seed failed", "seed", i+1, "error", err)
			os.Exit(1)
		}
		fmt.Printf("Seed %d inserted: status=%s\n", i+1, s.status)
	}

	fmt.Println("All seed data inserted.")
}
