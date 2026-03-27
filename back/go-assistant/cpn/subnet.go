package cpn

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// SubNetEventBusCapacity is the buffer size for child event bus channels.
// Covers a typical child lifecycle (10-20 transitions). If the bus fills,
// events are silently dropped — critical state is communicated via token deposit.
const SubNetEventBusCapacity = 64

// fireSubNet spawns a child CPN in a new goroutine.
//
// The child runs with an isolated place namespace. Input tokens from the parent
// are injected as the child's initial marking. When the child completes, its
// terminal place tokens are deposited into the parent's output places, and a
// compressed summary is added to the parent's history.
//
// The parent does NOT block — it continues its executor loop while the child runs.
func fireSubNet(ctx context.Context, t *Transition, parent *CPN, consumed []Token) error {
	// REQ-001/CON-003: SubNetFactory takes precedence over SubNet.
	var child *CPN
	var err error
	switch {
	case t.SubNetFactory != nil:
		child = t.SubNetFactory()
		if child == nil {
			return fmt.Errorf("transition %s: SubNetFactory returned nil", t.ID)
		}
	case t.SubNet != nil:
		child, err = cloneCPN(t.SubNet)
		if err != nil {
			return fmt.Errorf("transition %s: cloneCPN: %w", t.ID, err)
		}
	default:
		return fmt.Errorf("transition %s: neither SubNet nor SubNetFactory configured", t.ID)
	}

	// REQ-002: Set child metadata.
	childID, err := newUUID()
	if err != nil {
		return fmt.Errorf("transition %s: %w", t.ID, err)
	}
	child.ID = childID
	child.Depth = parent.Depth + 1
	child.SessionID = parent.SessionID

	// REQ-003: Create buffered event bus and wire it.
	bus := make(chan Event, SubNetEventBusCapacity)
	child.EventEmitter = bus
	parent.registerSubNetBus(child.ID, bus)

	// REQ-004: Inject consumed tokens into child's source places.
	injectTokens(child, consumed)

	// REQ-005: Register child in parent's group (lazy-init group if nil).
	// Use subNetMu to protect lazy-init of Group — fireSubNet may be called
	// concurrently from multiple transition goroutines.
	parent.subNetMu.Lock()
	if parent.Group == nil {
		parent.Group = NewGroupAgent(parent.ID+"-group", "subnet")
	}
	parent.subNetMu.Unlock()
	parent.Group.Register(child)

	// REQ-018: Emit start event.
	parent.emit(&Event{
		Type:           EventSubNetStarted,
		SessionID:      parent.SessionID,
		TransitionID:   t.ID,
		TransitionKind: NodeKindSubNet,
		Timestamp:      time.Now(),
	})

	// REQ-006/REQ-007: Launch child in a tracked goroutine — parent does NOT block.
	parent.activeChildren.Add(1)
	parent.childWg.Go(func() {
		defer parent.activeChildren.Add(-1)
		defer close(bus)

		runErr := child.Run(ctx)

		if runErr != nil {
			// REQ-011: Child failure handling.
			parent.emit(&Event{
				Type:           EventSubNetFailed,
				SessionID:      parent.SessionID,
				TransitionID:   t.ID,
				TransitionKind: NodeKindSubNet,
				Payload:        runErr,
				Timestamp:      time.Now(),
			})

			if t.ErrorPlace != "" {
				if ep, ok := parent.Places[t.ErrorPlace]; ok {
					errToken := &Token{
						Color:       ColorError,
						Payload:     fmt.Sprintf("%s: %v", ErrSubNetFailed, runErr),
						Space:       ep.Space,
						OriginID:    child.ID,
						OriginDepth: child.Depth,
						OriginKind:  NodeKindSubNet,
						SessionID:   parent.SessionID,
						Timestamp:   time.Now(),
					}
					_ = ep.Deposit(errToken) // best-effort error routing
				}
			} else {
				parent.setFailed(fmt.Errorf("%w: %v", ErrSubNetFailed, runErr))
			}

			parent.Group.Deregister(child.ID)
			return
		}

		// REQ-008: Collect terminal tokens, enrich, and deposit into parent's output places.
		terminals := child.TerminalPlaces()
		var outputTokens []Token
		for _, tp := range terminals {
			toks, ok := tp.Peek()
			if !ok {
				continue
			}
			for _, tok := range toks {
				tok.OriginID = child.ID
				tok.OriginDepth = child.Depth
				outputTokens = append(outputTokens, *tok)
			}
		}

		// Deposit output tokens into parent's output places.
		for _, pid := range t.OutputPlaces {
			p, ok := parent.Places[pid]
			if !ok {
				continue
			}
			for i := range outputTokens {
				tok := outputTokens[i] // copy per deposit
				tok.Space = p.Space
				tok.Color = p.Color
				_ = p.Deposit(&tok) // best-effort deposit
			}
		}

		// REQ-009: Compress summary and append to parent history.
		summary, summaryErr := CompressSubNetSummary(child.ID, child.Role, child.Depth, outputTokens)
		if summaryErr != nil {
			parent.emit(&Event{
				Type:           EventSubNetFailed,
				SessionID:      parent.SessionID,
				TransitionID:   t.ID,
				TransitionKind: NodeKindSubNet,
				Payload:        fmt.Errorf("compress summary: %w", summaryErr),
				Timestamp:      time.Now(),
			})
		} else {
			parent.mu.Lock()
			parent.History = append(parent.History, summary)
			parent.mu.Unlock()
		}

		// REQ-010: Deregister and emit completion.
		parent.Group.Deregister(child.ID)
		parent.emit(&Event{
			Type:           EventSubNetCompleted,
			SessionID:      parent.SessionID,
			TransitionID:   t.ID,
			TransitionKind: NodeKindSubNet,
			Timestamp:      time.Now(),
		})
	})

	return nil
}

