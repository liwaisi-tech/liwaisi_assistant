//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("liwaisi_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, dsn
}

func TestPoolConfig_String(t *testing.T) {
	t.Run("Redacts_credentials", func(t *testing.T) {
		cfg := PoolConfig{DSN: "postgres://myuser:mypassword@localhost:5432/mydb"}
		s := cfg.String()
		if containsStr(s, "myuser") || containsStr(s, "mypassword") {
			t.Fatalf("credentials not redacted: %s", s)
		}
	})

	t.Run("Preserves_host_port_db", func(t *testing.T) {
		cfg := PoolConfig{DSN: "postgres://user:pass@localhost:5432/mydb"}
		s := cfg.String()
		if !containsStr(s, "localhost") || !containsStr(s, "5432") {
			t.Fatalf("host/port not preserved: %s", s)
		}
	})

	t.Run("Invalid_DSN", func(t *testing.T) {
		cfg := PoolConfig{DSN: "://invalid"}
		s := cfg.String()
		if !containsStr(s, "invalid") {
			t.Fatalf("should handle invalid DSN: %s", s)
		}
	})
}

func TestNewPool(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		_, dsn := setupPostgres(t)
		ctx := context.Background()

		pool, err := NewPool(ctx, PoolConfig{DSN: dsn})
		if err != nil {
			t.Fatalf("NewPool: %v", err)
		}
		defer Close(pool)
	})

	t.Run("Empty_DSN", func(t *testing.T) {
		ctx := context.Background()
		_, err := NewPool(ctx, PoolConfig{DSN: ""})
		if err == nil {
			t.Fatal("should error on empty DSN")
		}
	})

	t.Run("Unreachable_host", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := NewPool(ctx, PoolConfig{DSN: "postgres://user:pass@192.0.2.1:5432/db"})
		if err == nil {
			t.Fatal("should error on unreachable host")
		}
	})

	t.Run("Cancelled_context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewPool(ctx, PoolConfig{DSN: "postgres://user:pass@localhost:5432/db"})
		if err == nil {
			t.Fatal("should error on cancelled context")
		}
	})
}

func TestHealth(t *testing.T) {
	t.Run("Healthy", func(t *testing.T) {
		pool, _ := setupPostgres(t)
		ctx := context.Background()
		if err := Health(ctx, pool); err != nil {
			t.Fatalf("Health: %v", err)
		}
	})
}

func TestClose(t *testing.T) {
	t.Run("Drains_gracefully", func(t *testing.T) {
		pool, _ := setupPostgres(t)
		Close(pool)
		// Pool is closed — further queries should fail
	})
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
