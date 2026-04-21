package toolsynth

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ErrPendingNotFound is returned when a lookup key misses.
var ErrPendingNotFound = errors.New("toolsynth: pending tool not found")

// PendingToolStore is the staging port. SC-12 uses it to hand off
// synthesized manifests to SC-13 (HITL gate). The in-memory implementation
// satisfies the interface for tests and local runs; a Postgres-backed store
// is deferred to SC-13.
type PendingToolStore interface {
	Stage(ctx context.Context, tool PendingTool) error
	List(ctx context.Context, sessionID string) ([]PendingTool, error)
	Get(ctx context.Context, toolName string) (PendingTool, bool, error)
	Delete(ctx context.Context, toolName string) error
}

// MemoryPendingToolStore is a goroutine-safe in-memory PendingToolStore.
// Keyed by ToolManifest.Name; session membership is indexed separately so
// List(sessionID) returns the staging subset deterministically.
type MemoryPendingToolStore struct {
	mu      sync.RWMutex
	byName  map[string]PendingTool
	bySess  map[string]map[string]struct{}
}

// NewMemoryPendingToolStore returns an empty in-memory store.
func NewMemoryPendingToolStore() *MemoryPendingToolStore {
	return &MemoryPendingToolStore{
		byName: make(map[string]PendingTool),
		bySess: make(map[string]map[string]struct{}),
	}
}

func (s *MemoryPendingToolStore) Stage(_ context.Context, tool PendingTool) error {
	name := tool.Manifest.Name
	if name == "" {
		return errors.New("toolsynth: pending tool manifest missing name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byName[name] = tool
	if tool.SessionID != "" {
		set, ok := s.bySess[tool.SessionID]
		if !ok {
			set = make(map[string]struct{})
			s.bySess[tool.SessionID] = set
		}
		set[name] = struct{}{}
	}
	return nil
}

func (s *MemoryPendingToolStore) List(_ context.Context, sessionID string) ([]PendingTool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var names []string
	if sessionID == "" {
		names = make([]string, 0, len(s.byName))
		for n := range s.byName {
			names = append(names, n)
		}
	} else {
		set := s.bySess[sessionID]
		names = make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]PendingTool, 0, len(names))
	for _, n := range names {
		if t, ok := s.byName[n]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *MemoryPendingToolStore) Get(_ context.Context, toolName string) (PendingTool, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.byName[toolName]
	return t, ok, nil
}

func (s *MemoryPendingToolStore) Delete(_ context.Context, toolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byName[toolName]
	if !ok {
		return ErrPendingNotFound
	}
	delete(s.byName, toolName)
	if t.SessionID != "" {
		if set, ok := s.bySess[t.SessionID]; ok {
			delete(set, toolName)
			if len(set) == 0 {
				delete(s.bySess, t.SessionID)
			}
		}
	}
	return nil
}
