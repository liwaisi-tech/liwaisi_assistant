package cpn

import "sync"

// FlowLibrary stores crystallized CPN topologies keyed by topology hash.
// Phase 1: Register/Get only. Propose returns nil (CON-003).
// Uses sync.RWMutex — read-heavy (Get), write-infrequent (Register) (GUD-003).
type FlowLibrary struct {
	flows map[string]*CPN
	mu    sync.RWMutex
}

// NewFlowLibrary creates an empty FlowLibrary.
func NewFlowLibrary() *FlowLibrary {
	return &FlowLibrary{
		flows: make(map[string]*CPN),
	}
}

// Register stores a CPN topology under the given hash (REQ-011).
// If the hash already exists, the entry is overwritten (last write wins).
func (fl *FlowLibrary) Register(c *CPN, hash string) {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	fl.flows[hash] = c
}

// Get retrieves a CPN topology by hash (REQ-011).
// Returns nil, false if the hash is not registered.
func (fl *FlowLibrary) Get(hash string) (*CPN, bool) {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	c, ok := fl.flows[hash]
	return c, ok
}

// Propose returns candidate CPNs for a client.
// Phase 1 stub: always returns nil (CON-003, REQ-011).
func (fl *FlowLibrary) Propose(_ string) []*CPN {
	return nil
}

// Len returns the number of registered flows.
func (fl *FlowLibrary) Len() int {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	return len(fl.flows)
}
