package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// HITLRouter is the minimal surface the HandleRequiresHITL helper needs
// to route a host-approval prompt through a CPN's existing HITL plumbing.
// The CPN exposes such a channel via its SessionService wiring. Consumers
// that don't have a full CPN at hand (tests) implement this interface
// directly.
type HITLRouter interface {
	// Publish pushes the A2UI surface + EventHITLRequested onto the CPN's
	// event bus. Equivalent to fireHITL's surface emit block.
	Publish(ctx context.Context, prompt HostApprovalPrompt) error

	// AwaitResponse blocks until the user responds. The returned
	// HostApprovalResponse carries the user's action; an error is
	// returned when ctx is cancelled or the channel closes.
	AwaitResponse(ctx context.Context) (HostApprovalResponse, error)
}

// HostApprovalResponse carries the user's verdict on a host.approval
// HITL prompt. Actions come from a fixed set (spec §3 REQ-042).
type HostApprovalResponse struct {
	Action           string `json:"action"`     // approve-once | approve-and-remember | deny | deny-and-blacklist
	AllowPattern     string `json:"allow_pattern,omitempty"`
	DenyPattern      string `json:"deny_pattern,omitempty"`
	RespondedAt      time.Time
	HITLResponseID   string
}

// Host-approval action literals.
const (
	ActionApproveOnce         = "approve-once"
	ActionApproveAndRemember  = "approve-and-remember"
	ActionDeny                = "deny"
	ActionDenyAndBlacklist    = "deny-and-blacklist"
)

// HandleRequiresHITL is the one-stop helper called by adapter layers
// when PolicyHostGate.Check returns *ErrRequiresHITL. It publishes the
// prompt via router, awaits the user's response, updates the policy
// (for approve-and-remember), and returns a terminal error: nil on
// approval, ErrGateDenied on denial.
func (g *PolicyHostGate) HandleRequiresHITL(ctx context.Context, dec Decision, op cpn.GateOp, router HITLRouter) error {
	if router == nil {
		// No plumbing configured — HITL requirement collapses to deny to
		// honour GUD-001 (when in doubt, deny).
		_ = g.AuditDecision(ctx, Decision{
			Verdict:   VerdictDeny,
			RiskBand:  dec.RiskBand,
			Reason:    "hitl_router_missing",
			Sandbox:   dec.Sandbox,
			SessionID: dec.SessionID,
		}, op, "")
		return cpn.NewHostError(cpn.HostErrCodeGateDenied, "HITL router not wired", nil)
	}

	prompt := HostApprovalPrompt{}
	if dec.Prompt != nil {
		prompt = *dec.Prompt
	}

	if err := router.Publish(ctx, prompt); err != nil {
		return fmt.Errorf("gate: publish HITL: %w", err)
	}

	resp, err := router.AwaitResponse(ctx)
	if err != nil {
		return fmt.Errorf("gate: await HITL: %w", err)
	}

	switch resp.Action {
	case ActionApproveOnce:
		return g.recordHITLOutcome(ctx, dec, op, VerdictAllow, "hitl_approve_once", resp)
	case ActionApproveAndRemember:
		if err := g.rememberApproval(ctx, op); err != nil {
			g.Logger.Warn("gate: remember approval failed", "error", err, "cmd", op.Command)
		}
		return g.recordHITLOutcome(ctx, dec, op, VerdictAllow, "hitl_approve_and_remember", resp)
	case ActionDeny, "":
		return g.recordHITLOutcome(ctx, dec, op, VerdictDeny, "hitl_deny", resp)
	case ActionDenyAndBlacklist:
		if err := g.blacklist(op); err != nil {
			g.Logger.Warn("gate: blacklist failed", "error", err, "cmd", op.Command)
		}
		return g.recordHITLOutcome(ctx, dec, op, VerdictDeny, "hitl_deny_and_blacklist", resp)
	default:
		return g.recordHITLOutcome(ctx, dec, op, VerdictDeny, "hitl_unknown_action", resp)
	}
}

func (g *PolicyHostGate) recordHITLOutcome(ctx context.Context, dec Decision, op cpn.GateOp, verdict Verdict, reason string, resp HostApprovalResponse) error {
	final := dec
	final.Verdict = verdict
	final.Reason = reason
	_ = g.AuditDecision(ctx, final, op, resp.HITLResponseID)
	if verdict == VerdictAllow {
		// Also approve the first-run ledger row if this was a first-run
		// prompt, so subsequent calls short-circuit past the ledger gate.
		if dec.FirstRun && g.FirstRun != nil {
			// Best effort — we no longer have the sum here; recompute.
			if sum, _, err := g.firstRunLookup(ctx, op.Command); err == nil {
				_ = g.FirstRun.Approve(ctx, g.HostID, sum, resp.HITLResponseID)
			}
		}
		return nil
	}
	return cpn.NewHostError(cpn.HostErrCodeGateDenied, "HITL denied operation", nil)
}

// rememberApproval appends a safe-pattern entry for this exact command
// line (escaped as a literal prefix). The policy holder handles the
// thread-safe append + disk overlay.
func (g *PolicyHostGate) rememberApproval(_ context.Context, op cpn.GateOp) error {
	if g.Policies == nil {
		return fmt.Errorf("no policy holder")
	}
	// Pin to the exact normalised command as a literal-prefix regex so
	// arg-pattern variations still require a new approval.
	pattern := "^" + regexpQuote(CommandKey(op.Command)) + "($|\\s)"
	return g.Policies.AppendLearnedSafePattern(pattern)
}

// blacklist appends a forbidden-pattern entry. Stored in memory only;
// the operator must edit default.yaml to persist across restarts.
// (approve-and-remember persists to learned.yaml; deny-and-blacklist
// deliberately does not, because a wrong click should not be permanent.)
func (g *PolicyHostGate) blacklist(op cpn.GateOp) error {
	if g.Policies == nil {
		return fmt.Errorf("no policy holder")
	}
	h := g.Policies
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.policy
	if p == nil {
		return fmt.Errorf("no policy")
	}
	pattern := "^" + regexpQuote(CommandKey(op.Command)) + "($|\\s)"
	p.ForbiddenPat = append(p.ForbiddenPat, pattern)
	return p.finalize()
}

// regexpQuote is a tiny duplicate of regexp.QuoteMeta kept inline so the
// file is self-contained and easy to audit.
func regexpQuote(s string) string {
	const meta = `\.+*?()|[]{}^$`
	var out []byte
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(meta); j++ {
			if s[i] == meta[j] {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

// ── JSON helpers for the HTTP layer ─────────────────────────────────────────

// EncodePrompt marshals the prompt to JSON for transport.
func EncodePrompt(p HostApprovalPrompt) ([]byte, error) {
	p.Schema = HostApprovalSchema
	return json.Marshal(p)
}

// DecodeResponse unmarshals a HostApprovalResponse from JSON.
func DecodeResponse(raw []byte) (HostApprovalResponse, error) {
	var r HostApprovalResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return HostApprovalResponse{}, err
	}
	return r, nil
}
