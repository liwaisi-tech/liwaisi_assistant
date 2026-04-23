package architect

import (
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Strategy enumerates how the architect decided to satisfy a request
// (spec §4.3). The zero value is StrategyUnknown — callers MUST set a
// concrete strategy before emitting an ArchitectDraft.
type Strategy string

const (
	StrategyUnknown   Strategy = ""
	StrategyReuse     Strategy = "reuse"      // library hit, run as-is
	StrategyExtend    Strategy = "extend"     // library near-hit, mutate + run
	StrategyCompose   Strategy = "compose"    // no hit, compose from retrieved tools
	StrategyToolForge Strategy = "toolforge"  // missing capability → author new tool first
	StrategyReject    Strategy = "reject"     // no feasible plan (budget/refinement exhausted)
)

// HITLMode toggles whether the architect interacts with the user before
// materialising the plan. Defaults to HITLAuto (ask only when confidence is
// low or the plan touches host-gated capabilities).
type HITLMode string

const (
	HITLAuto   HITLMode = ""
	HITLAlways HITLMode = "always"
	HITLNever  HITLMode = "never"
)

// ArchitectRequest is the typed input to Plan. The intake transition
// (t-architect-intake) is responsible for normalising the user turn into
// this shape — tools/hashtag normalisation, capability inference, budget.
//
// Equality over ArchitectRequest must be structural for deterministic caching.
// When adding fields, keep them comparable value types or sorted slices.
type ArchitectRequest struct {
	SessionID       string
	TurnID          string
	NL              string
	Intent          cpn.Intent
	Hashtags        []string
	RequiredCaps    []string
	InputColors     []string
	Budget          cpn.SizeCap
	ParallelismHint string // "fanout" | "sequence" | ""
	HITLPreference  HITLMode
}

// ArchitectDraft is the planner's output — a single iteration of the
// design loop. The Architect LLM transition (to be added later) consumes
// ArchitectDrafts from Plan and decides whether to iterate with the user
// or proceed to validate+instantiate.
type ArchitectDraft struct {
	Strategy     Strategy
	BaseFlowID   string               // set when Strategy == Reuse | Extend
	MatchSet     cpn.ToolMatchSet     // tools this plan uses
	MissingCaps  []string             // capabilities with no covering tool
	TopologyBlob []byte               // canonical JSON from the jit composer (empty on Reuse)
	UserPrompt   string               // optional HITL question
	Confidence   float64              // 0..1
	Reason       string               // human-readable rationale
	Candidates   []*cpn.FlowLibraryEntry // library near-matches (sorted best-first)
}

// LibraryPort is the narrow window the architect needs into the flow
// library. Production code passes a *cpn.FlowLibrary; tests can stub it.
type LibraryPort interface {
	FindCovering(hashtags, caps, inputColors []string) []*cpn.FlowLibraryEntry
	GetEntry(hash string) (*cpn.FlowLibraryEntry, bool)
}

// ToolRetriever ranks tools against the caller's hashtags + capabilities.
// Implementations must be deterministic: same request → same ordered result.
type ToolRetriever interface {
	Match(req ArchitectRequest) (cpn.ToolMatchSet, error)
}
