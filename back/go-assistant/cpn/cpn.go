package cpn

import "sync"

// CPN is the universal agent type.
// At Depth=0: root coordinator. At Depth=1: domain coordinator. At Depth=2+: worker.
// A CPN contains places and transitions that form a Coloured Petri Net.
type CPN struct {
	ID    string
	Role  string
	Depth int

	Mode  Mode
	State State
	Error error

	Places      map[string]*Place
	Transitions map[string]*Transition

	SessionID string

	mu sync.RWMutex
}

// NewCPN creates a CPN in StateIdle. Call Run(ctx) to start execution.
// Validation is performed at the start of Run, not at construction.
func NewCPN(id, role string, depth int, mode Mode, sessionID string,
	places map[string]*Place, transitions map[string]*Transition) *CPN {
	return &CPN{
		ID:          id,
		Role:        role,
		Depth:       depth,
		Mode:        mode,
		State:       StateIdle,
		Places:      places,
		Transitions: transitions,
		SessionID:   sessionID,
	}
}

// setState sets the CPN state under write lock.
func (c *CPN) setState(s State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = s
}

// getState returns the CPN state under read lock.
func (c *CPN) getState() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.State
}

// TerminalPlaces returns places not referenced as inputs to any transition.
// These are the "output" places of the CPN.
func (c *CPN) TerminalPlaces() []*Place {
	inputRefs := make(map[string]bool)
	for _, t := range c.Transitions {
		for _, pid := range t.InputPlaces {
			inputRefs[pid] = true
		}
	}

	var terminals []*Place
	for id, p := range c.Places {
		if !inputRefs[id] {
			terminals = append(terminals, p)
		}
	}
	return terminals
}

// IsComplete returns true when ALL terminal places have at least one token.
// Returns false if there are no terminal places.
func (c *CPN) IsComplete() bool {
	terminals := c.TerminalPlaces()
	if len(terminals) == 0 {
		return false
	}
	for _, p := range terminals {
		if p.Len() == 0 {
			return false
		}
	}
	return true
}
