package cpn

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// fireResult communicates errors from fire goroutines back to the main loop.
type fireResult struct {
	transitionID string
	err          error
}

// Run executes the CPN until completion, deadlock, timeout, or error.
//
// Algorithm:
//  1. Validate topology (Validate from Block 5)
//  2. Set State=Running
//  3. Loop:
//     a. Check context cancellation
//     b. Collect firable transitions (sorted by ID, NodeKindObserver excluded)
//     c. If none firable: check IsComplete -> Completed, else -> Deadlock
//     d. For each firable: re-check CanFire, consume tokens, launch goroutine
//     e. wg.Wait() for all goroutines
//     f. Process errors: ErrorPlace routing or CPN failure
//     g. Repeat
//
// NodeKindTool (Block 6), NodeKindLLM (Block 9), and NodeKindValidate (Block 10) are dispatched. Other kinds return ErrInvalidNodeKind.
func (c *CPN) Run(ctx context.Context) error {
	// REQ-003: Validate before entering the loop.
	if err := Validate(c.Places, c.Transitions); err != nil {
		c.setFailed(err)
		return err
	}

	// REQ-004: Set running after successful validation.
	c.setState(StateRunning)

	for {
		// REQ-015: Check context cancellation.
		select {
		case <-ctx.Done():
			c.setFailed(ErrTimeout)
			return ErrTimeout
		default:
		}

		// REQ-005: Collect firable transitions (exclude Observer).
		firable := c.collectFirable()

		// REQ-014: No firable transitions.
		if len(firable) == 0 {
			return c.checkCompletionOrDeadlock()
		}

		// REQ-020: Sort firable by ID for deterministic behavior.
		sort.Slice(firable, func(i, j int) bool {
			return firable[i].ID < firable[j].ID
		})

		// Consume-before-launch pattern (PAT-001).
		// REQ-006/007: Consume on main goroutine, re-check CanFire.
		type firing struct {
			transition *Transition
			consumed   []Token
		}
		var firings []firing
		for _, t := range firable {
			if !t.CanFire(c.Places) {
				continue // PAT-002: re-check after prior consumption
			}
			consumed := consumeAll(t.InputPlaces, c.Places)
			firings = append(firings, firing{transition: t, consumed: consumed})
		}

		if len(firings) == 0 {
			// All became unfirable after re-check.
			return c.checkCompletionOrDeadlock()
		}

		// PAT-003: WaitGroup + error channel.
		var wg sync.WaitGroup
		errCh := make(chan fireResult, len(firings))

		for _, f := range firings {
			wg.Add(1)
			go func(t *Transition, consumed []Token) {
				defer wg.Done()
				// REQ-008: fireWithRetry integrates Block 4 retry.
				err := fireWithRetry(ctx, t.Retry, t.CircuitBreaker(), func() error {
					return dispatch(ctx, t, c, consumed)
				})
				if err != nil {
					errCh <- fireResult{transitionID: t.ID, err: err}
				}
			}(f.transition, f.consumed)
		}

		// SEC-002: Wait for all goroutines before processing errors.
		wg.Wait()
		close(errCh)

		// Process errors from fire goroutines.
		for fr := range errCh {
			t := c.Transitions[fr.transitionID]

			// REQ-010: ErrorPlace routing.
			if t.ErrorPlace != "" {
				ep, ok := c.Places[t.ErrorPlace]
				if ok {
					errToken := &Token{
						Color:       ColorError,
						Payload:     fr.err.Error(),
						Space:       ep.Space,
						OriginID:    c.ID,
						OriginDepth: c.Depth,
						OriginKind:  t.Kind,
						SessionID:   c.SessionID,
						Timestamp:   time.Now(),
					}
					if depErr := ep.Deposit(errToken); depErr != nil {
						c.setFailed(fmt.Errorf("error place deposit failed: %w", depErr))
						return c.Error
					}
					continue
				}
			}

			// REQ-011: No ErrorPlace — CPN fails.
			c.setFailed(fr.err)
			return fr.err
		}
	}
}

// checkCompletionOrDeadlock returns nil if the CPN is complete, or ErrDeadlock otherwise.
func (c *CPN) checkCompletionOrDeadlock() error {
	if c.IsComplete() {
		c.setState(StateCompleted)
		return nil
	}
	c.setFailed(ErrDeadlock)
	return ErrDeadlock
}

// collectFirable returns transitions that can fire, excluding observers.
func (c *CPN) collectFirable() []*Transition {
	var firable []*Transition
	for _, t := range c.Transitions {
		if t.Kind == NodeKindObserver {
			continue
		}
		if t.CanFire(c.Places) {
			firable = append(firable, t)
		}
	}
	return firable
}

// consumeAll removes one token from each input place.
// Called on the main goroutine before launching fire goroutines.
func consumeAll(placeIDs []string, places map[string]*Place) []Token {
	tokens := make([]Token, 0, len(placeIDs))
	for _, pid := range placeIDs {
		p, ok := places[pid]
		if !ok {
			continue // Defensive: Validate + CanFire should prevent this.
		}
		t, err := p.Consume()
		if err != nil {
			continue // Defensive: CanFire was checked before consuming.
		}
		tokens = append(tokens, *t)
	}
	return tokens
}

// dispatch routes a transition to the correct fire function based on Kind.
// NodeKindTool (Block 6), NodeKindLLM (Block 9), and NodeKindValidate (Block 10) are implemented.
func dispatch(ctx context.Context, t *Transition, c *CPN, consumed []Token) error {
	switch t.Kind {
	case NodeKindTool:
		return fireTool(ctx, t, c, consumed)
	case NodeKindLLM:
		return fireLLM(ctx, t, c, consumed)
	case NodeKindValidate:
		return fireValidate(ctx, t, c, consumed)
	case NodeKindSubNet:
		return fmt.Errorf("%w: SubNet dispatch not implemented", ErrInvalidNodeKind)
	case NodeKindObserver:
		return fmt.Errorf("%w: Observer dispatch not implemented", ErrInvalidNodeKind)
	case NodeKindHITL:
		return fmt.Errorf("%w: HITL dispatch not implemented", ErrInvalidNodeKind)
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidNodeKind, t.Kind)
	}
}

// fireTool executes a NodeKindTool transition.
// REQ-016: Calls t.Executor, stamps origin metadata, deposits result in OutputPlaces.
func fireTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) error {
	if t.Executor == nil {
		return fmt.Errorf("transition %s: nil Executor", t.ID)
	}

	if len(consumed) == 0 {
		return fmt.Errorf("transition %s: no consumed tokens", t.ID)
	}

	// Call the tool executor with the first consumed token.
	result, err := t.Executor(ctx, consumed[0])
	if err != nil {
		return err
	}

	// REQ-019: Stamp origin metadata.
	result.OriginID = c.ID
	result.OriginDepth = c.Depth
	result.OriginKind = NodeKindTool
	result.SessionID = c.SessionID
	result.Timestamp = time.Now()

	// Deposit result into all output places.
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := result // copy per output place
		if err := p.Deposit(&tok); err != nil {
			return fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return nil
}
