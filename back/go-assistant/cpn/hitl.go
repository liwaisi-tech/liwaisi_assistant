package cpn

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DefaultMaxRevisions is the recommended cap for revision loop rounds.
const DefaultMaxRevisions = 5

// A2UIMarker is the protocol prefix that precedes a JSON A2UI surface
// payload in a StreamChunk.Content. The frontend detects this prefix and
// routes the chunk to the A2UI renderer instead of the markdown pipeline.
// REQ-005 / spec-process-bugfix-a2ui-hitl-rehydration.md — the literal
// MUST appear exactly once in the cpn package; every emission site and
// every test assertion references this constant.
const A2UIMarker = "$$a2ui:"

// HITLRequestedPayload is the payload carried by EventHITLRequested when the
// transition publishes its own A2UI surface (e.g. a questionnaire) via
// HITLConfig.A2UIPayloadBuilder. The CustomSurface flag signals to the
// integration layer that a default review card MUST NOT be emitted, since
// the custom surface is already on the wire.
//
// When A2UIPayloadBuilder is not configured, fireHITL keeps emitting the
// prompt as a bare string for backward compatibility with existing consumers
// (legacy review card emission, a2a mapper).
type HITLRequestedPayload struct {
	Prompt        string `json:"prompt"`
	CustomSurface bool   `json:"custom_surface"`
}

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

	// A2UIPayloadBuilder, when non-nil, is invoked by fireHITL just before
	// emitting EventHITLRequested. Its return value is JSON-encoded and
	// published as a StreamChunk with body "$$a2ui:"+json via the same
	// EventStreamChunk path used by streaming LLM transitions. The frontend
	// detects the prefix and renders an interactive surface (e.g. a
	// questionnaire). A nil return or error is non-fatal — the HITL request
	// still proceeds without an A2UI surface.
	A2UIPayloadBuilder func(consumed []Token) (any, error)

	// OutputBuilder, when non-nil, lets a HITL transition synthesize a
	// typed output token from (consumed, response) instead of depositing
	// the raw human token. Used for structured HITL flows like t-clarify
	// where answers must be merged into an upstream JSON payload before
	// flowing downstream. The returned token's Color must match every
	// output place's Color. Only consulted on HITLApprove or HITLSubmit;
	// HITLReject still short-circuits with ErrHITLRejected.
	OutputBuilder func(consumed []Token, resp HITLResponse) (Token, error)
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
func fireHITL(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	cfg := t.HITLConfig
	if cfg == nil || cfg.Channel == nil {
		return nil, 0, fmt.Errorf("%w: transition %s", ErrHITLMisconfigured, t.ID)
	}

	// Optional A2UI surface emission. When configured, the transition
	// publishes an interactive component spec via the standard streaming
	// channel BEFORE blocking on human input. The frontend matches the
	// "$$a2ui:" prefix and renders an interactive surface that ultimately
	// resolves the HITL request via the regular HTTP endpoint.
	customSurface := false
	var a2uiRowID string
	if cfg.A2UIPayloadBuilder != nil {
		if payload, err := cfg.A2UIPayloadBuilder(consumed); err == nil && payload != nil {
			if encoded, mErr := json.Marshal(payload); mErr == nil {
				content := A2UIMarker + string(encoded)
				c.StreamedOutput = true
				c.emit(&Event{
					Type:           EventStreamChunk,
					TransitionID:   t.ID,
					TransitionKind: NodeKindHITL,
					Payload: StreamChunk{
						SessionID: c.SessionID,
						CPNID:     c.ID,
						CPNRole:   c.Role,
						Content:   content,
						Done:      false,
					},
				})
				// Finalize the streaming assistant message so the next
				// StreamChunk (from any source) starts a fresh bubble
				// instead of being concatenated onto this one. Mirrors
				// the sentinel pattern used by the legacy main.go HITL
				// card emission path.
				c.emit(&Event{
					Type:           EventStreamChunk,
					TransitionID:   t.ID,
					TransitionKind: NodeKindHITL,
					Payload: StreamChunk{
						SessionID: c.SessionID,
						CPNID:     c.ID,
						CPNRole:   c.Role,
						Content:   "",
						Done:      true,
					},
				})

				// REQ-001 / REQ-002 / INV-002: persist the surface via
				// c.History so the session-service history-sync path
				// (internal/app/session_service.go lines ~293-307)
				// promotes it into session.Messages and persistAfterRun
				// writes it verbatim to the messages table. Append only
				// on this success branch — failures above MUST NOT append
				// (AC-012). fireHITL is invoked from the executor
				// goroutine without holding c.mu (see executor.go
				// dispatch), mirroring fire_llm.go which takes the lock
				// around its own History append.
				// The row gets a deterministic ID so the subsequent
				// HITL response row (appended after resolution) can
				// reference it via ParentMessageID (REQ-006/007).
				a2uiRowID = fmt.Sprintf("%s-a2ui-%d", c.SessionID, time.Now().UnixNano())
				c.mu.Lock()
				c.History = append(c.History, &Message{
					ID:        a2uiRowID,
					Role:      RoleAssistant,
					Content:   content,
					CPNID:     c.ID,
					CPNRole:   c.Role,
					CPNDepth:  c.Depth,
					Timestamp: time.Now(),
				})
				c.mu.Unlock()

				customSurface = true
			}
		}
	}

	// REQ-002: Emit EventHITLRequested with the prompt before blocking.
	// Note: consumed token content is NOT included in the payload because
	// upstream LLM transitions with StreamOutput=true already stream
	// their output to the frontend. Including it would cause duplication.
	//
	// When a custom A2UI surface was just published, emit a structured
	// HITLRequestedPayload so downstream consumers (legacy review-card
	// emitter, frontend useChat) can suppress their default rendering
	// and avoid duplicating the surface. Otherwise emit the bare string
	// prompt to preserve backward compatibility.
	var hitlPayload any = cfg.Prompt
	if customSurface {
		hitlPayload = HITLRequestedPayload{Prompt: cfg.Prompt, CustomSurface: true}
	}
	c.emit(&Event{
		Type:           EventHITLRequested,
		TransitionID:   t.ID,
		TransitionKind: NodeKindHITL,
		Payload:        hitlPayload,
	})

	// REQ-003: Set StateWaiting before blocking on channel.
	c.setState(StateWaiting)

	// REQ-004: Notify group when entering StateWaiting.
	if c.GroupNotifier != nil {
		c.GroupNotifier.SwitchCMP(c.ID)
	}

	// REQ-005: Block on channel or ctx.Done.
	var tok Token
	var chanOK bool
	select {
	case tok, chanOK = <-cfg.Channel:
		// FIX-003: Protect against closed channel.
		if !chanOK {
			c.setFailed(fmt.Errorf("HITL channel closed"))
			return nil, 0, fmt.Errorf("transition %s: HITL channel closed unexpectedly", t.ID)
		}
	case <-ctx.Done():
		// REQ-011: Context cancellation → StateFailed, ErrTimeout.
		c.setFailed(ErrTimeout)
		return nil, 0, ErrTimeout
	}

	// REQ-006: Received token MUST have Color == ColorHuman.
	if tok.Color != ColorHuman {
		return nil, 0, fmt.Errorf("%w: expected %s, got %s", ErrColorMismatch, ColorHuman, tok.Color)
	}

	// REQ-007: If token payload is HITLResponse{Action: HITLReject}, return ErrHITLRejected.
	if resp, ok := tok.Payload.(HITLResponse); ok && resp.Action == HITLReject {
		return nil, 0, ErrHITLRejected
	}

	// Structured-output path: when OutputBuilder is configured, the transition
	// synthesizes a typed output token from (consumed, response) instead of
	// depositing the raw human token. This is how t-clarify merges
	// questionnaire answers into the upstream classifier JSON before flowing
	// to t-plan-clarified.
	if cfg.OutputBuilder != nil {
		resp, ok := tok.Payload.(HITLResponse)
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: OutputBuilder requires HITLResponse payload, got %T", t.ID, tok.Payload)
		}
		built, err := cfg.OutputBuilder(consumed, resp)
		if err != nil {
			return nil, 0, fmt.Errorf("transition %s: OutputBuilder: %w", t.ID, err)
		}

		c.setState(StateRunning)
		if c.GroupNotifier != nil {
			c.GroupNotifier.SwitchCMP(c.ID)
		}
		c.emit(&Event{
			Type:           EventHITLResolved,
			TransitionID:   t.ID,
			TransitionKind: NodeKindHITL,
			Token:          &tok,
		})

		built.OriginID = c.ID
		built.OriginDepth = c.Depth
		built.OriginKind = NodeKindHITL
		built.SessionID = c.SessionID
		built.Timestamp = time.Now()
		outputSnaps := []TokenSnapshot{built.Snapshot()}

		// REQ-001/002/006: persist the user's response as a RoleUser row
		// linked to the A2UI surface so history rehydration can pair them
		// and lock the questionnaire component. Skip when no A2UI surface
		// was emitted — there's no parent to pair against.
		if a2uiRowID != "" {
			appendHITLResponseToHistory(c, resp, a2uiRowID)
		}

		needCentaurian := false
		for _, pid := range t.OutputPlaces {
			p, ok := c.Places[pid]
			if !ok {
				return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
			}
			deposit := built
			deposit.Space = p.Space
			deposit.OriginID = c.ID
			deposit.OriginDepth = c.Depth
			deposit.OriginKind = NodeKindHITL
			deposit.SessionID = c.SessionID
			deposit.Timestamp = time.Now()
			if err := p.Deposit(&deposit); err != nil {
				return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
			}
			if p.Space == SpaceComputation {
				needCentaurian = true
			}
		}
		if needCentaurian {
			c.switchModeToCentaurian()
		}
		return outputSnaps, 0, nil
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
	// FIX-001: Build snapshot BEFORE deposit to avoid Peek race.
	out := tok // template for snapshot
	out.OriginID = c.ID
	out.OriginDepth = c.Depth
	out.OriginKind = NodeKindHITL
	out.SessionID = c.SessionID
	out.Timestamp = time.Now()
	outputSnaps := []TokenSnapshot{out.Snapshot()}

	// REQ-001/002/006: raw-deposit path also appends the response row so
	// t-review-style HITL transitions (approve/revise/submit on surface
	// tokens) keep the questionnaire-lock invariant. Skip when no A2UI
	// surface was emitted — there's no parent to pair against.
	if a2uiRowID != "" {
		if resp, ok := tok.Payload.(HITLResponse); ok {
			appendHITLResponseToHistory(c, resp, a2uiRowID)
		}
	}

	needCentaurian := false
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}

		// GUD-001: Space bridging — HITL is the sanctioned boundary crossing
		// between Surface and Computation. Adjust token Space to match
		// each output place's Space before deposit. This is the ONLY
		// transition type that adjusts token Space.
		deposit := tok // copy per output place
		deposit.Space = p.Space
		deposit.OriginID = c.ID
		deposit.OriginDepth = c.Depth
		deposit.OriginKind = NodeKindHITL
		deposit.SessionID = c.SessionID
		deposit.Timestamp = time.Now()

		if err := p.Deposit(&deposit); err != nil {
			return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
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

	return outputSnaps, 0, nil
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
func fireHITLWithRevision(ctx context.Context, t *Transition, c *CPN, _ []Token) ([]TokenSnapshot, float64, error) {
	cfg := t.HITLConfig
	if cfg == nil || cfg.Channel == nil {
		return nil, 0, fmt.Errorf("%w: transition %s", ErrHITLMisconfigured, t.ID)
	}

	prompt := cfg.Prompt
	rounds := 0
	var totalRevisionCost float64

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
		var chanOK bool
		select {
		case tok, chanOK = <-cfg.Channel:
			// FIX-003: Protect against closed channel.
			if !chanOK {
				c.setFailed(fmt.Errorf("HITL channel closed"))
				return nil, totalRevisionCost, fmt.Errorf("transition %s: HITL channel closed unexpectedly", t.ID)
			}
		case <-ctx.Done():
			c.setFailed(ErrTimeout)
			return nil, totalRevisionCost, ErrTimeout
		}

		// Validate color.
		if tok.Color != ColorHuman {
			return nil, totalRevisionCost, fmt.Errorf("%w: expected %s, got %s", ErrColorMismatch, ColorHuman, tok.Color)
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
			return nil, totalRevisionCost, fmt.Errorf("transition %s: unsupported HITL payload type %T", t.ID, tok.Payload)
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
			snaps, depErr := depositHITLRevision(t, c, &tok)
			return snaps, totalRevisionCost, depErr

		case HITLReject:
			// REQ-003: Reject returns error.
			return nil, totalRevisionCost, ErrHITLRejected

		case HITLRevise:
			// REQ-005: Check MaxRevisions cap.
			if rounds >= cfg.MaxRevisions {
				return nil, totalRevisionCost, ErrHITLMaxRevisions
			}

			// REQ-011: Validate correction transition exists.
			corrTransition, ok := c.Transitions[cfg.CorrectionLLMID]
			if !ok {
				return nil, totalRevisionCost, fmt.Errorf("transition %s: correction LLM transition %s not found", t.ID, cfg.CorrectionLLMID)
			}

			// REQ-007: Call correction LLM via fireLLMDirect.
			// FIX-006: Propagate revision LLM cost.
			feedbackToken := Token{
				Color:   ColorString,
				Payload: resp.Content,
			}
			revised, revCost, err := fireLLMDirect(ctx, corrTransition, c, &feedbackToken)
			totalRevisionCost += revCost
			if err != nil {
				return nil, totalRevisionCost, fmt.Errorf("transition %s: revision LLM (round %d): %w", t.ID, rounds, err)
			}

			// Update prompt with revised text.
			prompt = formatRevisionPrompt(revised)
			rounds++
			continue

		default:
			return nil, totalRevisionCost, fmt.Errorf("transition %s: unknown HITL action %q", t.ID, resp.Action)
		}
	}
}

