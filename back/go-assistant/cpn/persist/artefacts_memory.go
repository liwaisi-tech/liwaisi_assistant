package persist

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryArtefactLedger is a thread-safe in-memory AuthoredArtefactLedger.
// It is intended for tests and single-process deployments where durability
// is not required. It mirrors the Postgres adapter behaviourally so the
// RollbackService / PurgeService tests can exercise the full flow without
// spinning up a database.
type MemoryArtefactLedger struct {
	mu        sync.RWMutex
	artefacts map[string]*Artefact // keyed by artefact ID
	bySet     map[string][]string  // set_id → [artefact_id...]
	byForge   map[string][]string  // forge_run_id → [artefact_id...]
	events    map[string][]ArtefactEvent
	now       func() time.Time
}

var _ AuthoredArtefactLedger = (*MemoryArtefactLedger)(nil)

// NewMemoryArtefactLedger constructs an empty in-memory ledger.
func NewMemoryArtefactLedger() *MemoryArtefactLedger {
	return &MemoryArtefactLedger{
		artefacts: make(map[string]*Artefact),
		bySet:     make(map[string][]string),
		byForge:   make(map[string][]string),
		events:    make(map[string][]ArtefactEvent),
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the clock used for CreatedAt / QuarantinedAt / RestoredAt
// timestamps. Used by the purge-service test to exercise the grace-window
// logic with a deterministic time source.
func (m *MemoryArtefactLedger) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

// PreWrite reserves an artefact row.
func (m *MemoryArtefactLedger) PreWrite(_ context.Context, intent WriteIntent, path string, mode uint32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New().String()
	setID := intent.SetID
	if setID == "" {
		setID = "singleton-" + id
	}
	classification := intent.Classification
	if classification == "" {
		classification = ClassOther
	}
	art := &Artefact{
		ID:             id,
		Path:           path,
		Classification: classification,
		SetID:          setID,
		ForgeRunID:     intent.ForgeRunID,
		HostID:         intent.HostID,
		FlowHash:       intent.FlowHash,
		AuthoringCPNID: intent.AuthoringCPNID,
		TransitionID:   intent.TransitionID,
		SessionID:      intent.SessionID,
		Mode:           mode,
		State:          ArtefactStatePending,
		CreatedAt:      m.now(),
	}
	m.artefacts[id] = art
	m.bySet[setID] = append(m.bySet[setID], id)
	if intent.ForgeRunID != "" {
		m.byForge[intent.ForgeRunID] = append(m.byForge[intent.ForgeRunID], id)
	}
	return id, nil
}

// PostWrite records the hash, size and MIME.
func (m *MemoryArtefactLedger) PostWrite(_ context.Context, id, sha256 string, size int64, mime string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	art, ok := m.artefacts[id]
	if !ok {
		return ErrArtefactNotFound
	}
	art.SHA256 = sha256
	art.Size = size
	art.MIME = mime
	art.State = ArtefactStateActive
	return nil
}

// GetByID fetches a single artefact.
func (m *MemoryArtefactLedger) GetByID(_ context.Context, id string) (Artefact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	art, ok := m.artefacts[id]
	if !ok {
		return Artefact{}, ErrArtefactNotFound
	}
	return *art, nil
}

// SetForForgeRun returns every artefact in a forge run.
func (m *MemoryArtefactLedger) SetForForgeRun(_ context.Context, forgeRunID string) ([]Artefact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.byForge[forgeRunID]
	out := make([]Artefact, 0, len(ids))
	for _, id := range ids {
		if art, ok := m.artefacts[id]; ok {
			out = append(out, *art)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// SetForSet returns every artefact sharing a set_id.
func (m *MemoryArtefactLedger) SetForSet(_ context.Context, setID string) ([]Artefact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.bySet[setID]
	if len(ids) == 0 {
		return nil, ErrArtefactSetEmpty
	}
	out := make([]Artefact, 0, len(ids))
	for _, id := range ids {
		if art, ok := m.artefacts[id]; ok {
			out = append(out, *art)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Rollback moves every row in set_id to state "quarantined".
func (m *MemoryArtefactLedger) Rollback(_ context.Context, setID, quarantinePath, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.bySet[setID]
	if len(ids) == 0 {
		return ErrArtefactSetEmpty
	}
	// Reject if all rows already quarantined or purged.
	allQuarantined := true
	for _, id := range ids {
		art := m.artefacts[id]
		if art == nil {
			continue
		}
		if art.State != ArtefactStateQuarantined && art.State != ArtefactStatePurged {
			allQuarantined = false
			break
		}
	}
	if allQuarantined {
		return ErrArtefactAlreadyRolledBack
	}
	ts := m.now()
	for _, id := range ids {
		art := m.artefacts[id]
		if art == nil {
			continue
		}
		tsCopy := ts
		art.QuarantinedAt = &tsCopy
		art.QuarantinePath = quarantinePath
		art.State = ArtefactStateQuarantined
	}
	m.events[setID] = append(m.events[setID], ArtefactEvent{
		EventID: uuid.New().String(),
		SetID:   setID,
		Kind:    "rollback",
		Actor:   actor,
		At:      ts,
	})
	return nil
}

// Restore flips every row in set_id back to state "restored".
func (m *MemoryArtefactLedger) Restore(_ context.Context, setID, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.bySet[setID]
	if len(ids) == 0 {
		return ErrArtefactSetEmpty
	}
	ts := m.now()
	anyQuarantined := false
	for _, id := range ids {
		art := m.artefacts[id]
		if art == nil {
			continue
		}
		if art.State != ArtefactStateQuarantined {
			continue
		}
		anyQuarantined = true
		tsCopy := ts
		art.RestoredAt = &tsCopy
		art.State = ArtefactStateRestored
	}
	if !anyQuarantined {
		return ErrArtefactNotQuarantined
	}
	m.events[setID] = append(m.events[setID], ArtefactEvent{
		EventID: uuid.New().String(),
		SetID:   setID,
		Kind:    "restore",
		Actor:   actor,
		At:      ts,
	})
	return nil
}

// PurgeExpired drops every row whose quarantined_at is older than cutoff.
func (m *MemoryArtefactLedger) PurgeExpired(_ context.Context, cutoff time.Time, actor string) ([]Artefact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Artefact, 0, len(m.artefacts))
	setsPurged := make(map[string]struct{})
	ts := m.now()
	for _, art := range m.artefacts {
		if art.State != ArtefactStateQuarantined {
			continue
		}
		if art.QuarantinedAt == nil || !art.QuarantinedAt.Before(cutoff) {
			continue
		}
		tsCopy := ts
		art.PurgedAt = &tsCopy
		art.State = ArtefactStatePurged
		out = append(out, *art)
		setsPurged[art.SetID] = struct{}{}
	}
	for setID := range setsPurged {
		m.events[setID] = append(m.events[setID], ArtefactEvent{
			EventID: uuid.New().String(),
			SetID:   setID,
			Kind:    "purge",
			Actor:   actor,
			At:      ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListByHost returns artefacts matching the filter.
func (m *MemoryArtefactLedger) ListByHost(_ context.Context, hostID string, filter ArtefactFilter) ([]Artefact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Artefact, 0, len(m.artefacts))
	for _, art := range m.artefacts {
		if hostID != "" && art.HostID != hostID {
			continue
		}
		if filter.ForgeRunID != "" && art.ForgeRunID != filter.ForgeRunID {
			continue
		}
		if filter.SetID != "" && art.SetID != filter.SetID {
			continue
		}
		if filter.State != "" && art.State != filter.State {
			continue
		}
		if len(filter.ClassificationIn) > 0 {
			match := false
			for _, c := range filter.ClassificationIn {
				if art.Classification == c {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, *art)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			return []Artefact{}, nil
		}
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// RecordEvent appends an audit row.
func (m *MemoryArtefactLedger) RecordEvent(_ context.Context, ev ArtefactEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.EventID == "" {
		ev.EventID = uuid.New().String()
	}
	if ev.At.IsZero() {
		ev.At = m.now()
	}
	m.events[ev.SetID] = append(m.events[ev.SetID], ev)
	return nil
}

// ListEvents returns the chronological audit trail for a set_id.
func (m *MemoryArtefactLedger) ListEvents(_ context.Context, setID string) ([]ArtefactEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	evs := m.events[setID]
	out := make([]ArtefactEvent, len(evs))
	copy(out, evs)
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}
