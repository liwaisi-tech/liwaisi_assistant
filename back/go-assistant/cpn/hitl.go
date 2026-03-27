package cpn

import (
	"context"
	"fmt"
	"time"
)

// HITLConfig configures human-in-the-loop behavior for NodeKindHITL transitions.
// Implements Building Block 7 (Feedback).
type HITLConfig struct {
	// Channel is the channel where the human token is received.
	// Created externally (by Session layer or test). Must not be nil.
	Channel chan Token

	// Prompt is the message shown to the human via the surface stream.
	Prompt string

	// RevisionLoop enables multi-round revision (Block 15).
	// When true, the HITL transition supports Approve/Reject/Revise actions.
	// Ignored in Block 14 (single-turn only).
	RevisionLoop bool

	// CorrectionLLMID is the Transition ID for revision LLM (Block 15).
	// Ignored in Block 14.
	CorrectionLLMID string

	// MaxRevisions caps revision rounds (Block 15). Default: 5.
	// Ignored in Block 14.
	MaxRevisions int
}

// GroupNotifier notifies the parent group of CPN state changes.
// Implemented by GroupAgent (Block 11). Nil means no group coordination.
type GroupNotifier interface {
	SwitchCMP(cpnID string)
}

// fireHITL executes a single-turn HITL transition.
//
// Flow:
//  1. Emit EventHITLRequested with prompt
//  2. Set StateWaiting, notify group
//  3. Block on channel (or ctx.Done for timeout)
//  4. Validate token is ColorHuman
//  5. Check for HITLReject action
//  6. Set StateRunning, notify group, emit EventHITLResolved
//  7. Deposit token into OutputPlaces (space bridging)
//  8. Check Centaurian mode switch
func fireHITL(ctx context.Context, t *Transition, c *CPN, _ []Token) error {
	cfg := t.HITLConfig
	if cfg == nil || cfg.Channel == nil {
		return fmt.Errorf("%w: transition %s", ErrHITLMisconfigured, t.ID)
	}

	// REQ-002: Emit EventHITLRequested with the prompt before blocking.
	c.emit(&Event{
		Type:           EventHITLRequested,
		TransitionID:   t.ID,
		TransitionKind: NodeKindHITL,
		Payload:        cfg.Prompt,
	})

	// REQ-003: Set StateWaiting before blocking on channel.
	c.setState(StateWaiting)

	// REQ-004: Notify group when entering StateWaiting.
	if c.GroupNotifier != nil {
		c.GroupNotifier.SwitchCMP(c.ID)
	}

	// REQ-005: Block on channel or ctx.Done.
	var tok Token
	select {
	case tok = <-cfg.Channel:
	case <-ctx.Done():
		// REQ-011: Context cancellation → StateFailed, ErrTimeout.
		c.setFailed(ErrTimeout)
		return ErrTimeout
	}

	// REQ-006: Received token MUST have Color == ColorHuman.
	if tok.Color != ColorHuman {
		return fmt.Errorf("%w: expected %s, got %s", ErrColorMismatch, ColorHuman, tok.Color)
	}

	// REQ-007: If token payload is HITLResponse{Action: HITLReject}, return ErrHITLRejected.
	if resp, ok := tok.Payload.(HITLResponse); ok && resp.Action == HITLReject {
		return ErrHITLRejected
	}

	// REQ-008: Set StateRunning, emit EventHITLResolved.
	c.setState(StateRunning)

	// REQ-004: Notify group when returning to StateRunning.
	if c.GroupNotifier != nil {
		c.GroupNotifier.SwitchCMP(c.ID)
	}

	c.emit(&Event{
		Type:           EventHITLResolved,
		TransitionID:   t.ID,
		TransitionKind: NodeKindHITL,
		Token:          &tok,
	})

	// REQ-008/REQ-009: Deposit token into all OutputPlaces with space bridging.
	needCentaurian := false
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}

		// GUD-001: Space bridging — HITL is the sanctioned boundary crossing
		// between Surface and Computation. Adjust token Space to match
		// each output place's Space before deposit. This is the ONLY
		// transition type that adjusts token Space.
		out := tok // copy per output place
		out.Space = p.Space
		out.OriginID = c.ID
		out.OriginDepth = c.Depth
		out.OriginKind = NodeKindHITL
		out.SessionID = c.SessionID
		out.Timestamp = time.Now()

		if err := p.Deposit(&out); err != nil {
			return fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}

		// REQ-010: Track if any output place is SpaceComputation.
		if p.Space == SpaceComputation {
			needCentaurian = true
		}
	}

	// REQ-010: Centaurian mode switch when human token enters computation space.
	if needCentaurian {
		c.switchModeToCentaurian()
	}

	return nil
}
