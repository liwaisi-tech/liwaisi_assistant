package sqlite

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestHealthRepository_CheckHealth(t *testing.T) {
	tests := []struct {
		name      string
		setupDB   func(t *testing.T) *sql.DB
		expectErr bool
	}{
		{
			name: "successful ping on open database",
			setupDB: func(t *testing.T) *sql.DB {
				return newTestDB(t)
			},
			expectErr: false,
		},
		{
			name: "ping fails on closed database",
			setupDB: func(t *testing.T) *sql.DB {
				db := newTestDB(t)
				db.Close()
				return db
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.setupDB(t)
			repo := NewHealthRepository(db)

			err := repo.CheckHealth(context.Background())
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewConnection(t *testing.T) {
	tests := []struct {
		name      string
		dbPath    string
		expectErr bool
	}{
		{
			name:      "in-memory connection succeeds",
			dbPath:    ":memory:",
			expectErr: false,
		},
		{
			name:      "invalid path fails",
			dbPath:    "/nonexistent/path/to/db.sqlite",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewConnection(tt.dbPath)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
					if db != nil {
						db.Close()
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			t.Cleanup(func() { db.Close() })

			if pingErr := db.Ping(); pingErr != nil {
				t.Errorf("ping after NewConnection failed: %v", pingErr)
			}
		})
	}
}
