package cpn

import (
	"context"
	"fmt"
	"time"
)

// DefaultMaxRevisions is the recommended cap for revision loop rounds.
const DefaultMaxRevisions = 5

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

// fireHITLWithRevision executes a multi-round HITL revision loop.
//
// Each round:
//  1. Emit EventHITLRequested with current prompt and round number
//  2. Set StateWaiting, notify group
//  3. Block on channel
//  4. Parse HITLResponse action
//  5. Approve -> deposit + exit | Reject -> error | Revise -> LLM correction -> loop
//
// Bounded by MaxRevisions. Context cancellation unblocks at every round.
func fireHITLWithRevision(ctx context.Context, t *Transition, c *CPN, _ []Token) error {
	cfg := t.HITLConfig
	if cfg == nil || cfg.Channel == nil {
		return fmt.Errorf("%w: transition %s", ErrHITLMisconfigured, t.ID)
	}

	prompt := cfg.Prompt
	rounds := 0

	for {
		// REQ-014: Set StateWaiting before blocking.
		c.setState(StateWaiting)

		// REQ-006: Emit EventHITLRequested with round number.
		c.emit(&Event{
			Type:           EventHITLRequested,
			TransitionID:   t.ID,
			TransitionKind: NodeKindHITL,
			Payload: map[string]any{
				"prompt": prompt,
				"round":  rounds,
			},
		})

		// REQ-015: Notify group on state change.
		if c.GroupNotifier != nil {
			c.GroupNotifier.SwitchCMP(c.ID)
		}

		// REQ-012: Block on channel or context cancellation.
		var tok Token
		select {
		case tok = <-cfg.Channel:
		case <-ctx.Done():
			c.setFailed(ErrTimeout)
			return ErrTimeout
		}

		// Validate color.
		if tok.Color != ColorHuman {
			return fmt.Errorf("%w: expected %s, got %s", ErrColorMismatch, ColorHuman, tok.Color)
		}

		// REQ-014: Set StateRunning after receiving.
		c.setState(StateRunning)

		// REQ-015: Notify group on state change.
		if c.GroupNotifier != nil {
			c.GroupNotifier.SwitchCMP(c.ID)
		}

		// REQ-009: Defensive payload type handling.
		var resp HITLResponse
		switch v := tok.Payload.(type) {
		case HITLResponse:
			resp = v
		case string:
			// GUD-004: Bare string treated as approve for backward compat.
			resp = HITLResponse{Action: HITLApprove, Content: v}
		default:
			return fmt.Errorf("transition %s: unsupported HITL payload type %T", t.ID, tok.Payload)
		}

		// PAT-002: Action dispatch.
		switch resp.Action {
		case HITLApprove:
			// REQ-002: Deposit and emit resolved.
			c.emit(&Event{
				Type:           EventHITLResolved,
				TransitionID:   t.ID,
				TransitionKind: NodeKindHITL,
				Token:          &tok,
			})
			return depositHITLRevision(t, c, &tok)

		case HITLReject:
			// REQ-003: Reject returns error.
			return ErrHITLRejected

		case HITLRevise:
			// REQ-005: Check MaxRevisions cap.
			if rounds >= cfg.MaxRevisions {
				return ErrHITLMaxRevisions
			}

			// REQ-011: Validate correction transition exists.
			corrTransition, ok := c.Transitions[cfg.CorrectionLLMID]
			if !ok {
				return fmt.Errorf("transition %s: correction LLM transition %s not found", t.ID, cfg.CorrectionLLMID)
			}

			// REQ-007: Call correction LLM via fireLLMDirect.
			feedbackToken := Token{
				Color:   ColorString,
				Payload: resp.Content,
			}
			revised, err := fireLLMDirect(ctx, corrTransition, c, &feedbackToken)
			if err != nil {
				return fmt.Errorf("transition %s: revision LLM (round %d): %w", t.ID, rounds, err)
			}

			// Update prompt with revised text.
			prompt = formatRevisionPrompt(revised)
			rounds++
			continue

		default:
			return fmt.Errorf("transition %s: unknown HITL action %q", t.ID, resp.Action)
		}
	}
}

// depositHITLRevision deposits the approved token into all output places
// with space bridging, following the same pattern as fireHITL.
func depositHITLRevision(t *Transition, c *CPN, tok *Token) error {
	needCentaurian := false
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}

		out := *tok
		out.Space = p.Space
		out.OriginID = c.ID
		out.OriginDepth = c.Depth
		out.OriginKind = NodeKindHITL
		out.SessionID = c.SessionID
		out.Timestamp = time.Now()

		if err := p.Deposit(&out); err != nil {
			return fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}

		if p.Space == SpaceComputation {
			needCentaurian = true
		}
	}

	if needCentaurian {
		c.switchModeToCentaurian()
	}

	return nil
}

// fireLLMDirect makes an inline LLM call for the revision correction loop.
// Uses the correction transition's LLMConfig for model selection.
// Returns the LLM's text response content.
// Does NOT consume from or deposit to places — this is an inline sub-call.
func fireLLMDirect(ctx context.Context, corrTransition *Transition, c *CPN, input *Token) (string, error) {
	if c.LLMClient == nil {
		return "", fmt.Errorf("nil LLMClient on CPN")
	}

	// GUD-001: Build minimal LLMRequest.
	systemPrompt := corrTransition.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a revision assistant. Revise the content based on the feedback provided."
	}

	var model string
	var maxTokens int
	if corrTransition.LLMConfig != nil {
		model = corrTransition.LLMConfig.Model
		maxTokens = corrTransition.LLMConfig.MaxTokens
	}
	if model == "" {
		model = "structured"
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	messages := []*LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("%v", input.Payload)},
	}

	req := &LLMRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: 0,
		SessionID:   c.SessionID,
	}

	if corrTransition.LLMConfig != nil && corrTransition.LLMConfig.RequireJSON {
		req.ResponseFmt = "json_object"
	}

	resp, err := c.LLMClient.Complete(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// formatRevisionPrompt creates a human-facing prompt for the next revision round.
func formatRevisionPrompt(revised string) string {
	return fmt.Sprintf("The following draft has been revised. Please review and approve, reject, or request further revisions:\n\n---\n%s\n---", revised)
}
