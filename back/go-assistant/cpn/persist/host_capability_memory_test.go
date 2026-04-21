package persist

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryHostCapabilityRepository_SaveAndLatest(t *testing.T) {
	repo := NewMemoryHostCapabilityRepository()
	ctx := context.Background()

	// Empty repo → ErrHostSnapshotNotFound.
	if _, err := repo.LatestForHost(ctx, "host-a"); !errors.Is(err, ErrHostSnapshotNotFound) {
		t.Fatalf("want ErrHostSnapshotNotFound, got %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	if err := repo.Save(ctx, HostCapabilitySnapshot{
		HostID: "host-a", CapturedAt: base, Source: HostSnapshotSourceBootstrap,
	}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := repo.Save(ctx, HostCapabilitySnapshot{
		HostID: "host-a", CapturedAt: base.Add(30 * time.Minute), Source: HostSnapshotSourceSession,
	}); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	latest, err := repo.LatestForHost(ctx, "host-a")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Source != HostSnapshotSourceSession {
		t.Fatalf("want latest.Source=session, got %q", latest.Source)
	}
}

func TestMemoryHostCapabilityRepository_AppendProbeResult(t *testing.T) {
	repo := NewMemoryHostCapabilityRepository()
	ctx := context.Background()

	if err := repo.AppendProbeResult(ctx, "host-a", BinaryProbe{Name: "gcc"}); !errors.Is(err, ErrHostSnapshotNotFound) {
		t.Fatalf("want ErrHostSnapshotNotFound, got %v", err)
	}

	if err := repo.Save(ctx, HostCapabilitySnapshot{
		HostID: "host-a", CapturedAt: time.Now(),
		Binaries: []BinaryProbe{{Name: "gcc", Present: false}},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := repo.AppendProbeResult(ctx, "host-a", BinaryProbe{Name: "gcc", Present: true, Version: "13.2.0"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	latest, _ := repo.LatestForHost(ctx, "host-a")
	if len(latest.Binaries) != 1 || !latest.Binaries[0].Present || latest.Binaries[0].Version != "13.2.0" {
		t.Fatalf("gcc probe not replaced: %+v", latest.Binaries)
	}

	// Append a new probe (not yet present) → appended.
	if err := repo.AppendProbeResult(ctx, "host-a", BinaryProbe{Name: "go", Present: true, Version: "1.22.3"}); err != nil {
		t.Fatalf("append go: %v", err)
	}
	latest, _ = repo.LatestForHost(ctx, "host-a")
	if len(latest.Binaries) != 2 {
		t.Fatalf("want 2 binaries, got %d", len(latest.Binaries))
	}
}

func TestHostCapabilitySnapshot_HasCapability(t *testing.T) {
	snap := HostCapabilitySnapshot{
		Capabilities: []Capability{
			{Name: "can-compile-c", Satisfied: true},
			{Name: "can-run-python", Satisfied: false},
		},
	}
	if !snap.HasCapability("can-compile-c") {
		t.Fatal("expected can-compile-c to be satisfied")
	}
	if snap.HasCapability("can-run-python") {
		t.Fatal("expected can-run-python to be unsatisfied")
	}
	if snap.HasCapability("can-nothing") {
		t.Fatal("unknown capability should return false")
	}
}
