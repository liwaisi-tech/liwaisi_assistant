package synthesis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// MemoryAuthoredFlowRepository is an in-memory implementation of
// cpn.AuthoredFlowRepository. Intended for tests and dev mode; every
// write is idempotent by SHA-256 of the canonical JSON.
type MemoryAuthoredFlowRepository struct {
	mu    sync.RWMutex
	flows map[string]*cpn.AuthoredFlowRecord
	order []string // insertion order for deterministic List output
}

// NewMemoryAuthoredFlowRepository returns a fresh in-memory repo.
func NewMemoryAuthoredFlowRepository() *MemoryAuthoredFlowRepository {
	return &MemoryAuthoredFlowRepository{
		flows: make(map[string]*cpn.AuthoredFlowRecord),
	}
}

// SaveAuthored hashes the canonical JSON and either inserts a new entry
// or returns the existing flow_id unchanged.
func (r *MemoryAuthoredFlowRepository) SaveAuthored(
	_ context.Context,
	canonicalJSON json.RawMessage,
	summary string,
	sizePlaces, sizeTransitions int,
	referenced []string,
	prov cpn.AuthoredFlowProvenance,
) (string, bool, error) {
	if len(canonicalJSON) == 0 {
		return "", false, errors.New("synthesis: empty topology JSON")
	}
	sum := sha256.Sum256(canonicalJSON)
	flowID := hex.EncodeToString(sum[:])

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.flows[flowID]; ok {
		return flowID, false, nil
	}
	r.flows[flowID] = &cpn.AuthoredFlowRecord{
		FlowID:          flowID,
		TopologyJSON:    append(json.RawMessage(nil), canonicalJSON...),
		Summary:         summary,
		SafeLintPassed:  true,
		SizePlaces:      sizePlaces,
		SizeTransitions: sizeTransitions,
		Provenance:      prov,
	}
	_ = referenced
	r.order = append(r.order, flowID)
	return flowID, true, nil
}

// GetByID returns a deep-copied record, or an error on miss.
func (r *MemoryAuthoredFlowRepository) GetByID(_ context.Context, flowID string) (*cpn.AuthoredFlowRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.flows[flowID]
	if !ok {
		return nil, errors.New("synthesis: flow not found")
	}
	cp := *rec
	cp.TopologyJSON = append(json.RawMessage(nil), rec.TopologyJSON...)
	return &cp, nil
}

// Reject flips the rejection flag.
func (r *MemoryAuthoredFlowRepository) Reject(_ context.Context, flowID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.flows[flowID]
	if !ok {
		return errors.New("synthesis: flow not found")
	}
	rec.Rejected = true
	rec.RejectedReason = reason
	return nil
}

// ListAuthored returns every flow in insertion order (newest last).
func (r *MemoryAuthoredFlowRepository) ListAuthored(_ context.Context, limit int) ([]*cpn.AuthoredFlowRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*cpn.AuthoredFlowRecord, 0, len(r.order))
	for _, id := range r.order {
		rec := r.flows[id]
		cp := *rec
		cp.TopologyJSON = append(json.RawMessage(nil), rec.TopologyJSON...)
		out = append(out, &cp)
	}
	// Stable sort by flow id for deterministic callers.
	sort.SliceStable(out, func(i, j int) bool { return out[i].FlowID < out[j].FlowID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
