package toolapproval

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ErrInvalidKey is returned when Record or Lookup is called with an empty
// composite key component. Fail-closed (SEC-003): callers must treat this
// as "not approved".
var ErrInvalidKey = errors.New("toolapproval: invalid composite key")

// ApprovalStore persists HITL approvals keyed by
// (session_id, tool_name, provenance_sha256).
type ApprovalStore interface {
	Record(ctx context.Context, a Approval) error
	Lookup(ctx context.Context, sessionID, toolName, provenance string) (Decision, error)
	ListForSession(ctx context.Context, sessionID string) ([]Approval, error)
}

// MemoryApprovalStore is a goroutine-safe in-memory ApprovalStore.
type MemoryApprovalStore struct {
	mu  sync.RWMutex
	byK map[string]Approval
}

func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{byK: make(map[string]Approval)}
}

func compositeKey(sessionID, toolName, provenance string) (string, error) {
	if sessionID == "" || toolName == "" || provenance == "" {
		return "", ErrInvalidKey
	}
	return sessionID + "|" + toolName + "|" + provenance, nil
}

func (s *MemoryApprovalStore) Record(_ context.Context, a Approval) error {
	k, err := compositeKey(a.SessionID, a.ToolName, a.ProvenanceSHA256)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byK[k] = a
	return nil
}

func (s *MemoryApprovalStore) Lookup(_ context.Context, sessionID, toolName, provenance string) (Decision, error) {
	k, err := compositeKey(sessionID, toolName, provenance)
	if err != nil {
		return DecisionUnknown, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.byK[k]; ok {
		return DecisionApproved, nil
	}
	return DecisionUnknown, nil
}

func (s *MemoryApprovalStore) ListForSession(_ context.Context, sessionID string) ([]Approval, error) {
	if sessionID == "" {
		return nil, ErrInvalidKey
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Approval, 0)
	for _, a := range s.byK {
		if a.SessionID == sessionID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ToolName != out[j].ToolName {
			return out[i].ToolName < out[j].ToolName
		}
		return out[i].ProvenanceSHA256 < out[j].ProvenanceSHA256
	})
	return out, nil
}
