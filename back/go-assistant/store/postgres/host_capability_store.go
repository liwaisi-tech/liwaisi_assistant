package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// HostCapabilityRepository is the Postgres-backed implementation of the
// persist.HostCapabilityRepository port (GAP-2 / REQ-010..013).
//
// Reads are served from a process-local 60 s TTL cache (REQ-013) so a burst
// of session starts — each looking up LatestForHost — does NOT hammer
// Postgres. Save invalidates the cache entry for the affected host.
type HostCapabilityRepository struct {
	pool pgxPool

	cacheTTL time.Duration
	cacheMu  sync.Mutex
	cache    map[string]hostCapCacheEntry

	now func() time.Time // injectable clock for tests
}

type hostCapCacheEntry struct {
	snapshot  persist.HostCapabilitySnapshot
	expiresAt time.Time
}

var _ persist.HostCapabilityRepository = (*HostCapabilityRepository)(nil)

// DefaultHostCapabilityCacheTTL is the REQ-013 cache lifetime.
const DefaultHostCapabilityCacheTTL = 60 * time.Second

// NewHostCapabilityRepository constructs the adapter with the default 60 s
// cache TTL.
func NewHostCapabilityRepository(pool pgxPool) *HostCapabilityRepository {
	return &HostCapabilityRepository{
		pool:     pool,
		cacheTTL: DefaultHostCapabilityCacheTTL,
		cache:    make(map[string]hostCapCacheEntry),
		now:      time.Now,
	}
}

// SetCacheTTL overrides the default cache TTL (test helper).
func (r *HostCapabilityRepository) SetCacheTTL(d time.Duration) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cacheTTL = d
	r.cache = make(map[string]hostCapCacheEntry)
}

// FlushHost invalidates the in-memory cache entry for hostID so the next
// LatestForHost re-reads from Postgres. Persisted rows are append-only
// (REQ-011) and remain as the audit trail — this only resets the REQ-013
// 60 s read cache, which is what admin-driven awakening-cache flush
// (REQ-305c) needs.
func (r *HostCapabilityRepository) FlushHost(_ context.Context, hostID string) error {
	if hostID == "" {
		return persist.ErrInvalidInput
	}
	r.cacheMu.Lock()
	delete(r.cache, hostID)
	r.cacheMu.Unlock()
	return nil
}

// Save inserts a new row. The adapter ignores s.ID (the DB generates a UUID
// via gen_random_uuid()) and stamps CapturedAt = NOW() if unset.
func (r *HostCapabilityRepository) Save(ctx context.Context, s persist.HostCapabilitySnapshot) error {
	if s.HostID == "" {
		return persist.ErrInvalidInput
	}
	if s.Source == "" {
		s.Source = persist.HostSnapshotSourceSession
	}

	identity, err := json.Marshal(s.Identity)
	if err != nil {
		return fmt.Errorf("postgres host capability save: marshal identity: %w", err)
	}
	kernel, err := json.Marshal(s.Kernel)
	if err != nil {
		return fmt.Errorf("postgres host capability save: marshal kernel: %w", err)
	}
	binaries, err := marshalSliceOrEmpty(s.Binaries)
	if err != nil {
		return fmt.Errorf("postgres host capability save: marshal binaries: %w", err)
	}
	capabilities, err := marshalSliceOrEmpty(s.Capabilities)
	if err != nil {
		return fmt.Errorf("postgres host capability save: marshal capabilities: %w", err)
	}
	var rawProbes any
	if len(s.RawProbes) > 0 {
		rawProbes = []byte(s.RawProbes)
	}

	capturedAt := s.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = r.now().UTC()
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO host_capability_snapshots
			(host_id, captured_at, source, identity, kernel, binaries, capabilities, raw_probes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.HostID, capturedAt, s.Source,
		identity, kernel, binaries, capabilities, rawProbes,
	)
	if err != nil {
		return fmt.Errorf("postgres host capability save: %w", err)
	}

	// Invalidate cache for this host — next LatestForHost reloads from DB.
	r.cacheMu.Lock()
	delete(r.cache, s.HostID)
	r.cacheMu.Unlock()
	return nil
}

