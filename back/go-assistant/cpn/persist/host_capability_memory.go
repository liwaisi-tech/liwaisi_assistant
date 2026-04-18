package persist

import (
	"context"
	"sync"
	"time"
)

// MemoryHostCapabilityRepository is the trivial in-memory adapter used by
// tests. Snapshots are append-only; LatestForHost picks the most recent by
// CapturedAt.
type MemoryHostCapabilityRepository struct {
	mu     sync.RWMutex
	byHost map[string][]HostCapabilitySnapshot
}

// Compile-time assertion.
var _ HostCapabilityRepository = (*MemoryHostCapabilityRepository)(nil)

// NewMemoryHostCapabilityRepository creates an empty repo.
func NewMemoryHostCapabilityRepository() *MemoryHostCapabilityRepository {
	return &MemoryHostCapabilityRepository{byHost: make(map[string][]HostCapabilitySnapshot)}
}

// Reset clears all stored snapshots (test helper).
func (r *MemoryHostCapabilityRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byHost = make(map[string][]HostCapabilitySnapshot)
}

// Save appends a snapshot under s.HostID. The caller is expected to supply an
// ID; if empty, a timestamp-derived placeholder is used so tests don't need
// to generate UUIDs.
func (r *MemoryHostCapabilityRepository) Save(_ context.Context, s HostCapabilitySnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.HostID == "" {
		return ErrInvalidInput
	}
	if s.ID == "" {
		s.ID = "mem-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	if s.CapturedAt.IsZero() {
		s.CapturedAt = time.Now().UTC()
	}
	r.byHost[s.HostID] = append(r.byHost[s.HostID], s)
	return nil
}

// LatestForHost returns the most-recent snapshot by CapturedAt for hostID.
func (r *MemoryHostCapabilityRepository) LatestForHost(_ context.Context, hostID string) (HostCapabilitySnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := r.byHost[hostID]
	if len(rows) == 0 {
		return HostCapabilitySnapshot{}, ErrHostSnapshotNotFound
	}
	latest := rows[0]
	for i := 1; i < len(rows); i++ {
		if rows[i].CapturedAt.After(latest.CapturedAt) {
			latest = rows[i]
		}
	}
	return latest, nil
}

// AppendProbeResult replaces (or appends) the probe entry on the LATEST
// snapshot row for hostID. Returns ErrHostSnapshotNotFound if no baseline
// exists.
func (r *MemoryHostCapabilityRepository) AppendProbeResult(_ context.Context, hostID string, probe BinaryProbe) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.byHost[hostID]
	if len(rows) == 0 {
		return ErrHostSnapshotNotFound
	}
	idx := 0
	for i := 1; i < len(rows); i++ {
		if rows[i].CapturedAt.After(rows[idx].CapturedAt) {
			idx = i
		}
	}
	latest := rows[idx]
	replaced := false
	for i, b := range latest.Binaries {
		if b.Name == probe.Name {
			latest.Binaries[i] = probe
			replaced = true
			break
		}
	}
	if !replaced {
		latest.Binaries = append(latest.Binaries, probe)
	}
	rows[idx] = latest
	r.byHost[hostID] = rows
	return nil
}
