package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// SessionHITLHandler adapts a PolicyHostGate to the cpn.HostHITLHandler
// seam consumed by fire_bash. It inspects the gate error for the
// require-HITL sentinel (CON-003) and, when present, builds a
// SessionHITLRouter bound to the firing transition's HITL channel then
// defers to PolicyHostGate.HandleRequiresHITL. Non-HITL errors are
// returned unchanged so the executor's ErrorPlace pipeline keeps its
// current semantics.
//
// SessionHITLHandler is safe for concurrent use: it holds no per-call
// state and the router it constructs is stack-local.
type SessionHITLHandler struct {
	Gate *PolicyHostGate
}

// HandleHITL implements cpn.HostHITLHandler.
func (h *SessionHITLHandler) HandleHITL(ctx context.Context, t *cpn.Transition, c *cpn.CPN, op cpn.GateOp, hitlErr error) error {
	if hitlErr == nil {
		return nil
	}
	dec, ok := IsRequiresHITL(hitlErr)
	if !ok {
		// Non-HITL error — preserve pre-existing behaviour.
		return hitlErr
	}
	if h == nil || h.Gate == nil {
		return hitlErr
	}

	// Reuse the HITL channel already registered for the transition. This
	// is the single inject channel wired by SessionService via
	// RegisterToolHITL — we do NOT introduce a parallel HITL bus.
	var inject <-chan cpn.Token
	if t != nil && t.HITLConfig != nil && t.HITLConfig.Channel != nil {
		inject = t.HITLConfig.Channel
	}

	router := &SessionHITLRouter{
		CPN:          c,
		TransitionID: transitionIDOrEmpty(t),
		Inject:       inject,
	}
	return h.Gate.HandleRequiresHITL(ctx, dec, op, router)
}

func transitionIDOrEmpty(t *cpn.Transition) string {
	if t == nil {
		return ""
	}
	return t.ID
}

// SessionHITLRouter adapts the CPN's existing HITL channels to the gate's
// HITLRouter interface. It publishes HostApprovalPrompt as an A2UI
// host.approval surface event and blocks on the session's hitl-inject
// channel until the user responds. The extended action from the HITL
// response's Content field ({"action":"<extended>"}) is decoded into a
// HostApprovalResponse.
//
// This type MUST reuse the HITL channel wired by SessionService
// (session.hitlInject[transitionID] via RegisterToolHITL) — it does not
// introduce a parallel HITL bus. The router is a pure adapter on top of
// the same channel used by the LLM-level tool HITL flow.
//
// Zero-value SessionHITLRouter is not usable: CPN, TransitionID and Inject
// must all be populated.
type SessionHITLRouter struct {
	// CPN is the engine used to publish the approval surface via the
	// event bus. MUST be non-nil.
	CPN *cpn.CPN

	// TransitionID is the id of the transition that raised the HITL
	// requirement. Used to address the event and match POST /hitl/{id}.
	TransitionID string

	// Inject is the read end of the HITL channel registered for the
	// transition on the enclosing Session. A nil channel immediately
	// collapses AwaitResponse to a ctx-wait and, on ctx cancellation,
	// returns ctx.Err().
	Inject <-chan cpn.Token

	// Logger, when non-nil, receives structured diagnostics for the
	// router's decoding and cancellation branches. Defaults to slog.Default()
	// when left nil so production always gets log coverage.
	Logger *slog.Logger
}

// Compile-time interface check.
var _ HITLRouter = (*SessionHITLRouter)(nil)

