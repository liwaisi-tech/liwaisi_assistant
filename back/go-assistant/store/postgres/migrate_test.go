//go:build integration

package postgres

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func migrationsPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "migrations")
}

func TestRunMigrations(t *testing.T) {
	t.Run("All_up", func(t *testing.T) {
		_, dsn := setupPostgres(t)
		if err := RunMigrations(dsn, migrationsPath()); err != nil {
			t.Fatalf("RunMigrations: %v", err)
		}
	})

	t.Run("Idempotent", func(t *testing.T) {
		_, dsn := setupPostgres(t)
		if err := RunMigrations(dsn, migrationsPath()); err != nil {
			t.Fatalf("first run: %v", err)
		}
		if err := RunMigrations(dsn, migrationsPath()); err != nil {
			t.Fatalf("second run should be idempotent: %v", err)
		}
	})
}

func TestMigration_001_Sessions(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()

	t.Run("Tables_exist", func(t *testing.T) {
		assertTableExists(t, pool, "sessions")
		assertTableExists(t, pool, "messages")
	})

	t.Run("Columns_correct", func(t *testing.T) {
		assertColumnExists(t, pool, "sessions", "user_id")
		assertColumnExists(t, pool, "sessions", "channel")
		assertColumnExists(t, pool, "sessions", "state")
		assertColumnExists(t, pool, "sessions", "last_activity_at")
		assertColumnExists(t, pool, "messages", "session_id")
		assertColumnExists(t, pool, "messages", "cpn_depth")
	})

	t.Run("FK_constraint", func(t *testing.T) {
		// Insert a session first
		_, err := pool.Exec(ctx, "INSERT INTO sessions (id, user_id, channel) VALUES ('s1', 'u1', 'web')")
		if err != nil {
			t.Fatalf("insert session: %v", err)
		}
		// Insert message with valid FK
		_, err = pool.Exec(ctx, "INSERT INTO messages (id, session_id, role, content) VALUES ('m1', 's1', 'user', 'hi')")
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}
		// Insert message with invalid FK should fail
		_, err = pool.Exec(ctx, "INSERT INTO messages (id, session_id, role, content) VALUES ('m2', 'nonexistent', 'user', 'hi')")
		if err == nil {
			t.Fatal("FK constraint should reject invalid session_id")
		}
	})
}

func TestMigration_002_Events(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	t.Run("Table_partitioned", func(t *testing.T) {
		var partKey string
		err := pool.QueryRow(context.Background(),
			"SELECT pg_get_partkeydef(c.oid) FROM pg_class c WHERE c.relname = 'events'").Scan(&partKey)
		if err != nil {
			t.Fatalf("query partition key: %v", err)
		}
		if partKey == "" {
			t.Fatal("events table should be partitioned")
		}
	})

	t.Run("Default_partition_exists", func(t *testing.T) {
		assertTableExists(t, pool, "events_default")
	})
}

func TestMigration_003_TokenLedger(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	t.Run("Table_exists", func(t *testing.T) {
		assertTableExists(t, pool, "token_ledger")
	})
}

func TestMigration_004_Flows(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	t.Run("Table_exists", func(t *testing.T) {
		assertTableExists(t, pool, "flows")
	})

	t.Run("Soft_delete_column", func(t *testing.T) {
		assertColumnExists(t, pool, "flows", "deleted_at")
	})
}

func TestMigration_005_ExecutionRecords(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	t.Run("Table_exists", func(t *testing.T) {
		assertTableExists(t, pool, "execution_records")
	})

	t.Run("Columns_correct", func(t *testing.T) {
		assertColumnExists(t, pool, "execution_records", "cpn_depth")
		assertColumnExists(t, pool, "execution_records", "tool_calls")
		assertColumnExists(t, pool, "execution_records", "tokens_produced")
	})
}

// --- Test Helpers ---

func assertTableExists(t *testing.T, pool interface{ QueryRow(ctx context.Context, sql string, args ...any) interface{ Scan(dest ...any) error } }, table string) {
	t.Helper()
	var exists bool
	row := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)", table)
	if err := row.Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if !exists {
		t.Fatalf("table %s should exist", table)
	}
}

func assertColumnExists(t *testing.T, pool interface{ QueryRow(ctx context.Context, sql string, args ...any) interface{ Scan(dest ...any) error } }, table, column string) {
	t.Helper()
	var exists bool
	row := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)", table, column)
	if err := row.Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	if !exists {
		t.Fatalf("column %s.%s should exist", table, column)
	}
}