// cloneCPN creates a new CPN from a prototype.
// New Places are created with the same IDs, Colors, and Spaces but empty token buffers.
// Transitions are shared (they are stateless descriptors).
// ID, Depth, SessionID are NOT set — caller must set them.
func cloneCPN(prototype *CPN) (*CPN, error) {
	if prototype == nil {
		return nil, fmt.Errorf("cloneCPN: nil prototype")
	}

	// Create new Places with same identity but empty tokens.
	places := make(map[string]*Place, len(prototype.Places))
	for id, p := range prototype.Places {
		places[id] = NewPlace(p.ID, p.Color, p.Space)
	}

	return &CPN{
		Role:        prototype.Role,
		Mode:        prototype.Mode,
		State:       StateIdle,
		Places:      places,
		Transitions: prototype.Transitions,
		LLMClient:   prototype.LLMClient,
	}, nil
}

// injectTokens deposits consumed tokens into the child's source places.
// Source places are those not referenced as OutputPlaces of any child transition.
// Tokens are matched by Color: each token goes to the source place with the same ColorSet.
// If no color match is found, the token goes to the first source place (fallback).
func injectTokens(child *CPN, tokens []Token) {
	sources := sourcePlaces(child)
	if len(sources) == 0 {
		return
	}

	// Build color index for fast lookup.
	colorIdx := make(map[ColorSet]*Place, len(sources))
	for _, sp := range sources {
		colorIdx[sp.Color] = sp
	}

	// Sort sources by ID for deterministic fallback.
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].ID < sources[j].ID
	})
	fallback := sources[0]

	for i := range tokens {
		tok := tokens[i] // copy
		target, ok := colorIdx[tok.Color]
		if !ok {
			target = fallback
		}
		// Adjust token color/space to match target place for deposit.
		tok.Color = target.Color
		tok.Space = target.Space
		_ = target.Deposit(&tok)
	}
}

// sourcePlaces returns child places not referenced as OutputPlaces of any transition.
// These are the natural entry points for token injection.
func sourcePlaces(c *CPN) []*Place {
	outputRefs := make(map[string]bool)
	for _, t := range c.Transitions {
		for _, pid := range t.OutputPlaces {
			outputRefs[pid] = true
		}
	}

	var sources []*Place
	for id, p := range c.Places {
		if !outputRefs[id] {
			sources = append(sources, p)
		}
	}
	return sources
}
