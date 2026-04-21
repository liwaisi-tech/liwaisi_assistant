package cpn

import "time"

// SizeCap bounds the size of a synthesised topology. Values ≤ 0 fall back
// to the spec §3 defaults (50 places, 50 transitions, 200 arcs — CON-001).
type SizeCap struct {
	MaxPlaces      int
	MaxTransitions int
	MaxArcs        int
}

// DefaultSizeCap returns the spec-mandated default cap.
func DefaultSizeCap() SizeCap {
	return SizeCap{MaxPlaces: 50, MaxTransitions: 50, MaxArcs: 200}
}

// Resolved returns a SizeCap with ≤0 fields filled from the default.
func (s SizeCap) Resolved() SizeCap {
	d := DefaultSizeCap()
	if s.MaxPlaces <= 0 {
		s.MaxPlaces = d.MaxPlaces
	}
	if s.MaxTransitions <= 0 {
		s.MaxTransitions = d.MaxTransitions
	}
	if s.MaxArcs <= 0 {
		s.MaxArcs = d.MaxArcs
	}
	return s
}

// SynthesizeConfig drives a NodeKindSynthesize transition (spec §4).
type SynthesizeConfig struct {
	// Model is the LLM model id; empty falls back to the CPN's default.
	Model string
	// SystemPrompt is the fixed instruction set (rules, catalogue, schema).
	// The task-specific body is appended separately.
	SystemPrompt string
	MaxTokens    int
	Temperature  float32
	// TaskPlaceholder is substituted with the consumed token's payload
	// inside SystemPrompt before the call. Empty means "append the task
	// verbatim as a user message".
	TaskPlaceholder string
	// SizeCap bounds the topology; DefaultSizeCap() when zero.
	SizeCap SizeCap
	// MaxCorrections bounds the auto-retry loop when the LLM returns a
	// topology that fails parsing or linting (REQ-022 / §9 edge). Zero
	// disables self-correction and falls straight through to ErrorPlace.
	MaxCorrections int
	// Summary is an optional static summary to attach to the produced
	// FlowRef token. When empty, fire_synthesize extracts one from the
	// parsed topology's name.
	Summary string
}

// InstantiateConfig drives a NodeKindInstantiate transition (spec §4).
type InstantiateConfig struct {
	// InputMapping wires parent output place → child source place.
	// Matching is by place ID; unmapped child sources receive the parent's
	// consumed token payload via Color-matching (same default rule as
	// NodeKindSubNet).
	InputMapping map[string]string
	// OutputMapping wires child terminal → parent output place by ID.
	// When empty, every child terminal token is deposited into every
	// OutputPlace listed on the transition (compatible with the SubNet
	// fan-out semantics).
	OutputMapping map[string]string
	// Timeout bounds the child run. Zero = inherit parent ctx.
	Timeout time.Duration
	// SkipHITL, when true, instantiates without ever surfacing a
	// topology.approval prompt. Reserved for tests and internal callers;
	// production topologies leave this false.
	SkipHITL bool
}
