package toolbuilder

// ActionSpec describes WHAT a sub-agent does in a single CPN transition.
// It is profile-agnostic: the same action is reusable across many
// profiles (e.g. review-spec × {pm, arch, qa, devops, ai-eng, go-eng}).
//
// ActionSpec carries the task verb, the input token contract, the
// output JSON schema, termination rules, and a few-shot example. It
// MUST NOT embed any profile-specific persona text.
//
// OutputSchema is the JSON schema the LLM output MUST satisfy. In PR1
// it is only embedded in the prompt and used by the passthrough
// validator (no enforcement). PR2 swaps in a strict validator.
type ActionSpec struct {
	ID              string
	Verb            string   // imperative task verb ("Review", "Produce", "Audit")
	InputToken      string   // symbolic name used in the prompt (e.g. "SPEC_DRAFT")
	Instructions    string   // task-shaped prose: rubric, decision ladder, severity
	OutputSchema    string   // JSON schema excerpt shown to the LLM
	FewShot         string   // golden example matching OutputSchema; may be empty
	Capabilities    ActionCapabilities
	RequiredTools   []string // tools this action needs; intersected with profile.MaxTools
}

// ActionCapabilities declares what the action's output is allowed to
// influence downstream. Used by policy_gate (PR2) to decide whether
// gating is required before a runner consumes the artifact.
type ActionCapabilities struct {
	EmitsCode     bool // output contains executable code
	EmitsTests    bool // output contains test source
	AdvisoryOnly  bool // structured-data only; cannot be executed
}

// ActionID constants — stable across the repo.
const (
	ActionReviewSpec = "review-spec"
	// Reserved for PR2/PR4:
	// ActionBuildTDDFromSpec = "build-tdd-from-spec"
	// ActionSecurityEval     = "security-eval"
	// ActionReviewCode       = "review-code"
	// ActionRefineSpec       = "refine-spec"
	// ActionDecomposeTotals  = "decompose-totals"
	// ActionPlanSubtasks     = "plan-subtasks"
)

// seedActions returns the built-in action catalog for PR1. Expanded in
// PR2 (security-eval, review-code) and PR4 (refine, decompose, plan).
func seedActions() []ActionSpec {
	return []ActionSpec{
		{
			ID:         ActionReviewSpec,
			Verb:       "Review",
			InputToken: "SPEC_DRAFT",
			Instructions: `Read the <SPEC_DRAFT> between the delimiters. Apply your profile's domain lens and red-flag taxonomy.
Emit a verdict: approve only when there are zero blocking issues within your discipline; otherwise list every blocking issue
with a short, specific message. Non-blocking observations go into suggestions. Stay within your epistemic limits — defer
issues outside your discipline rather than opining on them.`,
			OutputSchema: `{"role":"<your-profile-id>","approved":bool,"blocking_issues":[string],"suggestions":[string]}`,
			FewShot:      `{"role":"go-eng","approved":false,"blocking_issues":["ports import adapters"],"suggestions":["prefer table-driven tests for edge cases"]}`,
			Capabilities: ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: nil,
		},
	}
}
