package cpn

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// errChildrenCompleted is an internal sentinel returned by checkCompletionOrDeadlock
// when children have finished but the parent is not yet complete. Signals the main
// loop to re-evaluate firable transitions (children may have deposited tokens that
// enable parent transitions like a merge).
var errChildrenCompleted = errors.New("children completed, re-evaluate")

// fireResult communicates errors from fire goroutines back to the main loop.
type fireResult struct {
	transitionID string
	err          error
}

// fireMetaKey is the context key used to thread per-firing metadata (e.g. the
// actual executed LLM model id) from fire* functions back to the executor.
type fireMetaKey struct{}

// fireMeta carries metadata captured during a single transition firing.
// It is allocated fresh per firing and attached to the ctx passed to dispatch.
// Fields are only written by the firing goroutine, so no locking is needed.
type fireMeta struct {
	executedModel string
}

func metaFromCtx(ctx context.Context) *fireMeta {
	if m, ok := ctx.Value(fireMetaKey{}).(*fireMeta); ok {
		return m
	}
	return nil
}

// Run executes the CPN until completion, deadlock, timeout, or error.
//
// Algorithm:
//  1. Validate topology (Validate from Block 5)
//  2. Set State=Running
//  3. Loop:
//     a. Check context cancellation
//     b. drainObservers — deposit event tokens from sub-CPN buses (Block 13)
//     c. Collect firable transitions (sorted by ID, NodeKindObserver excluded)
//     d. If none firable: check IsComplete -> Completed, else -> Deadlock
//     e. For each firable: re-check CanFire, consume tokens, launch goroutine
//     f. wg.Wait() for all goroutines
//     g. Process errors: ErrorPlace routing or CPN failure
//     h. Repeat
//
// NodeKindTool (Block 6), NodeKindLLM (Block 9), NodeKindValidate (Block 10),
// NodeKindSubNet (Block 12), and NodeKindHITL (Block 14) are dispatched. Other kinds return ErrInvalidNodeKind.
func (c *CPN) Run(ctx context.Context) error {
	// REQ-003: Validate before entering the loop.
	if err := Validate(c.Places, c.Transitions); err != nil {
		c.setFailed(err)
		return err
	}

	// REQ-004: Set running after successful validation.
	c.setState(StateRunning)

	// Block 18: Create execution tracker if metrics are enabled (REQ-013).
	var tracker *executionTracker
	if c.Metrics != nil {
		tracker = newExecutionTracker(c.ID, c.Role, c.Depth, c.SessionID)
	}
	// PAT-003: Defer-based finalization captures metrics on all exit paths (REQ-013, CON-005).
	defer func() {
		if tracker == nil {
			return
		}
		var costUSD float64
		if c.Cost != nil {
			costUSD = c.Cost.SessionCostUSD(c.SessionID)
		}
		rec := tracker.Finalize(c.getState() == StateCompleted, costUSD)
		c.Metrics.Append(&rec)
	}()

	for {
		// REQ-015: Check context cancellation.
		select {
		case <-ctx.Done():
			c.setFailed(ErrTimeout)
			return ErrTimeout
		default:
		}

		// REQ-011 (Block 13): Drain observer events before collecting firable.
		// Observer-deposited tokens may enable downstream transitions.
		drainObservers(ctx, c)

		// REQ-005: Collect firable transitions (exclude Observer).
		firable := c.collectFirable()

		// REQ-014: No firable transitions.
		if len(firable) == 0 {
			if err := c.checkCompletionOrDeadlock(); err == errChildrenCompleted {
				continue // Children deposited tokens — re-evaluate firable transitions.
			} else {
				return err
			}
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
			if !c.effectiveCanFire(t) {
				continue // PAT-002: re-check after prior consumption
			}
			consumed := consumeAll(t.InputPlaces, c.Places)
			firings = append(firings, firing{transition: t, consumed: consumed})
		}

		if len(firings) == 0 {
			// All became unfirable after re-check.
			if err := c.checkCompletionOrDeadlock(); err == errChildrenCompleted {
				continue
			} else {
				return err
			}
		}

		// Emit transition_started events on the main goroutine (CON-001).
		for _, f := range firings {
			snapshots := make([]TokenSnapshot, len(f.consumed))
			for i := range f.consumed {
				snapshots[i] = f.consumed[i].Snapshot()
			}
			c.emit(&Event{
				Type:           EventTransitionStarted,
				TransitionID:   f.transition.ID,
				TransitionKind: f.transition.Kind,
				Payload:        TransitionStartedPayload{InputTokens: snapshots},
			})
		}

		// PAT-003: WaitGroup + error channel.
		var wg sync.WaitGroup
		errCh := make(chan fireResult, len(firings))

		for _, f := range firings {
			wg.Add(1)
			go func(t *Transition, consumed []Token) {
				defer wg.Done()
				start := time.Now()
				meta := &fireMeta{}
				fireCtx := context.WithValue(ctx, fireMetaKey{}, meta)
				// REQ-008: fireWithRetry integrates Block 4 retry.
				outputSnaps, costUSD, err := fireWithRetry(fireCtx, t.Retry, t.CircuitBreaker(), func() ([]TokenSnapshot, float64, error) {
					return dispatch(fireCtx, t, c, consumed)
				})
				elapsed := time.Since(start)

				// Emit transition_completed (CON-002: inside fire goroutine).
				payload := TransitionCompletedPayload{
					OutputTokens:  outputSnaps,
					CostUSD:       costUSD,
					DurationMs:    elapsed.Milliseconds(),
					ExecutedModel: meta.executedModel,
				}
				if err != nil {
					payload.Error = err.Error()
				}
				c.emit(&Event{
					Type:           EventTransitionCompleted,
					TransitionID:   t.ID,
					TransitionKind: t.Kind,
					Payload:        payload,
				})

				if err != nil {
					errCh <- fireResult{transitionID: t.ID, err: err}
				}
			}(f.transition, f.consumed)
		}

		// SEC-002: Wait for all goroutines before processing errors.
		wg.Wait()
		close(errCh)

		// Block 18: Record transition firings for metrics (REQ-014, GUD-004).
		// Recorded after wg.Wait() and BEFORE error processing so that all
		// dispatched transitions are counted regardless of success or failure —
		// the firing happened, the LLM call was made, the cost was incurred.
		if tracker != nil {
			for _, f := range firings {
				tracker.RecordTransitionFired(f.transition.Kind)
				tracker.RecordTokensProduced(len(f.transition.OutputPlaces))
			}
		}

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

		// REQ-012 (Block 16): Evaluate mode transition rules after each firing round.
		// checkModeSwitch runs after wg.Wait() + error processing, ensuring all
		// firings are complete and token deposits are visible.
		c.checkModeSwitch()
	}
}

