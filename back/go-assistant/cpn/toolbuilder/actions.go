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
	ActionReviewSpec      = "review-spec"
	ActionSecurityEval    = "security-eval"
	ActionReviewCode      = "review-code"
	ActionRefineSpec      = "refine-spec"
	ActionDecomposeTotals = "decompose-totals"
	ActionPlanSubtasks    = "plan-subtasks"
	// Reserved for future PRs:
	// ActionBuildTDDFromSpec = "build-tdd-from-spec"
)

// seedActions returns the built-in action catalog. PR1 shipped
// review-spec. PR2 adds security-eval + review-code. PR4 adds
// refine-spec / decompose-totals / plan-subtasks.
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
			OutputSchema:  `{"role":"<your-profile-id>","approved":bool,"blocking_issues":[string],"suggestions":[string]}`,
			FewShot:       `{"role":"go-eng","approved":false,"blocking_issues":["ports import adapters"],"suggestions":["prefer table-driven tests for edge cases"]}`,
			Capabilities:  ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: nil,
		},
		{
			ID:         ActionSecurityEval,
			Verb:       "Audit",
			InputToken: "ARTIFACT",
			Instructions: `Read the <ARTIFACT> (spec or code) between the delimiters. Classify threats using STRIDE
(Spoofing, Tampering, Repudiation, Info-disclosure, Denial-of-service, Elevation). For each threat assign a severity
(low|med|high|crit) and a concrete mitigation. Emit required_controls listing controls that MUST be present. Approve only
when zero high/crit threats remain. Do NOT emit executable code — this action is advisory-only.`,
			OutputSchema:  `{"role":"security","approved":bool,"threats":[{"id":string,"stride":"S|T|R|I|D|E","severity":"low|med|high|crit","mitigation":string}],"required_controls":[string]}`,
			FewShot:       `{"role":"security","approved":false,"threats":[{"id":"T1","stride":"I","severity":"high","mitigation":"move secret to env var"}],"required_controls":["no secrets in source"]}`,
			Capabilities:  ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: []string{"read_file"},
		},
		{
			ID:         ActionReviewCode,
			Verb:       "Review",
			InputToken: "CODE",
			Instructions: `Read the <CODE> between the delimiters. Apply your profile's domain lens to the source.
For each finding include file, 1-indexed line, severity, a short message, and a fix_hint. Approve only when zero blocking
findings remain. Non-blocking observations go into style_notes. Suggested patches are optional and MUST be minimal unified
diffs keyed by path.`,
			OutputSchema:  `{"role":"<your-profile-id>","approved":bool,"findings":[{"file":string,"line":int,"severity":"low|med|high","msg":string,"fix_hint":string}],"style_notes":[string],"suggested_patches":[{"path":string,"diff":string}]}`,
			FewShot:       `{"role":"go-eng","approved":false,"findings":[{"file":"main.go","line":42,"severity":"med","msg":"ignored error","fix_hint":"return the error"}],"style_notes":[],"suggested_patches":[]}`,
			Capabilities:  ActionCapabilities{EmitsCode: true},
			RequiredTools: []string{"read_file"},
		},
		{
			ID:         ActionRefineSpec,
			Verb:       "Produce",
			InputToken: "VERDICT_AND_SPEC",
			Instructions: `Read the <VERDICT_AND_SPEC> which contains a Verdict plus the prior SpecDraft. Produce an improved
SpecDraft that resolves every blocking issue from the verdict. Preserve all fields outside the scope of the blocking
issues. Increment "refine_count" by 1 so the outer loop terminates.`,
			OutputSchema:  `{"name":string,"purpose":string,"non_goals":[string],"domain_model":string,"ports":[{"name":string,"purpose":string}],"adapters":[{"name":string,"implements_port":string,"summary":string}],"cli_surface":string,"test_plan":[string],"risks":[string],"refine_count":int}`,
			FewShot:       "",
			Capabilities:  ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: nil,
		},
		{
			ID:         ActionDecomposeTotals,
			Verb:       "Produce",
			InputToken: "SPEC_APPROVED",
			Instructions: `Read the <SPEC_APPROVED>. Split the work into independent high-level workstreams ("totals"):
1 ≤ N ≤ MaxTotals. Each total has a unique ID, non-empty deliverables, and a DAG of dependencies (no cycles). Set
parallel_safe=true only when the total has no runtime-file conflict with any other total.`,
			OutputSchema:  `{"spec_name":string,"totals":[{"id":string,"name":string,"deliverables":[string],"parallel_safe":bool,"depends_on":[string]}]}`,
			FewShot:       "",
			Capabilities:  ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: nil,
		},
		{
			ID:         ActionPlanSubtasks,
			Verb:       "Produce",
			InputToken: "TOTAL_READY",
			Instructions: `Read the <TOTAL_READY>. For each total, produce an ordered list of subtasks. Each subtask
carries ONLY the context strictly needed to write a single failing test + implementation: files_to_touch, test_name, and
a one-line purpose. Set parallel_safe=true only when the subtask has no shared-file conflict with peers.`,
			OutputSchema:  `{"spec_name":string,"by_total":[{"total_id":string,"subtasks":[{"id":string,"purpose":string,"files_to_touch":[string],"test_name":string,"parallel_safe":bool,"depends_on":[string]}]}]}`,
			FewShot:       "",
			Capabilities:  ActionCapabilities{AdvisoryOnly: true},
			RequiredTools: nil,
		},
	}
}
