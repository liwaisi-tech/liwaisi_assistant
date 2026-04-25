package toolbuilder

import (
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// OutputHook implements cpn.SubAgentOutputHook. It is the production
// glue that wires the SubAgentCatalog (validators, allowlist matrix)
// and the ArtifactGate to the cpn engine without forcing the cpn
// package to import toolbuilder (which would be a cycle).
//
// Wired by cmd/server/main.go on session creation.
type OutputHook struct {
	catalog *SubAgentCatalog
	gate    *ArtifactGate
}

// NewOutputHook returns a hook ready to wire into cpn.CPN.SubAgentHook.
// Errors only when the underlying catalog seed fails — that is a
// programmer error and should be surfaced at boot, not at firing time.
func NewOutputHook() (*OutputHook, error) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		return nil, fmt.Errorf("subagent output hook: %w", err)
	}
	return &OutputHook{
		catalog: cat,
		gate:    DefaultArtifactGate(),
	}, nil
}

// Validate runs StrictJSONValidator against the action's contract.
// Returns nil for transitions whose Meta.kind != "subagent" so the
// hook is safe to attach to any CPN.
//
// Reviewer outputs (ActionReviewSpec) are intentionally exempt: a strict
// schema failure on a reviewer token used to route to PlaceErrors and
// terminate the flow. Reviewers are advisory only — parseReviews already
// degrades unparseable output to a synthetic Review, so we let the lenient
// downstream parser do its job and keep the flow forward-progressing.
func (h *OutputHook) Validate(t *cpn.Transition, raw []byte) error {
	actionID, ok := subAgentActionID(t)
	if !ok {
		return nil
	}
	if actionID == ActionReviewSpec {
		return nil
	}
	action, ok := h.catalog.Actions.Get(actionID)
	if !ok {
		return fmt.Errorf("subagent validate: transition %s references unknown action %q", t.ID, actionID)
	}
	if h.catalog.Validator == nil {
		return nil
	}
	return h.catalog.Validator.Validate(action, raw)
}

// Gate runs the artifact gate when the action's capabilities declare
// EmitsCode/EmitsTests. AdvisoryOnly actions skip gating.
func (h *OutputHook) Gate(t *cpn.Transition, raw []byte) error {
	actionID, ok := subAgentActionID(t)
	if !ok {
		return nil
	}
	action, ok := h.catalog.Actions.Get(actionID)
	if !ok {
		return fmt.Errorf("subagent gate: transition %s references unknown action %q", t.ID, actionID)
	}
	if h.gate == nil {
		return nil
	}
	return h.gate.Gate(action, raw)
}

// Catalog exposes the underlying catalog so callers (tests, the
// session service) can introspect or swap the validator without
// reaching into private state.
func (h *OutputHook) Catalog() *SubAgentCatalog { return h.catalog }

// ArtifactGate exposes the underlying artifact gate similarly.
func (h *OutputHook) ArtifactGate() *ArtifactGate { return h.gate }

// SetArtifactGate swaps the artifact gate. Used by the boot path to
// install rules loaded from YAML over the embedded defaults.
func (h *OutputHook) SetArtifactGate(g *ArtifactGate) { h.gate = g }

// ScanArtifact implements infra/host/gate.ArtifactScanner so the host
// gate can delegate sub-agent artifact decisions to the same gate the
// cpn engine uses. Symmetric to OutputHook.Gate but keyed by ActionID
// directly (no Transition needed) — the host gate audits decisions via
// its own session-aware logger.
func (h *OutputHook) ScanArtifact(actionID string, raw []byte) error {
	if h == nil || h.gate == nil {
		return nil
	}
	action, ok := h.catalog.Actions.Get(actionID)
	if !ok {
		return fmt.Errorf("scan-artifact: unknown action %q", actionID)
	}
	return h.gate.Gate(action, raw)
}

func subAgentActionID(t *cpn.Transition) (string, bool) {
	if t == nil || t.Meta == nil || t.Meta["kind"] != "subagent" {
		return "", false
	}
	id := t.Meta["action_id"]
	if id == "" {
		return "", false
	}
	return id, true
}