// depositHITLRevision deposits the approved token into all output places
// with space bridging, following the same pattern as fireHITL.
// Returns output snapshots built before deposit (FIX-001).
func depositHITLRevision(t *Transition, c *CPN, tok *Token) ([]TokenSnapshot, error) {
	// FIX-001: Build snapshot BEFORE deposit to avoid Peek race.
	out := *tok
	out.OriginID = c.ID
	out.OriginDepth = c.Depth
	out.OriginKind = NodeKindHITL
	out.SessionID = c.SessionID
	out.Timestamp = time.Now()
	outputSnaps := []TokenSnapshot{out.Snapshot()}

	needCentaurian := false
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}

		deposit := *tok
		deposit.Space = p.Space
		deposit.OriginID = c.ID
		deposit.OriginDepth = c.Depth
		deposit.OriginKind = NodeKindHITL
		deposit.SessionID = c.SessionID
		deposit.Timestamp = time.Now()

		if err := p.Deposit(&deposit); err != nil {
			return nil, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}

		if p.Space == SpaceComputation {
			needCentaurian = true
		}
	}

	if needCentaurian {
		c.switchModeToCentaurian()
	}

	return outputSnaps, nil
}

// fireLLMDirect makes an inline LLM call for the revision correction loop.
// Uses the correction transition's LLMConfig for model selection.
// Returns the LLM's text response content.
// Does NOT consume from or deposit to places — this is an inline sub-call.
func fireLLMDirect(ctx context.Context, corrTransition *Transition, c *CPN, input *Token) (content string, costUSD float64, err error) {
	if c.LLMClient == nil {
		return "", 0, fmt.Errorf("nil LLMClient on CPN")
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
		return "", 0, err
	}

	// FIX-006: Return cost for proper cost propagation.
	return resp.Content, resp.CostUSD, nil
}