// Publish emits the host.approval A2UI surface as a StreamChunk and the
// EventHITLRequested typed envelope so the frontend's HostApprovalCard
// renders the extended-action buttons. Mirrors the tool-HITL publication
// path in fire_llm.go so the two flows converge on a single card shape
// (PAT-001).
func (r *SessionHITLRouter) Publish(_ context.Context, prompt HostApprovalPrompt) error {
	if r == nil || r.CPN == nil {
		return fmt.Errorf("gate: session router missing CPN")
	}
	if r.TransitionID == "" {
		return fmt.Errorf("gate: session router missing TransitionID")
	}

	// Ensure the schema discriminator is stamped even when upstream
	// Decision builders leave it blank.
	if prompt.Schema == "" {
		prompt.Schema = HostApprovalSchema
	}

	// Build the A2UI envelope consumed by the frontend HostApprovalCard.
	// The payload carries the pre-parsed command line, the risk band and
	// a RememberAvailable hint so the extended-action buttons render.
	payload := cpn.HostApprovalPayload{
		Command: prompt.Command,
		Invocation: cpn.HostApprovalInvocation{
			Tool: prompt.Operation,
			Args: json.RawMessage("{}"),
		},
		Risk:              string(prompt.RiskBand),
		Title:             "Permiso para operar en tu máquina",
		Context:           prompt.Rationale,
		RememberAvailable: true,
	}
	envelope := struct {
		Schema       string                  `json:"schema"`
		HostApproval cpn.HostApprovalPayload `json:"hostApproval"`
	}{
		Schema:       HostApprovalSchema,
		HostApproval: payload,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("gate: marshal host.approval envelope: %w", err)
	}
	content := cpn.A2UIMarker + string(raw)

	// Emit the A2UI stream chunk so the frontend renders the card.
	r.CPN.PublishHostApprovalSurface(r.TransitionID, content)

	// Emit the typed HITLRequested payload with CustomSurface=true so the
	// integration layer suppresses any legacy review-card fallback
	// (spec-process-bugfix-tool-hitl-single-gate.md REQ-001/002).
	r.CPN.PublishHITLRequested(r.TransitionID, cpn.HITLRequestedPayload{
		Prompt:        "Permiso para operar en tu máquina",
		CustomSurface: true,
	})
	return nil
}

// AwaitResponse blocks until the registered HITL channel delivers a token
// or ctx is cancelled. The token's payload is expected to be a
// cpn.HITLResponse; the extended action literal is decoded from its
// Content field via JSON. Malformed or empty content collapses the
// extended action to the empty string, which HandleRequiresHITL treats as
// deny (GUD-001).
func (r *SessionHITLRouter) AwaitResponse(ctx context.Context) (HostApprovalResponse, error) {
	if r == nil || r.Inject == nil {
		// No channel registered — wait for ctx cancellation rather than
		// returning a synchronous error so callers get consistent deny
		// semantics via ctx.Err().
		<-ctx.Done()
		return HostApprovalResponse{}, ctx.Err()
	}

	select {
	case <-ctx.Done():
		return HostApprovalResponse{}, ctx.Err()
	case tok, ok := <-r.Inject:
		if !ok {
			return HostApprovalResponse{}, errors.New("gate: HITL channel closed")
		}
		return r.decode(tok), nil
	}
}

// decode extracts the extended-action literal from the token's payload.
// When the payload is a cpn.HITLResponse the Content field carries the
// frontend-encoded {"action":"<extended>"} object. Any other shape is
// treated as an empty action so HandleRequiresHITL collapses to deny.
func (r *SessionHITLRouter) decode(tok cpn.Token) HostApprovalResponse {
	out := HostApprovalResponse{RespondedAt: time.Now()}
	resp, ok := tok.Payload.(cpn.HITLResponse)
	if !ok {
		r.logger().Warn("gate: HITL token payload is not HITLResponse; denying",
			"transition_id", r.TransitionID,
			"payload_type", fmt.Sprintf("%T", tok.Payload),
		)
		return out
	}

	// Narrow-action-only ("approve" / "reject") with empty content is a
	// valid legacy shape — map it to the canonical extended literals.
	content := resp.Content
	if content == "" {
		switch resp.Action {
		case cpn.HITLApprove:
			out.Action = ActionApproveOnce
		case cpn.HITLReject:
			out.Action = ActionDeny
		}
		return out
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(content), &body); err != nil {
		r.logger().Warn("gate: malformed HITL content; denying",
			"transition_id", r.TransitionID,
			"error", err.Error(),
		)
		// Fall through to narrow-action mapping when content is garbage.
		switch resp.Action {
		case cpn.HITLApprove:
			out.Action = ActionApproveOnce
		case cpn.HITLReject:
			out.Action = ActionDeny
		}
		return out
	}

	out.Action = body.Action
	return out
}

func (r *SessionHITLRouter) logger() *slog.Logger {
	if r != nil && r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
