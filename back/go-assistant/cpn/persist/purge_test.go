package persist

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPurgeService_RespectsGrace(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	// Pin the clock.
	fakeNow := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	ledger.SetClock(func() time.Time { return fakeNow })

	path := filepath.Join(root, "src", "a.go")
	fs.addFile(path)
	id, _ := ledger.PreWrite(ctx, WriteIntent{SetID: "young", Classification: ClassSource}, path, 0o644)
	_ = ledger.PostWrite(ctx, id, "h", 1, "t")
	_ = ledger.Rollback(ctx, "young", filepath.Join(root, "quarantine", "young"), "alice")

	ledger.SetClock(func() time.Time { return fakeNow.Add(1 * 24 * time.Hour) })

	svc := &PurgeService{
		Ledger:    ledger,
		FS:        fs,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		GraceDays: 7,
	}

	// Explicit cutoff relative to the pinned "now": 1 day advance − 7 day
	// grace = 6 days in the past, which is before the row's quarantined_at
	// (which is the pinned "now"). The row is therefore WITHIN the grace.
	cutoff := fakeNow.Add(1 * 24 * time.Hour).Add(-7 * 24 * time.Hour)
	n, err := svc.Run(ctx, cutoff)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 0 {
		t.Errorf("purged = %d; want 0 (within grace)", n)
	}
}

func TestPurgeService_PurgesBeyondGrace(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	fakeNow := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	ledger.SetClock(func() time.Time { return fakeNow })

	path := filepath.Join(root, "src", "a.go")
	fs.addFile(path)
	id, _ := ledger.PreWrite(ctx, WriteIntent{SetID: "old", Classification: ClassSource}, path, 0o644)
	_ = ledger.PostWrite(ctx, id, "h", 1, "t")
	quarantineDir := filepath.Join(root, "quarantine", "old")
	_ = ledger.Rollback(ctx, "old", quarantineDir, "alice")
	// Simulate the file sitting in the quarantine dir.
	fs.addFile(filepath.Join(quarantineDir, "src", "a.go"))

	// Advance time by 10 days so the cutoff (now - 7d) > quarantinedAt.
	ledger.SetClock(func() time.Time { return fakeNow.Add(10 * 24 * time.Hour) })

	svc := &PurgeService{
		Ledger:    ledger,
		FS:        fs,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		GraceDays: 7,
	}

	// Compute an explicit cutoff relative to the pinned "now" so the test
	// is deterministic regardless of the wall clock.
	cutoff := fakeNow.Add(10 * 24 * time.Hour).Add(-7 * 24 * time.Hour)
	n, err := svc.Run(ctx, cutoff)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Errorf("purged = %d; want 1", n)
	}
	// Quarantine dir should be gone.
	if fs.Exists(quarantineDir) {
		t.Errorf("quarantine dir still exists")
	}
	// Ledger state reflects purge.
	arts, _ := ledger.SetForSet(ctx, "old")
	if arts[0].State != ArtefactStatePurged {
		t.Errorf("state = %q; want purged", arts[0].State)
	}
}
