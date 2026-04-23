package toolbuilder

// reviewerRole couples a role's identifiers with its prompt and the
// topology place IDs it reads from / writes to. Defined once so
// topology construction and aggregation stay in lock-step.
type reviewerRole struct {
	TransitionID string
	InputPlace   string
	OutputPlace  string
	Role         string // logical LLM role (also the Review.Role field value)
	Prompt       string
}

// reviewerRoles enumerates the 6 parallel expert reviewers. Order is
// stable; aggregator iterates in this order.
var reviewerRoles = []reviewerRole{
	{TrReviewGo, PlaceSpecForGo, PlaceReviewGo, "go-engineer", promptGoEngineer},
	{TrReviewAI, PlaceSpecForAI, PlaceReviewAI, "ai-engineer", promptAIEngineer},
	{TrReviewDevOps, PlaceSpecForDevOps, PlaceReviewDevOps, "devops-engineer", promptDevOps},
	{TrReviewQA, PlaceSpecForQA, PlaceReviewQA, "qa-engineer", promptQA},
	{TrReviewArch, PlaceSpecForArch, PlaceReviewArch, "architect", promptArchitect},
	{TrReviewPM, PlaceSpecForPM, PlaceReviewPM, "product-manager", promptPM},
}

// ReviewerRoles returns the stable ordering used by the aggregator and
// by t-fanout-reviewers. Exposed for tests; copies the slice so callers
// cannot mutate internals.
func ReviewerRoles() []string {
	out := make([]string, len(reviewerRoles))
	for i, r := range reviewerRoles {
		out[i] = r.Role
	}
	return out
}

// All reviewer prompts share the same output contract: a JSON object
//   {"role":"...","approved":bool,"blocking_issues":[],"suggestions":[]}
// The fanout transition injects the role-specific prompt; the review
// transition's SystemPrompt embeds it.

const promptGoEngineer = `You are a senior Go engineer reviewing a tool specification.
Evaluate: idiomatic Go usage, clear error handling, stdlib-first design,
sensible use of interfaces (not over-engineered), naming, package layout.
Red flags: needless interfaces, panic misuse, "interface{}" leaks,
non-idiomatic names, over-use of generics.
Output ONLY valid JSON: {"role":"go-engineer","approved":bool,"blocking_issues":[string],"suggestions":[string]}`

const promptAIEngineer = `You are a senior AI / LLM-tool-use engineer reviewing a tool specification.
Evaluate: is this tool's surface easy for an LLM to call? JSON shape clarity,
--help usable by an LLM, sensible argument schema, predictable failure modes.
Red flags: overstuffed arguments, ambiguous schemas, hidden state, no --help.
Output ONLY valid JSON: {"role":"ai-engineer","approved":bool,"blocking_issues":[string],"suggestions":[string]}`

const promptDevOps = `You are a senior DevOps engineer reviewing a tool specification.
Evaluate: reproducible build, Makefile soundness, install.sh safety, sandbox
compatibility (no privileged ops, no network calls during tests), sensible paths.
Red flags: sudo, network dependency in tests, /tmp races, non-reproducible build.
Output ONLY valid JSON: {"role":"devops-engineer","approved":bool,"blocking_issues":[string],"suggestions":[string]}`

const promptQA = `You are a senior QA engineer reviewing a tool specification.
Evaluate: testability, TDD seams, table-driven potential, coverage ≥85% feasibility,
edge-case identification.
Red flags: untestable private state, missing edge cases, weak assertions,
no coverage plan.
Output ONLY valid JSON: {"role":"qa-engineer","approved":bool,"blocking_issues":[string],"suggestions":[string]}`

const promptArchitect = `You are a senior software architect reviewing a tool specification.
Evaluate: hexagonal cleanliness, dependency direction (domain → nothing),
port/adapter separation, no leaky abstractions, single responsibility.
Red flags: domain importing adapters, fat ports, mixed responsibilities,
bidirectional deps.
Output ONLY valid JSON: {"role":"architect","approved":bool,"blocking_issues":[string],"suggestions":[string]}`

const promptPM = `You are a senior product manager reviewing a tool specification.
Evaluate: clarity of user value, scope discipline, non-goals completeness,
success criteria.
Red flags: feature creep, unclear outcome, vague success criteria, missing non-goals.
Output ONLY valid JSON: {"role":"product-manager","approved":bool,"blocking_issues":[string],"suggestions":[string]}`
