package tool

import (
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// BootstrapFunc registers bootstrap tools (find_tools, find_skills, who_am_i)
// into a freshly-created ActiveRegistry. The catalog is passed so the
// bootstrap function can wire meta-tools that reference it.
type BootstrapFunc func(ar *ActiveRegistry, catalog *Catalog)

// SessionRegistryStore maps session IDs to their ActiveRegistry instances.
// Each session gets an independent ActiveRegistry that starts with only
// bootstrap tools. All methods are safe for concurrent use.
//
// NOTE: sessions are never evicted automatically. For the current CLI
// (short-lived process), this is acceptable. A long-running server mode
// would need TTL-based eviction or explicit cleanup.
type SessionRegistryStore struct {
	mu        sync.RWMutex
	sessions  map[string]*ActiveRegistry
	catalog   *Catalog
	bootstrap BootstrapFunc
}

// NewSessionRegistryStore creates a store that provisions new sessions with
// the given catalog and bootstrap function.
func NewSessionRegistryStore(catalog *Catalog, bootstrap BootstrapFunc) *SessionRegistryStore {
	return &SessionRegistryStore{
		sessions:  make(map[string]*ActiveRegistry),
		catalog:   catalog,
		bootstrap: bootstrap,
	}
}

// GetOrCreate returns the ActiveRegistry for the given session. If none
// exists, a new ActiveRegistry is created with bootstrap tools registered
// and stored for future calls.
func (s *SessionRegistryStore) GetOrCreate(sessionID string) *ActiveRegistry {
	s.mu.RLock()
	ar, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if ok {
		return ar
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if ar, ok = s.sessions[sessionID]; ok {
		return ar
	}

	ar = NewActiveRegistry(s.catalog)
	if s.bootstrap != nil {
		s.bootstrap(ar, s.catalog)
	}
	s.sessions[sessionID] = ar
	return ar
}

// Remove deletes the session's ActiveRegistry, freeing resources.
func (s *SessionRegistryStore) Remove(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// Count returns the number of active sessions.
func (s *SessionRegistryStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// Has reports whether a session exists in the store.
func (s *SessionRegistryStore) Has(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[sessionID]
	return ok
}

// LoadCategoryForSession loads a tool category into the given session's
// registry. Returns an error if the session doesn't exist.
func (s *SessionRegistryStore) LoadCategoryForSession(sessionID string, cat valueobject.ToolCategory) (*valueobject.ToolCategoryInfo, error) {
	ar := s.GetOrCreate(sessionID)
	return ar.LoadCategory(cat)
}
