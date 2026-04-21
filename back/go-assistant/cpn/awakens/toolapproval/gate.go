package toolapproval

import (
	"context"
	"errors"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

// ErrGateMisconfigured is returned by CheckSynthesized when the Gate lacks
// a required collaborator. Fail-closed: callers surface this as a denial.
var ErrGateMisconfigured = errors.New("toolapproval: gate misconfigured")

// HITLPrompter is the integration seam between SC-13 and the A2UI SSE
// surface. Production wires this to the real message bus; tests stub it.
// Implementations MUST block until the operator decides or the context
// cancels; returning an error aborts the gate check fail-closed.
type HITLPrompter interface {
	PromptAndAwait(ctx context.Context, preview awakens.A2UIMessage) (Decision, error)
}

// Gate enforces first-run HITL approval for synthesized tools.
type Gate struct {
	Approvals ApprovalStore
	Pending   toolsynth.PendingToolStore
	Emitter   *awakens.Emitter
	Prompter  HITLPrompter
	Clock     func() time.Time
}

// CheckSynthesized is the SC-13 decision gate. Returns DecisionApproved
// (invocation may proceed) or DecisionDenied (invocation MUST be blocked).
// Non-synthesized manifests pass through with DecisionApproved and no
// side-effects.
func (g *Gate) CheckSynthesized(ctx context.Context, sessionID string, manifest cpn.ToolManifest) (Decision, error) {
	if manifest.Kind != toolsynth.KindSynthesized {
		return DecisionApproved, nil
	}
	if g == nil || g.Approvals == nil || g.Pending == nil || g.Prompter == nil {
		return DecisionDenied, ErrGateMisconfigured
	}
	if sessionID == "" || manifest.Name == "" {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, "", "missing_key")
		return DecisionDenied, ErrInvalidKey
	}

	pending, ok, err := g.Pending.Get(ctx, manifest.Name)
	if err != nil {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, "", "pending_lookup_error")
		return DecisionDenied, err
	}
	if !ok {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, "", "pending_not_found")
		return DecisionDenied, toolsynth.ErrPendingNotFound
	}
	provenance := pending.ProvenanceSHA256
	if provenance == "" {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, "", "empty_provenance")
		return DecisionDenied, ErrInvalidKey
	}

	switch decision, lookupErr := g.Approvals.Lookup(ctx, sessionID, manifest.Name, provenance); {
	case lookupErr != nil:
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, provenance, "approval_lookup_error")
		return DecisionDenied, lookupErr
	case decision == DecisionApproved:
		g.Emitter.SynthesizedToolAutoApproved(ctx, sessionID, manifest.Name, provenance)
		return DecisionApproved, nil
	}

	preview := BuildHITLPreview(pending)
	g.Emitter.SynthesizedToolHITLRequested(ctx, sessionID, manifest.Name, provenance)

	decision, err := g.Prompter.PromptAndAwait(ctx, preview)
	if err != nil {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, provenance, "prompter_error")
		return DecisionDenied, err
	}
	if decision != DecisionApproved {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, provenance, "operator_denied")
		return DecisionDenied, nil
	}

	clock := g.Clock
	if clock == nil {
		clock = time.Now
	}
	if err := g.Approvals.Record(ctx, Approval{
		SessionID:        sessionID,
		ToolName:         manifest.Name,
		ProvenanceSHA256: provenance,
		ApprovedAt:       clock().UTC(),
	}); err != nil {
		g.Emitter.SynthesizedToolDenied(ctx, sessionID, manifest.Name, provenance, "approval_record_error")
		return DecisionDenied, err
	}
	g.Emitter.SynthesizedToolApproved(ctx, sessionID, manifest.Name, provenance)
	return DecisionApproved, nil
}
