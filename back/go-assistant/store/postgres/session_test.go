//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func makeTestSession(id, userID string) *persist.SessionRecord {
	now := time.Now()
	return &persist.SessionRecord{
		ID:             id,
		UserID:         userID,
		Channel:        "web",
		State:          persist.SessionActive,
		CreatedAt:      now,
		LastActivityAt: now,
	}
}

func TestSessionRepository_Create(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		err := repo.Create(ctx, makeTestSession("s1", "u1"))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("DuplicateID", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("s-dup", "u1"))
		err := repo.Create(ctx, makeTestSession("s-dup", "u1"))
		if err != persist.ErrSessionExists {
			t.Fatalf("want ErrSessionExists, got %v", err)
		}
	})

	t.Run("EmptyID", func(t *testing.T) {
		err := repo.Create(ctx, makeTestSession("", "u1"))
		if err != persist.ErrInvalidInput {
			t.Fatalf("want ErrInvalidInput, got %v", err)
		}
	})
}

func TestSessionRepository_Get(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sg1", "u1"))
		s, err := repo.Get(ctx, "sg1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if s.ID != "sg1" || s.UserID != "u1" {
			t.Fatalf("unexpected: %+v", s)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := repo.Get(ctx, "missing")
		if err != persist.ErrSessionNotFound {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSessionRepository_AppendMessage(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sam1", "u1"))
		msg := &persist.MessageRecord{ID: "m1", Role: "user", Content: "hello", Timestamp: time.Now()}
		err := repo.AppendMessage(ctx, "sam1", msg)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		s, _ := repo.Get(ctx, "sam1")
		if len(s.Messages) != 1 {
			t.Fatalf("want 1 message, got %d", len(s.Messages))
		}
	})

	t.Run("SessionNotFound", func(t *testing.T) {
		msg := &persist.MessageRecord{ID: "m2", Role: "user", Content: "hello", Timestamp: time.Now()}
		err := repo.AppendMessage(ctx, "missing", msg)
		if err != persist.ErrSessionNotFound {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("SessionClosed", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sam-closed", "u1"))
		_ = repo.Close(ctx, "sam-closed")
		msg := &persist.MessageRecord{ID: "m3", Role: "user", Content: "hello", Timestamp: time.Now()}
		err := repo.AppendMessage(ctx, "sam-closed", msg)
		if err != persist.ErrSessionClosed {
			t.Fatalf("want ErrSessionClosed, got %v", err)
		}
	})
}

func TestSessionRepository_Touch(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	_ = repo.Create(ctx, makeTestSession("st1", "u1"))
	time.Sleep(10 * time.Millisecond)
	err := repo.Touch(ctx, "st1")
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}

	err = repo.Touch(ctx, "missing")
	if err != persist.ErrSessionNotFound {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestSessionRepository_Close(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sc1", "u1"))
		err := repo.Close(ctx, "sc1")
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		s, _ := repo.Get(ctx, "sc1")
		if s.State != persist.SessionClosed {
			t.Fatalf("want closed, got %s", s.State)
		}
	})

	t.Run("AlreadyClosed", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sc2", "u1"))
		_ = repo.Close(ctx, "sc2")
		err := repo.Close(ctx, "sc2")
		if err != persist.ErrSessionClosed {
			t.Fatalf("want ErrSessionClosed, got %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		err := repo.Close(ctx, "missing")
		if err != persist.ErrSessionNotFound {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSessionRepository_ListExpired(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	old := makeTestSession("sle-old", "u1")
	old.LastActivityAt = time.Now().Add(-2 * time.Hour)
	_ = repo.Create(ctx, old)

	fresh := makeTestSession("sle-fresh", "u1")
	_ = repo.Create(ctx, fresh)

	ids, err := repo.ListExpired(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	if len(ids) != 1 || ids[0] != "sle-old" {
		t.Fatalf("want [sle-old], got %v", ids)
	}
}

func TestSessionRepository_Delete(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewSessionRepository(pool)
	ctx := context.Background()

	t.Run("Success_CascadesMessages", func(t *testing.T) {
		_ = repo.Create(ctx, makeTestSession("sd1", "u1"))
		_ = repo.AppendMessage(ctx, "sd1", &persist.MessageRecord{ID: "dm1", Role: "user", Content: "hi", Timestamp: time.Now()})
		err := repo.Delete(ctx, "sd1")
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err = repo.Get(ctx, "sd1")
		if err != persist.ErrSessionNotFound {
			t.Fatalf("want ErrSessionNotFound after delete, got %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		err := repo.Delete(ctx, "missing")
		if err != persist.ErrSessionNotFound {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})
}