// LatestForHost serves from cache when fresh; otherwise reads the most recent
// row and refills the cache.
func (r *HostCapabilityRepository) LatestForHost(ctx context.Context, hostID string) (persist.HostCapabilitySnapshot, error) {
	if hostID == "" {
		return persist.HostCapabilitySnapshot{}, persist.ErrInvalidInput
	}

	// Cache hit (REQ-013).
	r.cacheMu.Lock()
	if entry, ok := r.cache[hostID]; ok && entry.expiresAt.After(r.now()) {
		r.cacheMu.Unlock()
		return entry.snapshot, nil
	}
	r.cacheMu.Unlock()

	row := r.pool.QueryRow(ctx,
		`SELECT id, host_id, captured_at, source, identity, kernel, binaries, capabilities, raw_probes
		 FROM host_capability_snapshots
		 WHERE host_id = $1
		 ORDER BY captured_at DESC
		 LIMIT 1`, hostID,
	)
	snap, err := scanHostSnapshot(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return persist.HostCapabilitySnapshot{}, persist.ErrHostSnapshotNotFound
		}
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("postgres host capability latest: %w", err)
	}

	r.cacheMu.Lock()
	r.cache[hostID] = hostCapCacheEntry{
		snapshot:  snap,
		expiresAt: r.now().Add(r.cacheTTL),
	}
	r.cacheMu.Unlock()
	return snap, nil
}

// AppendProbeResult updates the `binaries` JSONB array on the LATEST row for
// hostID. Replaces the existing entry with the same probe.Name, else appends.
// Uses a jsonb_path_query / jsonb_agg filter so the operation is atomic on
// a single row.
func (r *HostCapabilityRepository) AppendProbeResult(ctx context.Context, hostID string, probe persist.BinaryProbe) error {
	if hostID == "" || probe.Name == "" {
		return persist.ErrInvalidInput
	}
	probeJSON, err := json.Marshal(probe)
	if err != nil {
		return fmt.Errorf("postgres host capability append probe: marshal: %w", err)
	}

	// Read-modify-write within a single statement is cleaner with a CTE.
	// 1. `latest` picks the most recent row id for hostID.
	// 2. UPDATE filters the old probe out, concatenates the new value.
	tag, err := r.pool.Exec(ctx,
		`WITH latest AS (
		     SELECT id FROM host_capability_snapshots
		     WHERE host_id = $1
		     ORDER BY captured_at DESC
		     LIMIT 1
		 )
		 UPDATE host_capability_snapshots h
		 SET binaries = COALESCE(
		     (SELECT jsonb_agg(elem)
		        FROM jsonb_array_elements(h.binaries) elem
		       WHERE elem->>'name' <> $2),
		     '[]'::jsonb
		 ) || jsonb_build_array($3::jsonb)
		 FROM latest
		 WHERE h.id = latest.id`,
		hostID, probe.Name, string(probeJSON),
	)
	if err != nil {
		return fmt.Errorf("postgres host capability append probe: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrHostSnapshotNotFound
	}

	r.cacheMu.Lock()
	delete(r.cache, hostID)
	r.cacheMu.Unlock()
	return nil
}

// scanHostSnapshot reads a single row into HostCapabilitySnapshot.
func scanHostSnapshot(row pgx.Row) (persist.HostCapabilitySnapshot, error) {
	var (
		s                                                        persist.HostCapabilitySnapshot
		identityJSON, kernelJSON, binariesJSON, capabilitiesJSON []byte
		rawProbes                                                []byte
		capturedAt                                               time.Time
	)
	err := row.Scan(
		&s.ID, &s.HostID, &capturedAt, &s.Source,
		&identityJSON, &kernelJSON, &binariesJSON, &capabilitiesJSON,
		&rawProbes,
	)
	if err != nil {
		return persist.HostCapabilitySnapshot{}, err
	}
	s.CapturedAt = capturedAt
	if err := json.Unmarshal(identityJSON, &s.Identity); err != nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("unmarshal identity: %w", err)
	}
	if err := json.Unmarshal(kernelJSON, &s.Kernel); err != nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("unmarshal kernel: %w", err)
	}
	if len(binariesJSON) > 0 {
		if err := json.Unmarshal(binariesJSON, &s.Binaries); err != nil {
			return persist.HostCapabilitySnapshot{}, fmt.Errorf("unmarshal binaries: %w", err)
		}
	}
	if len(capabilitiesJSON) > 0 {
		if err := json.Unmarshal(capabilitiesJSON, &s.Capabilities); err != nil {
			return persist.HostCapabilitySnapshot{}, fmt.Errorf("unmarshal capabilities: %w", err)
		}
	}
	if len(rawProbes) > 0 {
		s.RawProbes = json.RawMessage(rawProbes)
	}
	return s, nil
}

// marshalSliceOrEmpty marshals a slice, returning "[]" for nil/empty inputs
// so the JSONB column never stores SQL NULL when a slice is unset.
func marshalSliceOrEmpty(v any) ([]byte, error) {
	if v == nil {
		return []byte(`[]`), nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 || string(out) == "null" {
		return []byte(`[]`), nil
	}
	return out, nil
}