// formatRevisionPrompt creates a human-facing prompt for the next revision round.
func formatRevisionPrompt(revised string) string {
	return fmt.Sprintf("The following draft has been revised. Please review and approve, reject, or request further revisions:\n\n---\n%s\n---", revised)
}

// appendHITLResponseToHistory appends a RoleUser row to c.History capturing
// the human's response to a HITL transition so session rehydration can show
// the answers and lock the A2UI surface. The row's ParentMessageID links it
// back to the A2UI surface row created when the transition was requested.
//
// Content encoding (REQ-001/002):
//   - HITLSubmit: raw resp.Content (byte-identical — typically questionnaire
//     answers JSON). Preserves original payload so rehydration can parse it
//     without re-wrapping.
//   - HITLRevise: canonical JSON {"action":"revise","content":"<feedback>"}.
//   - HITLApprove: canonical JSON {"action":"approve"} or
//     {"action":"approve","content":"<content>"} when non-empty.
//   - HITLReject: not persisted (REQ-003 — caller short-circuits before
//     reaching this helper anyway).
//
// See spec-process-bugfix-a2ui-hitl-response-persistence.md REQ-001..007.
func appendHITLResponseToHistory(c *CPN, resp HITLResponse, parentID string) {
	var content string
	switch resp.Action {
	case HITLSubmit:
		content = resp.Content
	case HITLRevise:
		b, err := json.Marshal(struct {
			Action  string `json:"action"`
			Content string `json:"content"`
		}{Action: string(resp.Action), Content: resp.Content})
		if err != nil {
			return
		}
		content = string(b)
	case HITLApprove:
		var b []byte
		var err error
		if resp.Content == "" {
			b, err = json.Marshal(struct {
				Action string `json:"action"`
			}{Action: string(resp.Action)})
		} else {
			b, err = json.Marshal(struct {
				Action  string `json:"action"`
				Content string `json:"content"`
			}{Action: string(resp.Action), Content: resp.Content})
		}
		if err != nil {
			return
		}
		content = string(b)
	default:
		return
	}

	msg := &Message{
		ID:              fmt.Sprintf("%s-hitlresp-%d", c.SessionID, time.Now().UnixNano()),
		Role:            RoleUser,
		Content:         content,
		CPNID:           c.ID,
		CPNRole:         c.Role,
		CPNDepth:        c.Depth,
		Timestamp:       time.Now(),
		ParentMessageID: parentID,
	}
	c.mu.Lock()
	c.History = append(c.History, msg)
	c.mu.Unlock()
}
