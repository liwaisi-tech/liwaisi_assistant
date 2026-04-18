//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// TestHostCapabilityRepository_SaveAndLatest validates REQ-011 (append-only)
// and REQ-013 (60 s cache) against a containerised Postgres.
func TestHostCapabilityRepository_SaveAndLatest(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	ctx := context.Background()
	repo := NewHostCapabilityRepository(pool)

	// Cold read → ErrHostSnapshotNotFound.
	if _, err := repo.LatestForHost(ctx, "host-a"); !errors.Is(err, persist.ErrHostSnapshotNotFound) {
		t.Fatalf("cold read err=%v, want ErrHostSnapshotNotFound", err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	snap1 := persist.HostCapabilitySnapshot{
		HostID: "host-a", CapturedAt: base, Source: "bootstrap",
		Identity:     persist.HostIdentity{User: "alice", MachineID: "host-a"},
		Kernel:       persist.HostKernel{OS: "Linux"},
		Binaries:     []persist.BinaryProbe{{Name: "gcc", Present: true, Version: "13.2.0"}},
		Capabilities: []persist.Capability{{Name: "can-compile-c", Satisfied: true}},
	}
	if err := repo.Save(ctx, snap1); err != nil {
		t.Fatalf("save 1: %v", err)
	}

	snap2 := snap1
	snap2.CapturedAt = base.Add(time.Hour)
	snap2.Source = "session"
	if err := repo.Save(ctx, snap2); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	latest, err := repo.LatestForHost(ctx, "host-a")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Source != "session" {
		t.Fatalf("latest.source = %q, want session", latest.Source)
	}

	// Append a probe and re-read.
	if err := repo.AppendProbeResult(ctx, "host-a",
		persist.BinaryProbe{Name: "go", Present: true, Version: "1.22.3"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	latest, _ = repo.LatestForHost(ctx, "host-a")
	found := false
	for _, b := range latest.Binaries {
		if b.Name == "go" && b.Version == "1.22.3" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("go probe not persisted: %+v", latest.Binaries)
	}
}

// TestHostCapabilityRepository_Cache verifies the 60 s TTL expires on Save.
func TestHostCapabilityRepository_CacheInvalidation(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	ctx := context.Background()
	repo := NewHostCapabilityRepository(pool)
	repo.SetCacheTTL(time.Hour) // never expires during the test

	first := persist.HostCapabilitySnapshot{
		HostID: "host-b", CapturedAt: time.Now(),
		Identity: persist.HostIdentity{MachineID: "host-b"},
	}
	_ = repo.Save(ctx, first)
	_, _ = repo.LatestForHost(ctx, "host-b") // populates cache

	// New save should invalidate the cache — next LatestForHost hits DB.
	second := first
	second.CapturedAt = first.CapturedAt.Add(time.Minute)
	second.Source = "manual"
	_ = repo.Save(ctx, second)

	latest, err := repo.LatestForHost(ctx, "host-b")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Source != "manual" {
		t.Fatalf("cache not invalidated: source=%q", latest.Source)
	}
}
