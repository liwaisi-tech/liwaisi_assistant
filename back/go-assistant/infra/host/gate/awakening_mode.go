package gate

import (
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
)

// AwakeningModeRegistry tracks sessions that are currently running the
// `brae-awakens` topology. While a session is registered, the host-gate
// SILENTLY denies any shell command that IsIntrospection rejects —
// no HITL card is pushed to the user (CON-003, SEC-004, AC-005).
//
// Introspection commands continue to be auto-approved regardless of the
// flag: callers should still classify them via the normal safe-band path.
//
// The registry is safe for concurrent use. It is intentionally scoped
// per-PolicyHostGate instance rather than global so tests (and potential
// multi-tenant hosts) stay isolated.
type AwakeningModeRegistry struct {
	mu       sync.RWMutex
	sessions map[string]struct{}
}

// NewAwakeningModeRegistry returns an empty registry.
func NewAwakeningModeRegistry() *AwakeningModeRegistry {
	return &AwakeningModeRegistry{sessions: make(map[string]struct{})}
}

// Begin marks sessionID as currently awakening. Safe to call multiple times.
// A nil receiver is a no-op so call-sites that optionally wire the registry
// do not need to branch.
func (r *AwakeningModeRegistry) Begin(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	r.sessions[sessionID] = struct{}{}
	r.mu.Unlock()
}

// End clears the awakening flag for sessionID. Safe to call on a session
// that is not currently flagged.
func (r *AwakeningModeRegistry) End(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

// Active reports whether sessionID is currently in awakening mode.
func (r *AwakeningModeRegistry) Active(sessionID string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.RLock()
	_, ok := r.sessions[sessionID]
	r.mu.RUnlock()
	return ok
}

// isIntrospectionCommand is an indirection so the gate does not take a
// hard dependency on awakens in case we later move the classifier. Keeps
// the import site local.
func isIntrospectionCommand(cmd string) bool {
	return awakens.IsIntrospection(cmd)
}
