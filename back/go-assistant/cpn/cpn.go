package cpn

import (
	"sync"
	"sync/atomic"
	"time"
)

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

	// LLMClient is the LLM API client shared by all LLM transitions.
	LLMClient LLMClient

	// History holds the conversation history for context assembly.
	History []*Message

	// ContextWindowSize is the max conversational turns for the sliding window.
	// Default: DefaultContextWindowSize (10).
	ContextWindowSize int

	// Group manages this CPN's sub-CPNs (ephemeral team).
	// Nil for leaf CPNs that do not spawn sub-nets.
	Group *GroupAgent

	// EventSink receives events emitted by this CPN.
	// Called synchronously by emit(). Nil means events are only sent to EventEmitter.
	// Decouples from concrete event bus (CON-004).
	EventSink func(Event)

	// GroupNotifier is called on state changes that affect group membership.
	// Nil means no group coordination (standalone CPN or testing).
	// Implemented by GroupAgent (Block 11).
	GroupNotifier GroupNotifier

	// EventEmitter is the channel where this CPN's events are sent.
	// Read by the parent CPN's observer transitions and GroupAgent.Deliver.
	// Nil if this CPN is not a sub-CPN.
	EventEmitter chan<- Event

	// subNetBuses maps child CPN IDs to their event bus channels.
	// Used by Observer transitions (Block 13) to drain child events.
	subNetBuses map[string]<-chan Event
	subNetMu    sync.Mutex

	// childWg tracks active child goroutines for graceful shutdown.
	childWg sync.WaitGroup

	// activeChildren counts running child goroutines.
	// Used by Run to avoid false deadlock when children are still producing tokens.
	activeChildren atomic.Int32

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

// setFailed atomically sets State=StateFailed and Error under a single write lock.
// Prevents concurrent readers from observing StateFailed with a nil Error.
func (c *CPN) setFailed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = StateFailed
	c.Error = err
}

// getState returns the CPN state under read lock.
func (c *CPN) getState() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.State
}

// getError returns the CPN error under read lock.
// Pairs with setFailed which writes Error under write lock.
func (c *CPN) getError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Error
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

// emit sends an event, stamping CPN identity and timestamp.
// REQ-012: Stamps CPNID, CPNDepth, CPNRole, SessionID, Timestamp on the event.
// Calls EventSink synchronously (if non-nil), then sends to EventEmitter (non-blocking).
// No-op if both EventSink and EventEmitter are nil.
func (c *CPN) emit(e *Event) {
	e.CPNID = c.ID
	e.CPNDepth = c.Depth
	e.CPNRole = c.Role
	e.SessionID = c.SessionID
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	if c.EventSink != nil {
		c.EventSink(*e)
	}

	if c.EventEmitter != nil {
		select {
		case c.EventEmitter <- *e:
		default:
		}
	}
}

// switchModeToCentaurian sets Mode to ModeCentaurian and emits EventModeSwitch.
// REQ-013: Called when a human token enters a SpaceComputation place via HITL.
func (c *CPN) switchModeToCentaurian() {
	c.mu.Lock()
	c.Mode = ModeCentaurian
	c.mu.Unlock()

	c.emit(&Event{
		Type:    EventModeSwitch,
		Payload: ModeCentaurian,
	})
}

// registerSubNetBus registers a child's event bus channel for observer draining.
// Lazy-initializes the subNetBuses map. Thread-safe.
func (c *CPN) registerSubNetBus(childID string, bus <-chan Event) {
	c.subNetMu.Lock()
	defer c.subNetMu.Unlock()
	if c.subNetBuses == nil {
		c.subNetBuses = make(map[string]<-chan Event)
	}
	c.subNetBuses[childID] = bus
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
