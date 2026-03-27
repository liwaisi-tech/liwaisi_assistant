package cpn

import "sync"

// GroupAgent manages an ephemeral team of sub-CPNs for a parent CPN.
// Implements the paper's §4.2 group-agent protocol:
//
//	register()  → Register
//	deliver()   → Deliver
//	deregister()→ Deregister
//	switchCMP() → SwitchCMP
//
// Sub-CPNs are partitioned into active (Running/Idle) and non-active
// (Completed/Failed/Waiting) sets. Events are fan-out delivered only to active members.
type GroupAgent struct {
	// ID uniquely identifies this group.
	ID string

	// Topic is the semantic label for this group's coordination concern.
	Topic string

	active    []*CPN
	nonActive []*CPN
	mu        sync.RWMutex
}

// NewGroupAgent creates a GroupAgent with empty active/nonActive sets.
func NewGroupAgent(id, topic string) *GroupAgent {
	return &GroupAgent{
		ID:    id,
		Topic: topic,
	}
}

// isActiveState returns true for states that belong in the active set.
func isActiveState(s State) bool {
	return s == StateRunning || s == StateIdle
}

// Register adds a sub-CPN to the group.
// Placed in active if State is Running or Idle, otherwise in nonActive.
// Thread-safe (write lock).
func (g *GroupAgent) Register(c *CPN) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if isActiveState(c.State) {
		g.active = append(g.active, c)
	} else {
		g.nonActive = append(g.nonActive, c)
	}
}

// Deliver sends an event to all active sub-CPNs via their EventEmitter.
// Non-blocking: full or nil channels are silently skipped.
// Thread-safe (read lock).
func (g *GroupAgent) Deliver(e *Event) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, c := range g.active {
		if c.EventEmitter == nil {
			continue
		}
		select {
		case c.EventEmitter <- *e:
		default:
		}
	}
}

// Deregister removes a sub-CPN from the active set by ID.
// No-op if the ID is not found in the active set.
// Thread-safe (write lock).
func (g *GroupAgent) Deregister(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active = filterCPNs(g.active, func(c *CPN) bool {
		return c.ID != id
	})
}

// SwitchCMP moves a sub-CPN between active and nonActive based on its current state.
// Active → NonActive when State is Completed, Failed, or Waiting.
// NonActive → Active when State is Running or Idle.
// No-op if the ID is not found or the state doesn't match a transition.
// Thread-safe (write lock).
func (g *GroupAgent) SwitchCMP(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check active → nonActive.
	for i, c := range g.active {
		if c.ID == id {
			if !isActiveState(c.State) {
				g.active = filterCPNs(g.active, func(x *CPN) bool {
					return x.ID != id
				})
				_ = i // used only for the match
				g.nonActive = append(g.nonActive, c)
			}
			return
		}
	}

	// Check nonActive → active.
	for _, c := range g.nonActive {
		if c.ID == id {
			if isActiveState(c.State) {
				g.nonActive = filterCPNs(g.nonActive, func(x *CPN) bool {
					return x.ID != id
				})
				g.active = append(g.active, c)
			}
			return
		}
	}
}

// Active returns a copy of the active sub-CPN list.
// Thread-safe (read lock). Returned slice is safe to iterate without locks.
func (g *GroupAgent) Active() []*CPN {
	g.mu.RLock()
	defer g.mu.RUnlock()

	cp := make([]*CPN, len(g.active))
	copy(cp, g.active)
	return cp
}

// NonActive returns a copy of the non-active sub-CPN list.
// Thread-safe (read lock).
func (g *GroupAgent) NonActive() []*CPN {
	g.mu.RLock()
	defer g.mu.RUnlock()

	cp := make([]*CPN, len(g.nonActive))
	copy(cp, g.nonActive)
	return cp
}

// Len returns the count of active and non-active sub-CPNs.
// Thread-safe (read lock).
func (g *GroupAgent) Len() (active, nonActive int) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return len(g.active), len(g.nonActive)
}

// filterCPNs returns a filtered copy of the slice, keeping elements where keep returns true.
// Nils out removed positions to prevent *CPN memory leaks.
func filterCPNs(cpns []*CPN, keep func(*CPN) bool) []*CPN {
	n := 0
	for _, c := range cpns {
		if keep(c) {
			cpns[n] = c
			n++
		}
	}
	// Nil out trailing positions to allow GC of removed CPNs.
	for i := n; i < len(cpns); i++ {
		cpns[i] = nil
	}
	return cpns[:n]
}
