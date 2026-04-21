package persist

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryArtefactLedger_PreWrite_PostWrite(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	intent := WriteIntent{
		ForgeRunID:     "run-1",
		SetID:          "set-1",
		HostID:         "host-1",
		Classification: ClassSource,
		Actor:          "alice@example.com",
	}

	id, err := l.PreWrite(ctx, intent, "/home/u/.local/brae/src/main.go", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	if id == "" {
		t.Fatal("PreWrite returned empty id")
	}

	art, err := l.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if art.State != ArtefactStatePending {
		t.Errorf("state = %q; want pending", art.State)
	}
	if art.Classification != ClassSource {
		t.Errorf("classification = %q; want source", art.Classification)
	}

	if err := l.PostWrite(ctx, id, "deadbeef", 42, "text/plain"); err != nil {
		t.Fatalf("PostWrite: %v", err)
	}

	art2, err := l.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after PostWrite: %v", err)
	}
	if art2.State != ArtefactStateActive {
		t.Errorf("state = %q; want active", art2.State)
	}
	if art2.SHA256 != "deadbeef" || art2.Size != 42 || art2.MIME != "text/plain" {
		t.Errorf("post-write metadata mismatch: %+v", art2)
	}
}

func TestMemoryArtefactLedger_PreWrite_AutoClassifies(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	id, err := l.PreWrite(ctx, WriteIntent{SetID: "s"}, "/x/file.go", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	art, _ := l.GetByID(ctx, id)
	// PreWrite itself does not auto-classify — that's the host adapter's
	// job. The ledger falls back to "other" when intent.Classification is
	// empty. See os_adapter.go for the Classify() call.
	if art.Classification != ClassOther {
		t.Errorf("classification = %q; want other", art.Classification)
	}
}

func TestMemoryArtefactLedger_RollbackRestore(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	for _, p := range []string{"/x/a", "/x/b", "/x/c"} {
		id, err := l.PreWrite(ctx, WriteIntent{SetID: "set-2", Classification: ClassSource}, p, 0o644)
		if err != nil {
			t.Fatalf("PreWrite %q: %v", p, err)
		}
		if err := l.PostWrite(ctx, id, "h", 1, "t"); err != nil {
			t.Fatalf("PostWrite: %v", err)
		}
	}

	if err := l.Rollback(ctx, "set-2", "/q/set-2", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	arts, _ := l.SetForSet(ctx, "set-2")
	for _, a := range arts {
		if a.State != ArtefactStateQuarantined {
			t.Errorf("after rollback state = %q; want quarantined", a.State)
		}
	}

	// Re-rollback should fail.
	if err := l.Rollback(ctx, "set-2", "/q/set-2", "alice"); !errors.Is(err, ErrArtefactAlreadyRolledBack) {
		t.Errorf("double rollback err = %v; want ErrArtefactAlreadyRolledBack", err)
	}

	if err := l.Restore(ctx, "set-2", "alice"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	arts2, _ := l.SetForSet(ctx, "set-2")
	for _, a := range arts2 {
		if a.State != ArtefactStateRestored {
			t.Errorf("after restore state = %q; want restored", a.State)
		}
	}

	evs, _ := l.ListEvents(ctx, "set-2")
	if len(evs) != 2 {
		t.Errorf("events = %d; want 2 (rollback+restore)", len(evs))
	}
}

func TestMemoryArtefactLedger_PurgeExpired(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	// Pin the clock.
	fakeNow := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return fakeNow })

	id, err := l.PreWrite(ctx, WriteIntent{SetID: "old-set", Classification: ClassSource}, "/x/old", 0o644)
	if err != nil {
		t.Fatalf("PreWrite: %v", err)
	}
	_ = l.PostWrite(ctx, id, "h", 1, "t")

	if err := l.Rollback(ctx, "old-set", "/q/old-set", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Advance clock 8 days.
	l.SetClock(func() time.Time { return fakeNow.Add(8 * 24 * time.Hour) })
	cutoff := fakeNow.Add(7 * 24 * time.Hour)

	purged, err := l.PurgeExpired(ctx, cutoff, "purge-svc")
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(purged) != 1 {
		t.Fatalf("purged = %d; want 1", len(purged))
	}
	if purged[0].State != ArtefactStatePurged {
		t.Errorf("state = %q; want purged", purged[0].State)
	}
}

func TestMemoryArtefactLedger_PurgeRespectsGrace(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	fakeNow := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return fakeNow })

	id, _ := l.PreWrite(ctx, WriteIntent{SetID: "young", Classification: ClassSource}, "/x/y", 0o644)
	_ = l.PostWrite(ctx, id, "h", 1, "t")
	_ = l.Rollback(ctx, "young", "/q/young", "alice")

	// Only 1 day passed — cutoff is now-7d, so the row must NOT be purged.
	l.SetClock(func() time.Time { return fakeNow.Add(1 * 24 * time.Hour) })
	cutoff := fakeNow.Add(1 * 24 * time.Hour).Add(-7 * 24 * time.Hour)

	purged, err := l.PurgeExpired(ctx, cutoff, "purge-svc")
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(purged) != 0 {
		t.Errorf("purged = %d; want 0 (within grace)", len(purged))
	}
}

func TestMemoryArtefactLedger_ListByHost(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	// host-a: two artefacts.
	id1, _ := l.PreWrite(ctx, WriteIntent{SetID: "s1", HostID: "host-a", Classification: ClassSource}, "/a/one", 0o644)
	_ = l.PostWrite(ctx, id1, "h", 1, "t")
	id2, _ := l.PreWrite(ctx, WriteIntent{SetID: "s1", HostID: "host-a", Classification: ClassBinary}, "/a/two", 0o755)
	_ = l.PostWrite(ctx, id2, "h", 1, "t")

	// host-b: one artefact.
	id3, _ := l.PreWrite(ctx, WriteIntent{SetID: "s2", HostID: "host-b", Classification: ClassConfig}, "/b/one", 0o644)
	_ = l.PostWrite(ctx, id3, "h", 1, "t")

	arts, err := l.ListByHost(ctx, "host-a", ArtefactFilter{})
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if len(arts) != 2 {
		t.Errorf("host-a count = %d; want 2", len(arts))
	}

	binArts, err := l.ListByHost(ctx, "host-a", ArtefactFilter{
		ClassificationIn: []ArtefactClassification{ClassBinary},
	})
	if err != nil {
		t.Fatalf("ListByHost filter: %v", err)
	}
	if len(binArts) != 1 {
		t.Errorf("binary filter count = %d; want 1", len(binArts))
	}
}

func TestMemoryArtefactLedger_SetForForgeRun(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	id, _ := l.PreWrite(ctx, WriteIntent{ForgeRunID: "r1", SetID: "s1", Classification: ClassSource}, "/p/a", 0o644)
	_ = l.PostWrite(ctx, id, "h", 1, "t")

	arts, err := l.SetForForgeRun(ctx, "r1")
	if err != nil {
		t.Fatalf("SetForForgeRun: %v", err)
	}
	if len(arts) != 1 {
		t.Errorf("count = %d; want 1", len(arts))
	}
}

func TestMemoryArtefactLedger_SetForSet_Empty(t *testing.T) {
	ctx := context.Background()
	l := NewMemoryArtefactLedger()

	_, err := l.SetForSet(ctx, "missing")
	if !errors.Is(err, ErrArtefactSetEmpty) {
		t.Errorf("SetForSet missing err = %v; want ErrArtefactSetEmpty", err)
	}
}
