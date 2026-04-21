//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func setupAuthoredArtefactStore(t *testing.T) (*AuthoredArtefactStore, context.Context) {
	t.Helper()
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return NewAuthoredArtefactStore(pool), context.Background()
}

func TestAuthoredArtefactStore_PreWritePostWrite(t *testing.T) {
	s, ctx := setupAuthoredArtefactStore(t)

	id, err := s.PreWrite(ctx, persist.WriteIntent{
		SetID:          "set-1",
		ForgeRunID:     "run-1",
		HostID:         "host-a",
		Classification: persist.ClassSource,
	}, "/home/u/.local/brae/src/main.go", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	if err := s.PostWrite(ctx, id, "deadbeef", 42, "text/plain"); err != nil {
		t.Fatalf("PostWrite: %v", err)
	}
	got, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != persist.ArtefactStateActive {
		t.Errorf("state = %q; want active", got.State)
	}
	if got.SHA256 != "deadbeef" {
		t.Errorf("sha256 = %q; want deadbeef", got.SHA256)
	}
}

func TestAuthoredArtefactStore_RollbackRestore(t *testing.T) {
	s, ctx := setupAuthoredArtefactStore(t)

	id, err := s.PreWrite(ctx, persist.WriteIntent{
		SetID:          "set-roll",
		Classification: persist.ClassSource,
	}, "/x/a.go", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	_ = s.PostWrite(ctx, id, "h", 1, "t")

	if err := s.Rollback(ctx, "set-roll", "/q/set-roll", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := s.Rollback(ctx, "set-roll", "/q/set-roll", "alice"); !errors.Is(err, persist.ErrArtefactAlreadyRolledBack) {
		t.Errorf("double rollback err = %v; want ErrArtefactAlreadyRolledBack", err)
	}
	if err := s.Restore(ctx, "set-roll", "alice"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != persist.ArtefactStateRestored {
		t.Errorf("state = %q; want restored", got.State)
	}

	evs, err := s.ListEvents(ctx, "set-roll")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 2 {
		t.Errorf("events = %d; want 2", len(evs))
	}
}

func TestAuthoredArtefactStore_PurgeExpired(t *testing.T) {
	s, ctx := setupAuthoredArtefactStore(t)

	id, err := s.PreWrite(ctx, persist.WriteIntent{
		SetID:          "set-purge",
		Classification: persist.ClassSource,
	}, "/x/p.go", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	_ = s.PostWrite(ctx, id, "h", 1, "t")
	_ = s.Rollback(ctx, "set-purge", "/q/set-purge", "alice")

	// cutoff in the future guarantees the row is past the grace window.
	cutoff := time.Now().UTC().Add(1 * time.Hour)
	rows, err := s.PurgeExpired(ctx, cutoff, "purge-svc")
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("purged = %d; want 1", len(rows))
	}
	if rows[0].State != persist.ArtefactStatePurged {
		t.Errorf("state = %q; want purged", rows[0].State)
	}
}

func TestAuthoredArtefactStore_ListByHost(t *testing.T) {
	s, ctx := setupAuthoredArtefactStore(t)

	for _, p := range []string{"/a", "/b", "/c"} {
		id, err := s.PreWrite(ctx, persist.WriteIntent{
			SetID:          "list-set",
			HostID:         "host-a",
			Classification: persist.ClassSource,
		}, p, 0o644)
		if err != nil {
			t.Fatalf("PreWrite: %v", err)
		}
		_ = s.PostWrite(ctx, id, "h", 1, "t")
	}
	arts, err := s.ListByHost(ctx, "host-a", persist.ArtefactFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if len(arts) != 3 {
		t.Errorf("count = %d; want 3", len(arts))
	}
}
