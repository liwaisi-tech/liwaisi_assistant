package cpn

import (
	"context"
	"time"
)

// drainObservers reads all pending events from sub-CPN event buses and
// the CPN's own self-bus, and deposits matching events as ColorEvent
// tokens into observer output places.
//
// Algorithm (drain-then-dispatch):
//
//	Phase 1: Non-blocking drain of ALL events from ALL SubNetBuses AND the
//	         CPN's own selfBus (GAP-9: process events from NodeKindBash
//	         transitions in THIS CPN) into a slice.
//	Phase 2: For each event, check ALL observer transitions. If ObservedCPNID
//	         and EventFilter both pass, create ColorEvent token and deposit
//	         into the observer's OutputPlaces.
//
// Runs on the main goroutine. Never blocks. Never spawns goroutines.
func drainObservers(_ context.Context, c *CPN) {
	// Snapshot sub-CPN buses under lock to avoid holding it during drain.
	c.subNetMu.Lock()
	buses := make(map[string]<-chan Event, len(c.subNetBuses))
	for id, bus := range c.subNetBuses {
		buses[id] = bus
	}
	c.subNetMu.Unlock()

	// Grab the self-bus (may be nil if no process events have been
	// emitted yet, in which case we skip it silently).
	c.selfMu.Lock()
	selfBus := c.selfBus
	c.selfMu.Unlock()

	// REQ-014: Nothing to drain.
	if len(buses) == 0 && selfBus == nil {
		return
	}

	// Collect observer transitions.
	var observers []*Transition
	for _, t := range c.Transitions {
		if t.Kind == NodeKindObserver {
			observers = append(observers, t)
		}
	}
	if len(observers) == 0 {
		return
	}

	// Phase 1: Non-blocking drain of ALL buses (sub-CPNs + self) into a
	// slice. GUD-002: Pre-allocate with estimated capacity.
	events := make([]Event, 0, len(buses)*4+4)
	for _, bus := range buses {
		for {
			select {
			case e, ok := <-bus:
				if !ok {
					goto nextBus
				}
				events = append(events, e)
			default:
				goto nextBus
			}
		}
	nextBus:
	}
	if selfBus != nil {
		for {
			select {
			case e, ok := <-selfBus:
				if !ok {
					goto selfDone
				}
				events = append(events, e)
			default:
				goto selfDone
			}
		}
	selfDone:
	}

	if len(events) == 0 {
		return
	}

	// Phase 2: Dispatch — every event is checked against EVERY observer.
	// PAT-002: Double iteration prevents event stealing.
	for i := range events {
		e := &events[i]
		for _, obs := range observers {
			// PAT-003: ObservedCPNID check first, then EventFilter.
			if obs.ObservedCPNID != "" && e.CPNID != obs.ObservedCPNID {
				continue
			}
			if obs.EventFilter != nil && !obs.EventFilter(*e) {
				continue
			}

			// REQ-007: Create ColorEvent token with observer metadata.
			tok := Token{
				Color:       ColorEvent,
				Payload:     *e,
				OriginID:    e.CPNID,
				OriginDepth: e.CPNDepth,
				OriginKind:  NodeKindObserver,
				Space:       SpaceObservation,
				SessionID:   c.SessionID,
				Timestamp:   time.Now(),
			}

			// REQ-008/REQ-009: Deposit a copy into each output place.
			for _, pid := range obs.OutputPlaces {
				p, ok := c.Places[pid]
				if !ok {
					continue // CON-004: silently ignore missing places.
				}
				tokCopy := tok // GUD-003: independent copy per output place.
				_ = p.Deposit(&tokCopy)
			}
		}
	}
}