// checkCompletionOrDeadlock returns nil if the CPN is complete, or ErrDeadlock otherwise.
// If child goroutines are still active (SubNet), waits for them to deposit tokens
// before declaring deadlock — children may produce tokens that unblock the parent.
func (c *CPN) checkCompletionOrDeadlock() error {
	if c.IsComplete() {
		c.setState(StateCompleted)
		return nil
	}

	// If children are still running, wait for them — they may deposit output tokens.
	// Blocks until ALL children finish, even if one already set parent to failed.
	// This ensures no goroutine leaks. Callers who need faster failure
	// propagation should use context cancellation.
	if c.activeChildren.Load() > 0 {
		c.childWg.Wait()
		// Re-check after children have completed and deposited tokens.
		if c.IsComplete() {
			c.setState(StateCompleted)
			return nil
		}
		// Children are done but parent is still not complete.
		// Check if parent was set to failed by a child.
		if c.getState() == StateFailed {
			return c.getError()
		}
		// Children deposited tokens but parent isn't complete yet.
		// Signal the main loop to re-evaluate — child output tokens may
		// enable parent transitions (e.g., a merge after two children).
		return errChildrenCompleted
	}

	c.setFailed(ErrDeadlock)
	return ErrDeadlock
}

// collectFirable returns transitions that can fire, excluding observers.
// Uses effectiveCanFire to apply mode-aware guard injection (Block 16).
func (c *CPN) collectFirable() []*Transition {
	var firable []*Transition
	for _, t := range c.Transitions {
		if t.Kind == NodeKindObserver {
			continue
		}
		if c.effectiveCanFire(t) {
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
// NodeKindTool (Block 6), NodeKindLLM (Block 9), NodeKindValidate (Block 10),
// NodeKindSubNet (Block 12), NodeKindHITL (Block 14), and NodeKindHITL
// with RevisionLoop (Block 15) are implemented.
func dispatch(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	switch t.Kind {
	case NodeKindTool:
		return fireTool(ctx, t, c, consumed)
	case NodeKindLLM:
		return fireLLM(ctx, t, c, consumed)
	case NodeKindValidate:
		return fireValidate(ctx, t, c, consumed)
	case NodeKindSubNet:
		return fireSubNet(ctx, t, c, consumed)
	case NodeKindHITL:
		if t.HITLConfig != nil && t.HITLConfig.RevisionLoop {
			return fireHITLWithRevision(ctx, t, c, consumed)
		}
		return fireHITL(ctx, t, c, consumed)
	case NodeKindObserver:
		return nil, 0, fmt.Errorf("%w: Observer dispatch not implemented", ErrInvalidNodeKind)
	default:
		return nil, 0, fmt.Errorf("%w: unknown kind %q", ErrInvalidNodeKind, t.Kind)
	}
}

// fireTool executes a NodeKindTool transition.
// REQ-016: Calls t.Executor, stamps origin metadata, deposits result in OutputPlaces.
// Returns (0, error) — tool transitions have no LLM cost.
func fireTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if t.ToolHandler != nil {
		return fireToolHandler(ctx, t, c, consumed)
	}

	if t.Executor == nil {
		return nil, 0, fmt.Errorf("transition %s: nil Executor", t.ID)
	}

	if len(consumed) == 0 {
		return nil, 0, fmt.Errorf("transition %s: no consumed tokens", t.ID)
	}

	// Call the tool executor with the first consumed token.
	result, err := t.Executor(ctx, consumed[0])
	if err != nil {
		return nil, 0, err
	}

	// REQ-019: Stamp origin metadata.
	result.OriginID = c.ID
	result.OriginDepth = c.Depth
	result.OriginKind = NodeKindTool
	result.SessionID = c.SessionID
	result.Timestamp = time.Now()

	// FIX-001: Build snapshot BEFORE deposit to avoid Peek race.
	outputSnaps := []TokenSnapshot{result.Snapshot()}

	// Deposit result into all output places.
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := result // copy per output place
		if err := p.Deposit(&tok); err != nil {
			return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return outputSnaps, 0, nil
}

// fireToolHandler is the multi-in/multi-out tool firing path. Used by routing
// + counter-update transitions (e.g. t-followup in the clarification loop)
// that consume more than one input token and deposit DIFFERENT tokens into
// DIFFERENT output places. Stamps origin metadata on every deposited token.
func fireToolHandler(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	results, err := t.ToolHandler(ctx, consumed)
	if err != nil {
		return nil, 0, err
	}

	outputSnaps := make([]TokenSnapshot, 0, len(t.OutputPlaces))
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok, ok := results[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: ToolHandler produced no token for output place %s", t.ID, pid)
		}
		tok.OriginID = c.ID
		tok.OriginDepth = c.Depth
		tok.OriginKind = NodeKindTool
		tok.SessionID = c.SessionID
		tok.Timestamp = time.Now()
		if tok.Space == "" {
			tok.Space = p.Space
		}
		outputSnaps = append(outputSnaps, tok.Snapshot())
		if err := p.Deposit(&tok); err != nil {
			return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}
	return outputSnaps, 0, nil
}
