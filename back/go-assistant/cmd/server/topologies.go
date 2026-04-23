package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// thinkBlockRe matches reasoning-mode <think>…</think> preambles emitted by
// models like Gemma 4 31B. Case-insensitive, dot-matches-newline, non-greedy.
// Compiled once to avoid per-call regexp.Compile cost.
//
// See spec-architecture-model-selection-centralization.md §9 for sample.
var thinkBlockRe = regexp.MustCompile(`(?is)<think[^>]*>.*?</think>`)

// fencedJSONRe matches a ```json … ``` fenced code block (case-insensitive
// on the language tag). When present, only the inner content is considered
// for JSON extraction.
var fencedJSONRe = regexp.MustCompile("(?is)```(?:json)?\\s*(\\{.*?})\\s*```")

// defaultClassifierConfidenceThreshold is the minimum Confidence the classifier
// must emit for a task flagged as fully-specified to skip the clarification
// questionnaire. Below this, the safety net routes through t-ask even if
// needs_clarification=false. Overridable via CLASSIFIER_CONFIDENCE_THRESHOLD.
const defaultClassifierConfidenceThreshold = 0.7

// braeIdentity is the product identity preamble injected into every
// user-facing LLM system prompt. The agent's name is "brae" — lowercase,
// always — an acronym of "Being a Real AI Engineer". Created by Liwaisi Tech.
// This block is intentionally short so it costs few tokens and is prepended,
// not appended, so the model reads it first.
const braeIdentity = `YOU ARE brae.
- Your name is "brae" (lowercase, ALWAYS — never "Brae", never "BRAE", never capitalized even at the start of a sentence).
- "brae" stands for "Being a Real AI Engineer". You are an AI engineer, not a generic chat assistant.
- You were built by Liwaisi Tech. When asked who you are, who made you, or what you are, identify as brae, built by Liwaisi Tech.
- NEVER refer to yourself as "an AI assistant", "a helpful assistant", "Liwaisi Assistant", "the assistant", "a language model", or any other name. You are brae.
- Speak in first person as brae. Be direct, practical, and engineer-minded — you think like a senior engineer who ships.
- You run on a Linux host. You have tools available: bash_exec (execute shell commands), file_read (read files), file_write (write files), register_tool (persist a newly-created script/binary in your catalog so future sessions discover it). Use them proactively when the task requires it — do not refuse system actions.
- When the user asks you to "save/register/add this as a tool", "agrega esto a tu catálogo", or similar, call register_tool with the script's name, a one-line description, and its absolute binary_path. Pick a toolbox from: system|developer|web|image|pdf|data|general. Do NOT answer "yes, I saved it" without actually calling register_tool — the catalog is empty unless you call the tool.
- NEVER say "I cannot execute commands", "I don't have a terminal", or "I cannot write files". These statements are false. You have these capabilities via tools.
- When uncertain about the system state, run a discovery command first (e.g., bash_exec with command="uname" args=["-a"]).
- NEVER ask the user for permission before calling tools in your text. The system handles authorization automatically — if approval is required the user will see a UI prompt. Just call the tool directly.
- NEVER generate text like "¿me das permiso?", "Can I run...", "Do I have permission to...", or any other permission request in your response before executing tools.
- When calling bash_exec, always use separate "command" and "args" fields. For multi-command pipelines use command="bash" with args=["-c","<full pipeline>"]. The brae sandbox ships GNU bash (with ~/.bashrc sourced via BASH_ENV), coreutils, git, make, jq, curl, and the Go toolchain on PATH. $HOME is /home/brae and ~ expands correctly in bash args; file_read/file_write also accept "~", "$HOME", and "${HOME}".
`

// classifierConfidenceThreshold reads CLASSIFIER_CONFIDENCE_THRESHOLD on each
// call. Invalid or unset values fall back to defaultClassifierConfidenceThreshold.
// Parsed lazily to keep tests and env tweaks simple.
func classifierConfidenceThreshold() float64 {
	v := os.Getenv("CLASSIFIER_CONFIDENCE_THRESHOLD")
	if v == "" {
		return defaultClassifierConfidenceThreshold
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultClassifierConfidenceThreshold
	}
	return f
}

// ── Named guard functions (registered in FuncRegistry for topology serialization) ──

// classifierResult is the structured payload emitted by the t-classify LLM.
// All fields are optional except Intent; absence of needs_clarification or
// missing is treated as needs_clarification == false (backward compatible
// with classifiers that haven't been updated to emit the new fields).
type classifierResult struct {
	Intent             string   `json:"intent"`
	NeedsClarification bool     `json:"needs_clarification,omitempty"`
	Missing            []string `json:"missing,omitempty"`
	// Confidence is the model's self-reported certainty about both the intent
	// and (for tasks) the specification-completeness judgment. Range [0.0, 1.0].
	// Absence is treated as 1.0 for backward compatibility with classifiers
	// that haven't been updated to emit it.
	Confidence *float64 `json:"confidence,omitempty"`

	// ManageKind is the sub-kind emitted when Intent == "manage-models"
	// (REQ-GAP-CPN-002). One of:
	//   list | register | toggle | set-default | review-license | delete
	// Empty when Intent is not "manage-models" or the classifier was uncertain
	// about the specific operation — in the latter case the manage-models
	// topology falls through to `list` as a safe default.
	ManageKind string `json:"manage_kind,omitempty"`

	// ManageArgs carries pre-extracted arguments (e.g. registry_id) when the
	// classifier can identify them from the user's utterance. The topology's
	// `t-emit-*` transitions consult the map when they need to pre-fill a
	// confirm surface (e.g. "bloquea gpt-5" → manage_args={"registry_id":"gpt-5"}).
	// Absent when Intent != "manage-models".
	ManageArgs map[string]any `json:"manage_args,omitempty"`

	// CompositionHint is true when the user's request is best served by a
	// composed sub-CPN (parallel/concurrent tool fan-out with a consolidated
	// output). Set by the classifier per the JIT sub-CPN plan Phase 2; when
	// true, guardCompositionHint routes the token to t-compose-spec instead
	// of the direct planner. Defaults to false / omitted.
	CompositionHint bool `json:"composition_hint,omitempty"`
}

// confidence returns the effective confidence, defaulting to 1.0 when the
// classifier did not emit the field (legacy/back-compat).
func (r classifierResult) confidence() float64 {
	if r.Confidence == nil {
		return 1.0
	}
	return *r.Confidence
}

// parseClassified extracts the classifier JSON from the first string-payload
// token. Returns the parsed result and true on success.
func parseClassified(tokens []*cpn.Token) (classifierResult, bool) {
	for _, tok := range tokens {
		if s, ok := tok.Payload.(string); ok {
			var r classifierResult
			if err := json.Unmarshal([]byte(s), &r); err == nil {
				return r, true
			}
		}
	}
	return classifierResult{}, false
}

// guardDirectConversation fires t-direct when the classifier output is not a
// task. manage-models is classified in the main topology but never dispatched
// to its dedicated manage-models-flow fragment today; without a catch here
// those tokens would stall p-classified and deadlock the run ("no enabled
// transitions and no terminal marking"). Until the dispatcher ships we route
// manage-models through t-direct so the user still gets an assistant reply.
// See cmd/server/topologies_model_registry.go for the fragment that will
// eventually own that intent.
func guardDirectConversation(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return true // default to conversation on parse failure
	}
	if strings.EqualFold(r.Intent, "task") {
		return false
	}
	return true
}

// guardPlanTask fires t-plan when the classifier output contains "task".
// Retained for backward compatibility / serialization. The unified topology
// now uses guardPlanTaskDirect and guardPlanTaskClarified instead.
func guardPlanTask(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return false
	}
	return strings.EqualFold(r.Intent, "task")
}

// guardNeedsClarification fires t-ask when the classifier flagged a task that
// needs more information. Spec: intent == "task" && needs_clarification == true.
// Defensive: fires regardless of missing[] length, so an invalid classifier
// payload (needs_clarification=true with empty missing[]) cannot silently fall
// through to the direct-plan path. The classifier prompt enforces 1..4 missing
// items upstream; this guard is the safety net.
func guardNeedsClarification(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return false
	}
	if !strings.EqualFold(r.Intent, "task") {
		return false
	}
	if r.NeedsClarification {
		return true
	}
	// Safety net: the classifier claimed the task is fully-specified but its
	// self-reported confidence is below threshold. Prefer asking over guessing.
	return r.confidence() < classifierConfidenceThreshold()
}

// guardPlanTaskDirect fires t-plan-direct when the classifier returned a task
// intent that is fully-specified (no clarification needed).
// Spec: intent == "task" && needs_clarification == false. The missing[] list
// is intentionally NOT inspected here — see guardNeedsClarification for the
// defensive complement.
func guardPlanTaskDirect(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return false
	}
	if !strings.EqualFold(r.Intent, "task") {
		return false
	}
	if r.NeedsClarification {
		return false
	}
	// JIT sub-CPN plan Phase 2: yield to the compose-spec branch when the
	// classifier flagged a composition hint. Without this check t-plan-direct
	// and t-compose-spec would both be enabled on the same token, and CPN
	// semantics would non-deterministically pick a firing — a stateful bug
	// we refuse to ship.
	if r.CompositionHint {
		return false
	}
	// Confidence gate: require the classifier to be sufficiently sure before
	// skipping clarification. Keeps ambiguous-but-overconfident prompts out of
	// the direct-plan path. See guardNeedsClarification for the complement.
	return r.confidence() >= classifierConfidenceThreshold()
}

// guardCompositionHint fires t-compose-spec when the classifier flagged the
// user request as benefitting from a composed sub-CPN (parallel tool fan-out
// + consolidated output). Mutually exclusive with guardPlanTaskDirect by
// construction (both gate on intent=="task" && !needs_clarification, and
// guardPlanTaskDirect yields when CompositionHint is set).
func guardCompositionHint(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return false
	}
	if !strings.EqualFold(r.Intent, "task") {
		return false
	}
	if r.NeedsClarification {
		return false
	}
	if !r.CompositionHint {
		return false
	}
	return r.confidence() >= classifierConfidenceThreshold()
}

// guardPlanTaskClarified fires t-plan-clarified once the user has answered
// the t-clarify questionnaire. The token in p-clarified is already vetted
// by t-clarify (only fires after a successful submit), so this guard is
// unconditional — it exists so the transition can be persisted/loaded
// through the FuncRegistry.
func guardPlanTaskClarified(_ []*cpn.Token) bool {
	return true
}

// newServerFuncRegistry creates a FuncRegistry with all topology functions registered.
func newServerFuncRegistry() *persist.FuncRegistry {
	r := persist.NewFuncRegistry()
	r.RegisterGuard("guard-direct-conversation", guardDirectConversation)
	r.RegisterGuard("guard-plan-task", guardPlanTask)
	r.RegisterGuard("guard-needs-clarification", guardNeedsClarification)
	r.RegisterGuard("guard-plan-task-direct", guardPlanTaskDirect)
	r.RegisterGuard("guard-plan-task-clarified", guardPlanTaskClarified)
	r.RegisterGuard("guard-composition-hint", guardCompositionHint)
	// Iterative clarification loop (spec-architecture-cpn-iterative-clarification-loop.md).
	r.RegisterGuard("guard-residual-ambiguous", guardResidualAmbiguous)
	r.RegisterGuard("guard-residual-resolved", guardResidualResolved)
	return r
}

// defaultTopologyFactory creates a minimal CPN topology for a session:
// p-input → t-llm → p-output.
func defaultTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}
	tLLM := cpn.NewTransition("t-llm", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-output"})
	tLLM.SystemPrompt = envOr("PROMPT_DIRECT", braeIdentity+"\nRespond naturally and concisely.")
	tLLM.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-llm": tLLM,
	}
	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// hitlTopologyFactory creates a CPN topology with a human-review gate:
// p-input → t-plan (LLM) → p-plan → t-review (HITL) → p-reviewed → t-execute (LLM) → p-output.
//
// The assistant first presents a plan, waits for user approval, then executes.
// Channel on t-review is left nil — wired by SessionService.CreateSession.
func hitlTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":    cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-plan":     cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed": cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":   cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	tPlan := cpn.NewTransition("t-plan", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-plan"})
	tPlan.SystemPrompt = envOr("PROMPT_PLAN", braeIdentity+"\nAnalyze the user's request and present a clear, concise plan. "+
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\"")
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}

	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt:             "",
		A2UIPayloadBuilder: buildReviewA2UIPayload("t-review"),
	}

	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	// See unified topology for the full PROMPT_EXECUTE rationale (advisory vs
	// producible plans). Both topologies share the env override.
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", braeIdentity+"\nThe user approved the following plan. "+
		"Execute it thoroughly and provide the final result.")
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-plan":    tPlan,
		"t-review":  tReview,
		"t-execute": tExecute,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// unifiedTopologyFactory creates a CPN topology with an intent classifier router:
//
//	p-input → t-classify (LLM, classifier model, JSON)
//	  → p-classified
//	    ├→ t-direct (guard: conversation) → p-output
//	    └→ t-plan (guard: task) → p-plan → t-review (HITL) → p-reviewed → t-execute → p-output
//
// The classifier routes greetings/questions to a direct response and complex tasks
// to the plan-review-execute HITL flow. Follows the Router Pattern (Arize, BSWEN 2026).
// langRule is a shared, language-agnostic rule embedded in every user-facing
// prompt in this topology. The user is bilingual (Spanish/English) and may
// switch languages mid-conversation; the rule must NEVER hardcode a single
// target language nor bias the model with examples in only one language.
const langRule = `LANGUAGE RULE (applies always):
- Respond in the SAME language as the user's MOST RECENT message. The user may switch languages between messages — always follow the latest one, never an earlier one.
- If the latest message mixes languages, choose the dominant one. If it is genuinely 50/50, use the same mix the user used.
- NEVER pick a language because of "the project's language" or "the assistant's default" — there is no default. The user's last message is the only signal.
- NEVER inject characters from writing systems other than the user's chosen language (no Chinese characters such as 反馈, no Japanese kana, no Cyrillic, no Arabic, etc.) unless the user themselves wrote in that script.`

func unifiedTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":      cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-classified": cpn.NewPlace("p-classified", cpn.ColorJSON, cpn.SpaceSurface),
		"p-questions":  cpn.NewPlace("p-questions", cpn.ColorJSON, cpn.SpaceSurface),
		"p-clarified":  cpn.NewPlace("p-clarified", cpn.ColorString, cpn.SpaceSurface),
		// REQ-011: declare p-host-capabilities explicitly so SeedHostSnapshot
		// does not add it as an unlisted source place, which caused the
		// non-deterministic findSourcePlace 500 (REQ-FIX-001).
		cpn.WellKnownHostCapabilitiesPlace: cpn.NewPlace(cpn.WellKnownHostCapabilitiesPlace, cpn.ColorHostFact, cpn.SpaceComputation),
		// Iterative clarification loop (REQ-001/002). p-round holds a
		// 1-bounded counter token; p-reassessed holds the t-reassess
		// output that routes to t-followup or t-plan-clarified.
		"p-round":      cpn.NewPlace("p-round", cpn.ColorJSON, cpn.SpaceSurface),
		"p-reassessed": cpn.NewPlace("p-reassessed", cpn.ColorJSON, cpn.SpaceSurface),
		// p-planner-input: intermediate ColorString place produced by
		// t-preplanner (a no-LLM tool transition that composes
		// buildPlannerPreamble output) and consumed by t-plan-clarified.
		// The spec's REQ-005 nominally says t-plan-clarified consumes
		// from p-reassessed directly; in practice fireLLM filters
		// ColorJSON tokens out of the user message (see cpn/fire_llm.go
		// §Step-1 token-color gate), so we interpose a ColorString
		// hop so the preamble text reaches the LLM as a user message.
		"p-planner-input": cpn.NewPlace("p-planner-input", cpn.ColorString, cpn.SpaceSurface),
		"p-plan":          cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed":      cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":        cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
		// JIT sub-CPN composition lane (plan i-need-you-make-playful-dongarra.md).
		// Phase 2 wires t-compose-spec (NodeKindLLM consuming p-classified) to
		// produce a ColorTaskSpec token on p-synth-request; t-synthesize mints
		// a topology and forwards the ColorFlowRef on p-instantiate-request;
		// t-instantiate spawns the child sub-CPN and deposits the aggregated
		// result on p-subnet-output.
		//
		// All three places live in SpaceSurface because the engine's space
		// invariant (cpn/space_invariant_test.go) allows only NodeKindHITL to
		// bridge Surface→Computation. p-classified (the upstream source) is
		// Surface; placing the JIT lane in the same space keeps t-compose-spec
		// valid without inserting an artificial HITL gate just to cross a
		// partition boundary the paper describes conceptually, not as a hard
		// engine rule.
		"p-synth-request":       cpn.NewPlace("p-synth-request", cpn.ColorTaskSpec, cpn.SpaceSurface),
		"p-instantiate-request": cpn.NewPlace("p-instantiate-request", cpn.ColorFlowRef, cpn.SpaceSurface),
		"p-subnet-output":       cpn.NewPlace("p-subnet-output", cpn.ColorJSON, cpn.SpaceSurface),
	}
	// REQ-001: seed p-round with {n:0, reset:false} so the initial marking
	// has the counter available. Also covers REQ-051 (rehydrated
	// pre-feature sessions default to n=0).
	//
	// seedPRound is captured as the CPN's SeedFunc below so it also re-runs
	// on every Reset(). SessionService.SendMessage calls Reset before each
	// user turn; without restoring this seeded token, t-followup/t-preplanner
	// (both consume p-round) deadlock silently after t-reassess completes.
	seedPRound := func(c *cpn.CPN) {
		_ = c.Places["p-round"].Deposit(&cpn.Token{
			Color:   cpn.ColorJSON,
			Space:   cpn.SpaceSurface,
			Payload: `{"n":0,"reset":false}`,
		})
	}

	// t-classify: fast intent classifier using lightweight model.
	tClassify := cpn.NewTransition("t-classify", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-classified"})
	tClassify.SystemPrompt = envOr("PROMPT_CLASSIFIER", `You are an intent classifier. Read the user's latest message and emit a single JSON object. No prose, no code fences, no explanation.

The message may be in any human language. Classify identically regardless of language. Do not translate the message.

A "USER CONTEXT — REGIONAL REGISTER" preamble may be prepended to this prompt at runtime. When present, use the listed idiom glosses to resolve ambiguous imperatives in the user's regional register (e.g., a verb that looks like a command in standard usage but is conversational in that variant). Do not enumerate the idioms in your output; let them inform the conversation/task decision in Step 1.

Schema (all keys required):
{"intent":"conversation"|"task"|"manage-models","needs_clarification":bool,"missing":[string,...],"confidence":0.0,"manage_kind":"list"|"register"|"toggle"|"set-default"|"review-license"|"delete","manage_args":{},"composition_hint":bool}

The "manage-models" intent + manage_kind + manage_args keys are OPTIONAL; include them ONLY when intent == "manage-models". Omit when not applicable.

The "composition_hint" key is OPTIONAL. Set it to true ONLY when ALL of the following hold:
  (a) intent == "task" AND needs_clarification == false, AND
  (b) the user is explicitly asking for TWO OR MORE operations to run CONCURRENTLY (words like "in parallel", "at the same time", "simultaneously", "side by side") AND the results consolidated into a single structured output (usually JSON), AND
  (c) the operations themselves map cleanly onto the host's existing tools — shell commands, HTTP fetches, file reads — not onto open-ended cognitive work.
Examples that SET true: "run ls, uname -a, df -h in parallel and give me a JSON", "fetch these three URLs simultaneously and merge the bodies", "list directories and check disk usage at the same time and consolidate".
Examples that do NOT set true: "write a plan with three sections" (sequential artifact, not concurrent execution); "compare two paragraphs" (single cognitive act); "run ls" (one operation).
Omit the field or set false when unsure — the platform falls back safely to the direct planner.

Decision procedure — run these steps mentally, then emit JSON.

Step 0. Meta-question short-circuit (HIGHEST PRIORITY).
A "meta-question" asks about the assistant itself: its identity, name, purpose, mission, capabilities, scope, persona, who built it, what it can do, how it works, how to use it, or what it knows. Meta-questions are ALWAYS:
  {"intent":"conversation","needs_clarification":false,"missing":[],"confidence":0.98}
Detect meta-questions by SEMANTICS, not by keyword. Any phrasing in any language that interrogates the assistant's nature qualifies.

Step 1. Intent.
- "conversation": greetings, small talk, thanks, acknowledgements, emotional reactions, single factual questions answerable in one short paragraph, follow-up questions about something already said, opinion questions, and ALL meta-questions (see Step 0).
- "task": the user explicitly asks the assistant to PRODUCE a multi-step deliverable — build, design, plan, implement, write a document or code, analyze a dataset, research a topic in depth, refactor, teach a multi-step procedure, or otherwise hand back a structured artifact that a reasonable person would review before shipping.
- "manage-models": the user is asking the ASSISTANT ITSELF to operate its own model registry — list/show/browse registered LLMs, register a new model, toggle a model on/off, change the product default model, review a model's license (approve/block), or delete a model. Seed utterances in any language: "list my models", "lista mis modelos", "registra un modelo nuevo", "register a model", "bloquea gpt-5", "block gpt-5", "cámbiame el modelo por defecto", "set default to claude opus", "quita/elimina ese modelo". When intent == "manage-models", emit:
    manage_kind ∈ {list, register, toggle, set-default, review-license, delete}
    manage_args: extract a registry_id (or its free-form candidate, e.g. "gpt-5", "claude opus 4.6") when the user named one; empty object otherwise.
  Use "list" as a safe fallback when the sub-kind is genuinely ambiguous. Model-management intents ALWAYS set needs_clarification=false and missing=[] (Step 4 consistency still applies). confidence is your usual self-score.
- When ambiguous, prefer "conversation". The task pipeline is expensive, adds a human-review gate, and produces poor output on under-specified inputs; a conversational reply can always offer to escalate. Misrouting a one-shot reply into the planner is a much worse failure than the reverse.
- Do NOT classify a message as "task" merely because you don't immediately know the answer, or merely because the message contains an imperative verb. Imperatives are common in casual speech ("tell me...", "say...", "count...", "repeat...", "define...", "translate this phrase...", "give me an example...", "try streaming"). The grammatical mood is NOT the signal.
- The real test is the EXPECTED OUTPUT SHAPE. Ask: "Could a competent assistant satisfy this in a single short turn of free-form text, with no planning, no structure, and nothing a human would want to review before it ships?" If yes → "conversation". If the user is asking for something a senior practitioner would outline, draft in sections, or iterate on → "task".
- Requests framed as probes, demos, warm-ups, or curiosity checks ("test X", "show me what you can do", "try Y") are conversation even when phrased as commands — they want a quick live reply, not a deliverable.
- "I don't know" is a valid conversational reply.

Step 2. If intent == "task", judge specification-completeness.
A task is FULLY SPECIFIED if a senior practitioner in the relevant field could produce a concrete, non-generic plan without making more than ONE significant assumption about an unnamed parameter. Otherwise it is UNDER-SPECIFIED.

Step 3. Gate clarification tightly. Set needs_clarification = true ONLY IF ALL of:
  (a) intent == "task", AND
  (b) the task cannot be meaningfully started without a missing field, AND
  (c) the missing field has no sensible default a senior practitioner would pick, AND
  (d) you can name 1..4 concrete missing fields in snake_case (snake_case is required for the field NAMES — never translate the field names; the values describe abstract dimensions like "audience", "deadline", "format", "scope", "constraints").
If any of (a)-(d) fails, set needs_clarification = false and missing = []. Over-asking is a failure mode; default to answering.

Step 4. Field consistency rules (HARD):
- intent == "conversation"     ⇒ needs_clarification = false, missing = [].
- intent == "manage-models"    ⇒ needs_clarification = false, missing = [], manage_kind is REQUIRED.
- needs_clarification == true  ⇒ intent MUST be "task" AND missing MUST have 1..4 entries.
- needs_clarification == false ⇒ missing MUST be [].

Step 5. Confidence (float in [0.0, 1.0]).
Your certainty about BOTH the intent choice AND (for tasks) the specification judgment. NOT your confidence you could answer the user. Calibration: 0.99 = certain, 0.85 = clearly right, 0.7 = more likely right than not, 0.5 = coin flip. Be honest; do not inflate.

Output contract: respond with ONLY the JSON object. The first character of your response MUST be "{" and the last character MUST be "}". No preamble, no code fences, no trailing text.`)
	tClassify.LLMConfig = &cpn.LLMConfig{
		// REQ-CFG-003/004: Role carries routing intent; Model is filled in
		// by applyUserModelPreferences at session-resolve time.
		Role:         "classifier",
		MaxTokens:    128,
		Temperature:  0.0,
		RequireJSON:  true,
		StreamOutput: false,
		SkipHistory:  true,
	}

	// t-direct: fires for conversation intent — direct streaming response.
	tDirect := cpn.NewTransition("t-direct", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-output"})
	tDirect.SystemPrompt = envOr("PROMPT_DIRECT", braeIdentity+`
Respond naturally and concisely. Be warm but engineer-minded — direct, practical, no fluff.

`+langRule)
	tDirect.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}
	tDirect.Guard = guardDirectConversation
	tDirect.LLMTools = []string{"bash_exec", "file_read", "file_write", "register_tool"}

	planSharedRules := `Hard rules:
- DO NOT ask clarifying questions to the user. Do not end with a question that requests more input.
- If something is genuinely unspecified, make ONE reasonable assumption per gap and state them at the top under an "Assumptions" header (translated into the user's language). Max 3 bullets. Then proceed.
- Format the body of the plan as a numbered list of concrete steps.
- End with exactly the phrase that means "Would you like me to proceed?" in the user's language. Use the natural form a native speaker would use.

` + langRule + `

Audience disambiguation rule (CRITICAL):
A plan step may include questions intended for THIRD PARTIES the user must contact (e.g. survey questions for preview users, interview questions for stakeholders, screening questions for candidates). When a step contains such questions, you MUST label that block with a heading translated into the user's language meaning "Questions to send to [the third party]". Place the third-party questions as a sub-list under that heading. Never mix them inline with the step text. The user is approving a plan, not answering more questions — every question mark in your output must be unambiguously addressed to a third party (under a labeled block) or be the final "would you like me to proceed?" sentence.`

	planDirectSystemPrompt := envOr("PROMPT_PLAN_DIRECT", braeIdentity+`
You are producing an actionable plan.

The user's request (in conversation history) has already been judged fully specified by an upstream classifier. Your job is to deliver the plan, not to gather more information.

`+planSharedRules)

	planClarifiedSystemPrompt := envOr("PROMPT_PLAN_CLARIFIED", braeIdentity+`
You are producing an actionable plan.

The user's original request is in the conversation history. The CURRENT user message contains their answers to a clarification questionnaire you previously asked. You now have enough information — your job is to deliver the plan, not to gather more.

`+planSharedRules)

	planLLMConfig := func() *cpn.LLMConfig {
		return &cpn.LLMConfig{
			MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),
			Temperature:  0.7,
			StreamOutput: true,
		}
	}

	// t-plan-direct: fires when a task is fully-specified (no clarification needed).
	tPlanDirect := cpn.NewTransition("t-plan-direct", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-plan"})
	tPlanDirect.SystemPrompt = planDirectSystemPrompt
	tPlanDirect.LLMConfig = planLLMConfig()
	tPlanDirect.Guard = guardPlanTaskDirect

	// t-plan-clarified: fires AFTER t-reassess has decided the clarification
	// loop can close. Consumes a ColorString preamble token from
	// p-planner-input (produced by t-preplanner) so the preamble text —
	// composed by buildPlannerPreamble — reaches the planner LLM as a
	// user message (fireLLM's ColorJSON filter would otherwise suppress
	// it; see cpn/fire_llm.go user-message gate). The preamble carries
	// the explicit "do not ask again" / "budget exhausted" / "user chose
	// to proceed" directive mandated by REQ-031/032 §4.5.
	tPlanClarified := cpn.NewTransition("t-plan-clarified", cpn.NodeKindLLM,
		[]string{"p-planner-input"}, []string{"p-plan"})
	tPlanClarified.SystemPrompt = planClarifiedSystemPrompt
	tPlanClarified.LLMConfig = planLLMConfig()
	// Guard stays unconditional here — gating happens upstream in
	// t-preplanner via guardResidualResolved (REQ-011). By the time a
	// token is sitting on p-planner-input, the loop is closed.
	tPlanClarified.Guard = guardPlanTaskClarified

	// t-ask: when classifier flags missing info, generate a small JSON
	// questionnaire using the missing[] list as hints. Output is consumed
	// by t-clarify (HITL).
	//
	// REQ-007 (pragmatic interpretation): t-ask does NOT consume p-round.
	// fireLLM deposits its single output token into ALL OutputPlaces
	// (cpn/fire_llm.go Step-5), so an "output to p-questions + p-round"
	// arc would double-write the questionnaire JSON into p-round and
	// destroy the counter. Instead we leave p-round untouched — the
	// counter was seeded to {n:0} in the initial marking and sits ready
	// for t-reassess to consume on the FIRST reassess pass. The CPN's
	// 1-bounded invariant is preserved by never depositing into p-round
	// while it is already marked (CON-004).
	tAsk := cpn.NewTransition("t-ask", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-questions"})
	tAsk.SystemPrompt = envOr("PROMPT_ASK", `You produce a REFRAME-THEN-ASK clarification artifact. Before asking anything, you prove you understood the user by restating their goal and listing the assumptions you would make. Then — and only then — you ask the few questions whose answers would change the plan.

You will receive the FULL conversation history. The user's most recent message is the one you must clarify. The upstream classifier may attach a JSON blob with a "missing" array — treat it ONLY as a weak hint. The user's actual words are the source of truth.

Output: JSON only, matching this EXACT schema:
{
  "restated_goal": "One sentence, in the user's language, naming the concrete subject and outcome the user asked for. Use the user's own nouns.",
  "assumptions": ["1 to 3 working assumptions you would make if the user said nothing more. Each assumption is concrete and falsifiable."],
  "questions": [
    {
      "id": "q1",
      "prompt": "The question, under 100 chars, ending with ?",
      "quote_from_user": "A short phrase lifted VERBATIM from the user's most recent message that this question is anchored to.",
      "why_it_matters": "One sentence explaining how a different answer would change the plan.",
      "recommended": "opt-a",
      "options": [
        {"id":"opt-a","label":"..."},
        {"id":"opt-b","label":"..."}
      ]
    }
  ]
}

═══ CORE RULE: GROUNDING (mechanical) ═══
The "quote_from_user" field is a HARD CONSTRAINT, not decoration. Every question MUST carry a quote_from_user that appears LITERALLY in the user's most recent message (case-insensitive substring match). If you cannot find a literal phrase to quote, you cannot ask the question — drop it. This single rule eliminates generic questionnaires.

═══ STRUCTURE ILLUSTRATION (language-neutral) ═══
The schema below uses placeholder tokens — NOT literal text to copy. Substitute every <PLACEHOLDER> with content written in the SAME language as the user's most recent message. Do not anchor to the placeholder language; do not default to any specific language.

{
  "restated_goal": "<one sentence in user's language using user's own nouns>",
  "assumptions": [
    "<assumption 1 — concrete and falsifiable>",
    "<assumption 2>",
    "<assumption 3 — optional>"
  ],
  "questions": [
    {
      "id": "q1",
      "prompt": "<question in user's language, ends with ?>",
      "quote_from_user": "<verbatim substring lifted from user's most recent message>",
      "why_it_matters": "<one sentence: how a different answer changes the plan>",
      "recommended": "opt-a",
      "options": [
        {"id":"opt-a","label":"<concrete candidate answer 1>"},
        {"id":"opt-b","label":"<concrete candidate answer 2>"},
        {"id":"opt-c","label":"<concrete candidate answer 3 — optional>"},
        {"id":"opt-d","label":"<concrete candidate answer 4 — optional>"}
      ]
    }
  ]
}

The <PLACEHOLDER> markers above are illustrative only. Your output must contain ZERO angle brackets and ZERO placeholder strings — only real, grounded content in the user's language.

═══ HARD LIMITS ═══
- restated_goal: REQUIRED. One sentence. Must use the user's own nouns.
- assumptions: 1 to 3 items. Each must be falsifiable ("X is Y") not a platitude ("we want quality").
- questions: 0 to 3 items. CALIBRATION RULE: pick the count by request shape, NOT by minimization:
    • Strategic / multi-step requests (launches, campaigns, projects with audience+channel+timing+success): aim for 2-3 questions. One question is almost always too few here — it leaves the planner guessing on the dimensions you didn't ask about.
    • Single-deliverable requests (write a poem, summarize this, fix this snippet): 0-1 questions. The deliverable shape is usually obvious from the request.
    • If you would have written 1 question for a strategic request, pause and ask: "what's the SECOND decision the user must make that would change the plan?" — usually there is one, and it's load-bearing.
  Only emit "questions": [] when the assumptions truly cover every load-bearing decision.
- Each question.quote_from_user MUST be a literal substring of the user's most recent message. No paraphrasing, no translation.
- Each question.why_it_matters MUST describe how the plan branches on the answer. Vague justifications like "to understand better" are forbidden.
- Options: 2-4 per question. ids opt-a, opt-b, opt-c, opt-d. Each option ≤70 chars, mutually distinct, never ends with "?".
- recommended: id of the option that best fits context. Never random.
- Do NOT include any top-level keys other than restated_goal, assumptions, questions.
- Do NOT ask about anything the user already specified. Re-read before each question.

`+langRule+`

═══ ANTI-PATTERNS (forbidden) ═══
- Generic "what is the goal / purpose / audience?" questions when the user already implied them.
- Template-filler options (abstract category labels not grounded in the request).
- Reusing the same questionnaire shape for different requests.
- Restating the goal in your OWN words that drop the user's nouns.
- quote_from_user that paraphrases instead of quoting verbatim.

═══ OUTPUT FORMAT (CRITICAL) ═══
Your ENTIRE response must be the raw JSON object and NOTHING ELSE. No greeting, no preamble, no markdown code fences, no trailing commentary. The very first character of your response MUST be "{" and the very last character MUST be "}". Any text before "{" or after "}" will break the parser.`)
	tAsk.LLMConfig = &cpn.LLMConfig{
		// REQ-CFG-003/004: Role drives per-role override lookup in
		// applyUserModelPreferences. Model is written by the resolver.
		Role:        "structured",
		MaxTokens:   envInt("MAX_TOKENS_ASK", 1024),
		Temperature: 0.3,
		RequireJSON: true,
		// REQ-PAR-004: verify JSON-ness of the response and retry once with
		// a tighter directive if the model emits prose or reasoning tokens.
		ResponseFmtRequired: true,
		StreamOutput:        false,
		// Dual-flag configuration (REQ-104, spec-process-bugfix-a2ui-rehydration-completion.md):
		//   SkipHistory:       false → t-ask MUST read the user's message from
		//                              c.History on the input side. Its consumed
		//                              p-classified token is metadata-only, so
		//                              flipping this true would blind the LLM to
		//                              the user's words (v1.1 regression).
		//   SkipOutputHistory: true  → t-ask's raw JSON questionnaire is routing
		//                              metadata consumed downstream by
		//                              t-clarify's A2UIPayloadBuilder via the
		//                              token pipeline, never via c.History.
		//                              Persisting it pollutes the transcript and
		//                              renders as a raw JSON bubble above the
		//                              $$a2ui: surface on rehydration (INV-101).
		SkipHistory:       false,
		SkipOutputHistory: true,
		// JSON questionnaire generator: schema is rigid and the regional
		// register adds no signal — opt out to save tokens (CON-004).
		SkipRegionalPreamble: true,
	}
	tAsk.Guard = guardNeedsClarification

	// t-clarify: HITL transition that publishes the questionnaire as an A2UI
	// surface and waits for the user's structured "submit" response. On
	// resolve it merges the answers into the classifier JSON and deposits a
	// ColorJSON token into p-clarified.
	//
	// REQ-100: buildClarifyA2UIPayload now reads an optional p-round token
	// from consumed to stamp the "Follow-up n/k" badge on the envelope.
	// The builder also sniffs the p-questions payload to pick between
	// standard-questionnaire and escape-hatch-card rendering (PAT-002).
	tClarify := cpn.NewTransition("t-clarify", cpn.NodeKindHITL,
		[]string{"p-questions"}, []string{"p-clarified"})
	tClarify.HITLConfig = &cpn.HITLConfig{
		Prompt:             "Answer the questionnaire to clarify your request.",
		RevisionLoop:       false,
		A2UIPayloadBuilder: buildClarifyA2UIPayload,
		OutputBuilder:      buildClarifiedToken,
	}

	// t-reassess: post-clarify LLM that scores residual ambiguity and
	// decides whether to (a) re-fire t-clarify with a deeper questionnaire,
	// (b) proceed to the planner with stated assumptions, or (c) emit a
	// binary escape-hatch surface (REQ-004, §4.2).
	//
	// Consumes: p-clarified. The round counter (p-round) is read by
	// downstream guards/handlers (t-followup / t-preplanner), not by this
	// transition — fireLLM deposits its single output token into every
	// OutputPlace, which would clobber p-round if it appeared here.
	tReassess := cpn.NewTransition("t-reassess", cpn.NodeKindLLM,
		[]string{"p-clarified"}, []string{"p-reassessed"})
	tReassess.SystemPrompt = envOr("PROMPT_REASSESS", braeIdentity+`
You are the clarification-loop reassessor for a LATAM-entrepreneurship planning assistant. On every user-answered clarification round, you score residual ambiguity and decide whether one more question would measurably improve the plan.

Seven ambiguity axes you recognize (LATAM entrepreneurship domain):
event_type, stage, audience, budget_band, geography, timeline, success_metric.

═══ CORE RULES ═══

1. DRILL DEEPER, NEVER WIDER. Next-round questions MUST target `+"`missing_dimensions`"+` and MUST NOT reuse any question id from round_history, nor re-ask anything the user already stated. Drop the question before repeating.

2. ROUND-AWARE DISCIPLINE. At round == max_rounds - 1, bias toward `+"`decision: proceed_to_plan`"+` unless a plan-invalidating axis is still missing. Users prefer a plan with stated assumptions over a third questionnaire.

3. QUOTE GROUNDING (round 2+). Every `+"`next_questions.questions[*].quote_from_user`"+` MUST be a literal substring of the user's original message OR a prior user turn in the conversation. If you cannot ground a follow-up question, drop it and lower residual_ambiguity accordingly.

4. FRUSTRATION DETECTION. If the latest answer contains any of: "whatever", "lo que sea", "just pick", "tú decide", "no sé", "I don't know", "da igual", "you choose", "any of them", "doesn't matter" (case-insensitive, in any language) — set `+"`frustration_signal: true`"+` and `+"`decision: request_user_choice`"+` with a two-option user_choice (drill / proceed).

5. CONTRADICTION DETECTION. If the latest answer contradicts a prior answer or the original message, set `+"`contradiction_detected: true`"+`, make convergence_delta negative, and set `+"`decision: request_user_choice`"+` with BOTH contradictory values as equal-weight options.

═══ OUTPUT SCHEMA (strict JSON — first char MUST be "{" and last char MUST be "}") ═══

{
  "residual_ambiguity": 0.0,
  "convergence_delta": 0.0,
  "missing_dimensions": ["..."],
  "resolved_dimensions": ["..."],
  "contradiction_detected": false,
  "frustration_signal": false,
  "topic_shift": false,
  "decision": "clarify_again" | "proceed_to_plan" | "request_user_choice",
  "rationale": "one sentence, telemetry only, never user-facing",

  // Required iff decision == "clarify_again":
  "next_questions": {
    "restated_goal": "...",
    "assumptions": ["..."],
    "questions": [
      {"id":"qN","prompt":"...","quote_from_user":"...","why_it_matters":"...","recommended":"opt-a",
       "options":[{"id":"opt-a","label":"..."},{"id":"opt-b","label":"..."}]}
    ]
  },

  // Required iff decision == "proceed_to_plan":
  "stated_assumptions": ["..."],

  // Required iff decision == "request_user_choice":
  "user_choice": {
    "prompt": "...",
    "options": [{"id":"drill|<opt>","label":"..."},{"id":"proceed|<opt>","label":"..."}]
  }
}

OMIT conditional keys that do not apply to the chosen decision — never set them to null.

`+langRule)
	tReassess.LLMConfig = &cpn.LLMConfig{
		Role:                 "structured",
		MaxTokens:            envInt("MAX_TOKENS_REASSESS", 1024),
		Temperature:          0.2,
		RequireJSON:          true,
		ResponseFmtRequired:  true,
		StreamOutput:         false,
		SkipHistory:          false,
		SkipOutputHistory:    true,
		SkipRegionalPreamble: true,
	}

	// t-followup: deterministic NodeKindTool — consumes (p-reassessed,
	// p-round), deposits (p-questions, p-round') with the counter
	// incremented, reset (on topic_shift), or held (on
	// request_user_choice). No LLM call (REQ-006).
	tFollowup := cpn.NewTransition("t-followup", cpn.NodeKindTool,
		[]string{"p-reassessed", "p-round"},
		[]string{"p-questions", "p-round"})
	tFollowup.Guard = guardResidualAmbiguous
	tFollowup.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		ptrs := tokensByValue(consumed)
		rr, ok := parseReassessResult(ptrs)
		if !ok {
			return nil, fmt.Errorf("t-followup: unparseable reassess token")
		}
		rt, ok := parseRoundToken(ptrs)
		if !ok {
			// Defensive: if the round token is missing, start fresh.
			rt = roundToken{}
		}
		return buildFollowupDeposits(rr, rt)
	}

	// t-preplanner: deterministic NodeKindTool — consumes (p-reassessed,
	// p-round), deposits a ColorString preamble token to p-planner-input
	// using buildPlannerPreamble. This is the loop-exit branch: the round
	// counter dies here (no re-emission), preserving REQ-008's
	// counter-dies-in-terminal-branches guarantee.
	tPreplanner := cpn.NewTransition("t-preplanner", cpn.NodeKindTool,
		[]string{"p-reassessed", "p-round"},
		[]string{"p-planner-input"})
	tPreplanner.Guard = guardResidualResolved
	tPreplanner.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		ptrs := tokensByValue(consumed)
		rr, _ := parseReassessResult(ptrs) // fail-open: zero-value triggers AC-007 preamble
		rt, _ := parseRoundToken(ptrs)
		cfg := loadReassessConfig()
		preamble := buildPlannerPreamble(rr, rt, cfg)
		// Append the last user/clarification content from consumed as
		// context so the planner sees the user's answers alongside the
		// preamble. In practice c.History already carries the
		// t-clarify OutputBuilder's "User answers follow." token, so
		// we just emit the preamble — the planner reads c.History for
		// the rest.
		return map[string]cpn.Token{
			"p-planner-input": {Color: cpn.ColorString, Payload: preamble},
		}, nil
	}

	// t-review: HITL gate — waits for user approval. The prompt text is
	// intentionally empty: the A2UI Review Required card already labels
	// itself, and a duplicate plain-text bubble was visual noise.
	//
	// REQ-BE-001/002: t-review owns its A2UI surface via a transition-scoped
	// A2UIPayloadBuilder so the surface row flows through c.History and the
	// paired response row is persisted by fireHITL's raw-deposit branch.
	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt:             "",
		A2UIPayloadBuilder: buildReviewA2UIPayload("t-review"),
	}

	// t-execute: fires after approval. The plan above may be either:
	//   (a) PRODUCIBLE — the LLM can deliver the artifact right now (write
	//       code, draft an email, summarize a document, generate a list).
	//   (b) ADVISORY — the steps require the human to act in the real world
	//       (launch a product, contact people, configure infrastructure).
	// The previous prompt assumed (a) universally and the model went meta
	// when handed (b). The new prompt makes the model classify the plan and
	// behave correctly in each case.
	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", braeIdentity+`
The user just approved the plan shown above. Decide whether the plan is PRODUCIBLE by you right now, or ADVISORY (requires the human to take real-world actions you cannot perform).

DECISION RULE:
- PRODUCIBLE = every step is something a language model can deliver as text in this reply (code, drafts, summaries, translations, calculations, structured data, copy).
- ADVISORY = at least one step requires the human to do something in the world you cannot do (contact people, deploy infrastructure, hold meetings, ship products, take photos, sign documents, run physical experiments).

═══ IF PRODUCIBLE ═══
Produce the actual deliverable now. Do NOT restate the plan, do NOT ask permission, do NOT offer options. Just deliver. Use markdown for structure.

═══ IF ADVISORY ═══
You CANNOT execute the plan — and pretending you can is the failure mode that frustrates the user. Instead, do exactly this:

1. Acknowledge the approval in ONE short sentence (a brief affirmation that you are ready to move forward).
2. Identify the FIRST concrete sub-task in the plan that you CAN help with as a deliverable — usually drafting copy, designing a form, writing an email, building a checklist, generating a template, producing a script, etc. Name it explicitly using the user's own words for the artifact.
3. Offer to produce that ONE deliverable right now. Frame it as a specific named artifact, not as a vague offer of help. Phrase it as a single yes/no question.
4. Stop. Do not list multiple options. Do not ask the user to "choose between" anything. One concrete offer, one yes/no answer expected.

NEVER tell the user "you decide if you want to proceed" or any equivalent that bounces the decision back — they already approved the plan, that question has been answered.

`+langRule)
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),
		Temperature:  0.5,
		StreamOutput: true,
	}
	tExecute.LLMTools = []string{"bash_exec", "file_read", "file_write", "register_tool"}

	// System tool transitions — REQ-007/REQ-010. NodeKindTool with empty
	// InputPlaces/OutputPlaces: fireLLM dispatches to Executor inline via the
	// tool-call loop; no CPN arc wiring needed. Executors are injected by
	// tools.Registry.InjectIntoCPN at session creation time.
	tBashExec := cpn.NewTransition("bash_exec", cpn.NodeKindTool, []string{}, []string{})
	tBashExec.ToolName = "bash_exec"

	tFileRead := cpn.NewTransition("file_read", cpn.NodeKindTool, []string{}, []string{})
	tFileRead.ToolName = "file_read"

	tFileWrite := cpn.NewTransition("file_write", cpn.NodeKindTool, []string{}, []string{})
	tFileWrite.ToolName = "file_write"

	tRegisterTool := cpn.NewTransition("register_tool", cpn.NodeKindTool, []string{}, []string{})
	tRegisterTool.ToolName = "register_tool"

	// JIT sub-CPN lane (plan i-need-you-make-playful-dongarra.md).
	// Phase 1 wired the places + synthesize/instantiate transitions; Phase 2
	// routes task-intent tokens whose classifier flagged composition_hint
	// into t-compose-spec, which translates the conversation into a
	// ColorTaskSpec JSON the synthesizer can consume.
	tComposeSpec := cpn.NewTransition("t-compose-spec", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-synth-request"})
	tComposeSpec.SystemPrompt = envOr("PROMPT_COMPOSE_SPEC", braeIdentity+`
You are the sub-CPN composer for brae. The user's latest request has been classified as a task whose ideal execution is a composed Colored Petri Net — parallel tool fan-out with a consolidated output.

Your ONLY output is a TaskSpec JSON object. No prose. No code fences.

Schema (all keys required except where noted):
{
  "intent": "string — one sentence restating what the sub-CPN should accomplish, in the user's language",
  "tools_needed": ["<tool qualified name>", ...],
  "inputs": [ {"name":"<field>","value":<literal-or-null>} , ...],
  "expected_output": {"color":"JSON","shape":"string describing the consolidated object"},
  "parallelism_hint": "fanout" | "sequence" | "auto",
  "budget": {"max_places": 20, "max_transitions": 20, "timeout_ms": 30000}
}

Rules:
- Choose "parallelism_hint":"fanout" when the user explicitly asks for concurrent execution.
- "tools_needed" MUST reference qualified tool names from the host's toolbox catalogue; pass through bare names ("bash_exec") only when the tool has no namespace.
- "expected_output.shape" must describe the consolidated JSON keys the user will see.
- Keep "budget" modest — the synthesizer will reject oversized topologies.
- If you cannot produce a defensible TaskSpec, emit {"intent":"fallback","tools_needed":[],"inputs":[],"expected_output":{"color":"JSON","shape":"empty"},"parallelism_hint":"auto","budget":{"max_places":5,"max_transitions":5,"timeout_ms":10000}} so the downstream error handler routes to a normal reply.

`+langRule)
	tComposeSpec.LLMConfig = &cpn.LLMConfig{
		Role:                 "structured",
		MaxTokens:            envInt("MAX_TOKENS_COMPOSE_SPEC", 1024),
		Temperature:          0.2,
		RequireJSON:          true,
		ResponseFmtRequired:  true,
		StreamOutput:         false,
		SkipHistory:          false,
		SkipOutputHistory:    true,
		SkipRegionalPreamble: true,
	}
	tComposeSpec.Guard = guardCompositionHint

	tSynthesize := cpn.NewTransition("t-synthesize", cpn.NodeKindSynthesize,
		[]string{"p-synth-request"}, []string{"p-instantiate-request"})
	tSynthesize.SynthesizeConfig = &cpn.SynthesizeConfig{
		MaxTokens:      envInt("MAX_TOKENS_SYNTHESIZE", 4096),
		Temperature:    0.2,
		MaxCorrections: 2,
		SizeCap:        cpn.DefaultSizeCap(),
	}

	tInstantiate := cpn.NewTransition("t-instantiate", cpn.NodeKindInstantiate,
		[]string{"p-instantiate-request"}, []string{"p-subnet-output"})
	tInstantiate.InstantiateConfig = &cpn.InstantiateConfig{}

	// Phase 4 continuation: once the sub-CPN terminates and its aggregated
	// result lands on p-subnet-output, an LLM transition presents the
	// structured result to the user as a natural-language reply. Without
	// this hop the subnet's output would sit on a terminal place forever
	// and the user would never see it.
	tSubnetReply := cpn.NewTransition("t-subnet-reply", cpn.NodeKindLLM,
		[]string{"p-subnet-output"}, []string{"p-output"})
	tSubnetReply.SystemPrompt = envOr("PROMPT_SUBNET_REPLY", braeIdentity+`
A sub-CPN you just composed and executed produced a structured result (visible to you as the most recent user message — even though it is system-generated). Your job is to present the result as a clear, concise reply to the original user request.

Rules:
- Do NOT mention CPNs, sub-nets, topologies, or any internal machinery. The user should experience this as a natural answer.
- Keep the reply tight. If the payload is small JSON, inline it in a code block. If it is larger, summarize the key findings and offer to show more on request.
- Match the user's language (see the language rule below).
- Preserve meaningful data fidelity. Do NOT paraphrase numeric values or hostnames.

`+langRule)
	tSubnetReply.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_SUBNET_REPLY", 2048),
		Temperature:  0.5,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-classify":       tClassify,
		"t-direct":         tDirect,
		"t-ask":            tAsk,
		"t-clarify":        tClarify,
		"t-reassess":       tReassess,
		"t-followup":       tFollowup,
		"t-preplanner":     tPreplanner,
		"t-plan-direct":    tPlanDirect,
		"t-plan-clarified": tPlanClarified,
		"t-review":         tReview,
		"t-execute":        tExecute,
		// System tool transitions (REQ-007): keyed by ToolName so that
		// fireLLM's c.Transitions[tc.ToolName] lookup succeeds.
		"bash_exec":     tBashExec,
		"file_read":     tFileRead,
		"file_write":    tFileWrite,
		"register_tool": tRegisterTool,
		// JIT sub-CPN lane — see plan Phases 1-4.
		"t-compose-spec": tComposeSpec,
		"t-synthesize":   tSynthesize,
		"t-instantiate":  tInstantiate,
		"t-subnet-reply": tSubnetReply,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	c.SeedFunc = seedPRound
	seedPRound(c)
	return c
}

// ── Review helpers ──────────────────────────────────────────────────────────

// buildReviewA2UIPayload returns an A2UIPayloadBuilder for the t-review HITL
// transition. The returned builder ignores `consumed` (the review card content
// is fixed) and produces the same typed component tree the legacy
// cmd/server/main.go buildHITLReviewCard historically emitted, so live and
// rehydrated renders converge byte-identically.
//
// The transitionID is captured by closure so each button's `id` prop points
// at the correct transition.
//
// See spec-process-bugfix-treview-surface-and-locked-parser.md REQ-BE-001/002.
func buildReviewA2UIPayload(transitionID string) func([]cpn.Token) (any, error) {
	return func(_ []cpn.Token) (any, error) {
		return reviewCardPayload(transitionID), nil
	}
}

// reviewCardPayload returns the typed component tree for a HITL review card.
// The shape MUST match what cmd/server/main.go buildHITLReviewCard
// historically produced — single source of truth. The legacy emission path in
// main.go is a WARN-and-emit defensive fallback (PAT-003) and also consumes
// this helper so a misconfigured topology still ships the correct card shape.
func reviewCardPayload(transitionID string) any {
	return map[string]any{
		"components": []any{
			map[string]any{
				"type":  "card",
				"props": map[string]any{"title": "Review Required"},
				"children": []any{
					map[string]any{"type": "text", "props": map[string]any{"content": "Please review and confirm."}},
					map[string]any{"type": "divider"},
					map[string]any{"type": "text", "props": map[string]any{"content": "Choose an action to continue:", "variant": "secondary"}},
				},
			},
			map[string]any{"type": "button", "props": map[string]any{
				"label": "✓ Approve", "variant": "success",
				"actionType": "hitl:approve", "id": transitionID,
			}},
			map[string]any{"type": "button", "props": map[string]any{
				"label": "✎ Request Changes", "variant": "primary",
				"actionType": "hitl:revise", "id": transitionID,
			}},
			map[string]any{"type": "button", "props": map[string]any{
				"label": "✗ Discard", "variant": "danger",
				"actionType": "hitl:reject", "id": transitionID,
			}},
		},
	}
}

// ── Clarification helpers ───────────────────────────────────────────────────

// questionnaireSpec mirrors the JSON shape produced by t-ask. Only the fields
// we need for A2UI rendering and answer-merging are decoded.
//
// Schema (reframe-then-ask, 2026):
//
//	{
//	  "restated_goal": "...",       // model's one-sentence interpretation
//	  "assumptions":   ["...", ...],// 1-3 working assumptions
//	  "questions":     [{...}, ...] // 0-3 grounded questions
//	}
type questionnaireSpec struct {
	RestatedGoal string                  `json:"restated_goal,omitempty"`
	Assumptions  []string                `json:"assumptions,omitempty"`
	Questions    []questionnaireQuestion `json:"questions"`
}

type questionnaireQuestion struct {
	ID            string                `json:"id"`
	Prompt        string                `json:"prompt"`
	QuoteFromUser string                `json:"quote_from_user,omitempty"`
	WhyItMatters  string                `json:"why_it_matters,omitempty"`
	Recommended   string                `json:"recommended,omitempty"`
	Options       []questionnaireOption `json:"options"`
}

type questionnaireOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// extractJSONObject pulls a usable JSON object out of a string that may be
// wrapped in chatty prose, reasoning-mode <think>…</think> preambles, or
// fenced code blocks. Models asked for JSON-only sometimes prepend
// "Claro, aquí tienes:" or similar; Gemma 4 31B emits <think> blocks
// containing brace-balanced but semantically garbage pseudo-JSON. This
// function's tolerance is the regression hotspot — it MUST survive every
// shape in the parser test matrix.
//
// Extraction pipeline (REQ-PAR-001):
//  1. Strip every <think>…</think> block (case-insensitive, dot matches
//     newlines, non-greedy).
//  2. If a ```json fence is present with a balanced object inside, prefer
//     the fenced content.
//  3. Scan forward through the remaining string collecting every balanced
//     {…} object. Return the FIRST object that decodes into a usable
//     questionnaireSpec (non-empty Questions OR Assumptions OR RestatedGoal).
//     If none is usable, return the first syntactically balanced object so
//     the caller's existing "no JSON" branch still fires for truly empty
//     input.
//
// The inner brace-balancer loop (quote-aware, backslash-aware) is preserved
// verbatim — it was never the bug; the extractor's single-shot scan was.
func extractJSONObject(s string) string {
	// (1) Strip reasoning-mode preambles that frequently contain
	// brace-balanced junk. This MUST happen before the scan so the junk
	// can never win the first-candidate race.
	s = thinkBlockRe.ReplaceAllString(s, "")

	// (2) Prefer the content of a ```json fence. Go regex is greedy-safe
	// under (?s); the non-greedy inside captures the shortest balanced
	// object, which matches Gemma's observed shape.
	if m := fencedJSONRe.FindStringSubmatch(s); len(m) == 2 {
		// The fence's inner content is already a candidate object; fall
		// through to the scan-forward loop below operating on that slice,
		// so we still get schema-aware preference within the fence.
		s = m[1]
	}

	// (3) Scan forward, collecting balanced candidates.
	var firstSyntactic string
	cursor := 0
	for cursor < len(s) {
		start := strings.IndexByte(s[cursor:], '{')
		if start < 0 {
			break
		}
		start += cursor

		// ── Quote-aware brace balancer (inner loop preserved) ─────────
		depth := 0
		inString := false
		escaped := false
		end := -1
		for i := start; i < len(s); i++ {
			c := s[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == '"' {
					inString = false
				}
				continue
			}
			switch c {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			// Unterminated: the LLM response was cut off mid-object (we've
			// seen Gemini-2.5-flash truncate a long `restated_goal` string
			// before the closing quote, leaving
			//   {"restated_goal":"El usuario quiere que, asumiendo la pe
			// which otherwise fails the caller with "no JSON object found"
			// and surfaces as "Your selected model did not return a valid
			// questionnaire" in the UI. Try a conservative repair: close
			// any open string, then append the missing ] / } in the right
			// order. If the repaired payload decodes into a usable spec,
			// use it; otherwise fall through to firstSyntactic.
			if repaired, ok := repairTruncatedJSONObject(s[start:]); ok {
				var spec questionnaireSpec
				if err := json.Unmarshal([]byte(repaired), &spec); err == nil {
					if spec.RestatedGoal != "" || len(spec.Assumptions) > 0 || len(spec.Questions) > 0 {
						return repaired
					}
				}
				// Repaired but empty — drop it. We deliberately do NOT
				// set firstSyntactic here: an empty `{}` would look like
				// a "valid but useless" object to the caller and suppress
				// the more specific "no JSON object found" error.
			}
			// Nothing further in s can be balanced — bail.
			break
		}

		candidate := s[start : end+1]
		if firstSyntactic == "" {
			firstSyntactic = candidate
		}

		// Schema-aware preference: accept the first candidate that decodes
		// into a usable questionnaireSpec. Stop scanning when one matches.
		var spec questionnaireSpec
		if err := json.Unmarshal([]byte(candidate), &spec); err == nil {
			if spec.RestatedGoal != "" || len(spec.Assumptions) > 0 || len(spec.Questions) > 0 {
				return candidate
			}
		}

		cursor = end + 1
	}

	// Nothing decoded into a usable spec. Return the first balanced
	// candidate if we found one — the caller's Unmarshal will surface
	// a more specific error than "no JSON found".
	return firstSyntactic
}

// repairTruncatedJSONObject takes a string that starts with '{' but whose
// object was never closed (LLM response truncated mid-token) and returns a
// best-effort balanced object with ok=true, or ("", false) when the input
// can't be salvaged.
//
// Strategy:
//  1. Walk the input maintaining a stack of open containers ('{' / '[') and
//     a "currently inside string" flag (respecting backslash-escapes).
//  2. When we hit end-of-input, close whatever is still open in reverse
//     order — first close an unterminated string with '"', then pop each
//     container with its matching closer.
//  3. Before appending the final '}', trim any trailing comma + whitespace
//     so `{"a":"b",` → `{"a":"b"}` instead of `{"a":"b",}` (invalid).
//  4. Also handle a dangling `"key":` where the value never arrived —
//     replace the trailing `"key":` with nothing (drop the half-field).
//
// This is intentionally conservative: it only closes unterminated shapes.
// It does not try to fix semantically broken JSON (wrong types, duplicate
// keys, etc.) — those cases still fall through to the caller's error path.
func repairTruncatedJSONObject(s string) (string, bool) {
	if len(s) == 0 || s[0] != '{' {
		return "", false
	}
	var stack []byte
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
		}
	}
	if len(stack) == 0 && !inString {
		// Input was already balanced — caller's main loop should've
		// taken this path. Nothing to repair.
		return s, true
	}

	var b strings.Builder
	b.Grow(len(s) + len(stack) + 1)
	b.WriteString(s)
	if inString {
		// If the unterminated string ends with a lone backslash, drop it
		// so we don't escape our closing quote.
		raw := b.String()
		if len(raw) > 0 && raw[len(raw)-1] == '\\' {
			b.Reset()
			b.WriteString(raw[:len(raw)-1])
		}
		b.WriteByte('"')
	}

	repaired := b.String()

	// Strip a dangling `"key":` or `"key": ` when we never received the
	// value. Detect by looking back past whitespace for a ':' with only
	// an identifier-like string behind it.
	repaired = dropDanglingObjectKey(repaired)

	// Pop containers in reverse, trimming trailing commas/whitespace
	// before each closer so `{"a":1,` → `{"a":1}`.
	for i := len(stack) - 1; i >= 0; i-- {
		repaired = strings.TrimRight(repaired, " \t\r\n")
		repaired = strings.TrimSuffix(repaired, ",")
		repaired += string(stack[i])
	}
	return repaired, true
}

// dropDanglingObjectKey trims a `"key":` (with optional whitespace) from the
// tail of s when no value has been written yet — the JSON would otherwise
// parse as `{"key":}` which is invalid. Leaves s untouched when the tail
// doesn't match that exact shape.
func dropDanglingObjectKey(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n")
	if !strings.HasSuffix(trimmed, ":") {
		return s
	}
	// Walk back past the ':' and any whitespace.
	i := len(trimmed) - 1 // ':'
	i--
	for i >= 0 && (trimmed[i] == ' ' || trimmed[i] == '\t') {
		i--
	}
	// Expect a closing quote of the key string.
	if i < 0 || trimmed[i] != '"' {
		return s
	}
	// Walk back to the opening quote (respecting backslash escapes).
	j := i - 1
	for j >= 0 {
		if trimmed[j] == '"' {
			// Count preceding backslashes to check escaping.
			bs := 0
			for k := j - 1; k >= 0 && trimmed[k] == '\\'; k-- {
				bs++
			}
			if bs%2 == 0 {
				break
			}
		}
		j--
	}
	if j < 0 {
		return s
	}
	// Walk back past any leading whitespace before the key. If the char
	// immediately before is '{' or ',' we have a clean "dangling key"
	// shape; otherwise leave s untouched (be conservative).
	k := j - 1
	for k >= 0 && (trimmed[k] == ' ' || trimmed[k] == '\t' || trimmed[k] == '\r' || trimmed[k] == '\n') {
		k--
	}
	if k < 0 || (trimmed[k] != '{' && trimmed[k] != ',') {
		return s
	}
	// If the preceding char was ',', strip it too so we don't leave a
	// trailing comma for the container-closer pass to handle.
	if trimmed[k] == ',' {
		return trimmed[:k]
	}
	// Leave the '{' in place; just drop the dangling key+colon.
	return trimmed[:k+1]
}

// firstQuestionnaireFromTokens decodes the first ColorJSON / string-payload
// token in consumed as a questionnaireSpec. Tolerates leading/trailing prose
// around the JSON object.
//
// Error shape is load-bearing: REQ-PAR-002 requires that callers (namely
// buildClarifyA2UIPayload) can surface the first 512 bytes of the raw
// response when decoding fails, so the returned error ALWAYS wraps the raw
// payload prefix (truncated) when one was present.
func firstQuestionnaireFromTokens(consumed []cpn.Token) (questionnaireSpec, error) {
	for i := range consumed {
		s, ok := consumed[i].Payload.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(s) == "" {
			return questionnaireSpec{}, fmt.Errorf("decode questionnaire: empty LLM output: raw=%q", rawPayloadPrefix(s))
		}
		jsonStr := extractJSONObject(s)
		if jsonStr == "" {
			return questionnaireSpec{}, fmt.Errorf("decode questionnaire: no JSON object found in payload: raw=%q", rawPayloadPrefix(s))
		}
		var spec questionnaireSpec
		if err := json.Unmarshal([]byte(jsonStr), &spec); err != nil {
			return questionnaireSpec{}, fmt.Errorf("decode questionnaire: %w: raw=%q", err, rawPayloadPrefix(s))
		}
		return spec, nil
	}
	return questionnaireSpec{}, fmt.Errorf("no questionnaire token found")
}

// rawPayloadPrefix returns the first 512 bytes of s so a decode error can
// surface what the model actually emitted without dumping a huge payload
// into the error string. 512 is the cap mandated by REQ-PAR-002.
func rawPayloadPrefix(s string) string {
	const maxLen = 512
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// sessionIDFromTokens best-effort extracts the session id from the first
// consumed token that carries one. Used only for structured logging.
func sessionIDFromTokens(consumed []cpn.Token) string {
	for i := range consumed {
		if consumed[i].SessionID != "" {
			return consumed[i].SessionID
		}
	}
	return ""
}

// buildClarifyA2UIPayload converts the t-ask questionnaire JSON into the A2UI
// component payload that the React renderer understands.
//
// Contract (REQ-PAR-002): on unrecoverable decode failure, returns (nil, err)
// — NEVER (nil, nil). The error wraps the first 512 bytes of the raw LLM
// output so operators can diagnose the failing model directly from logs. A
// structured slog.Error accompanies the return so even callers that
// swallow the error still see the payload prefix in the log stream.
//
// Dispatch (spec-architecture-cpn-iterative-clarification-loop.md §4.3, §4.4):
// the builder sniffs the p-questions payload to pick one of:
//   - escape-hatch card (payload contains "escape_hatch" field)
//   - standard questionnaire (default)
//
// Both variants receive the optional `round` envelope (REQ-100) when a
// non-zero p-round token is present in consumed.
func buildClarifyA2UIPayload(consumed []cpn.Token) (any, error) {
	// Escape-hatch sniff: if the first JSON-string payload carries the
	// escape_hatch field, render the binary card instead of a
	// questionnaire. This lets the same t-clarify HITL transition drive
	// either surface (REQ-013 / PAT-002).
	if seed, ok := firstEscapeHatchSeed(consumed); ok {
		rr := reassessResult{
			Decision:              "request_user_choice",
			ContradictionDetected: seed.Kind == "contradiction",
			FrustrationSignal:     seed.Kind == "frustration",
			UserChoice:            seed.UserChoice,
		}
		rt, _ := parseRoundTokenValue(consumed)
		return buildEscapeHatchA2UIPayload(rr, rt, loadReassessConfig())
	}

	spec, err := firstQuestionnaireFromTokens(consumed)
	if err != nil {
		// Structured log carries the same identity fields the LLM-call
		// log emits (REQ-OBS-001) so the failure can be correlated back
		// to a specific session/transition.
		var raw string
		for i := range consumed {
			if s, ok := consumed[i].Payload.(string); ok {
				raw = rawPayloadPrefix(s)
				break
			}
		}
		slog.Error("clarify payload build failed",
			"session_id", sessionIDFromTokens(consumed),
			"raw_prefix", raw,
			"error", err.Error(),
		)
		return nil, err
	}

	children := make([]map[string]any, 0, len(spec.Questions))
	for _, q := range spec.Questions {
		opts := make([]map[string]any, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, map[string]any{"id": o.ID, "label": o.Label})
		}
		children = append(children, map[string]any{
			"type": "choice",
			"props": map[string]any{
				"id":            q.ID,
				"label":         q.Prompt,
				"recommended":   q.Recommended,
				"quoteFromUser": q.QuoteFromUser,
				"whyItMatters":  q.WhyItMatters,
				"options":       opts,
			},
		})
	}

	env := map[string]any{
		"components": []map[string]any{{
			"type": "questionnaire",
			"props": map[string]any{
				"id":           "t-clarify",
				"submitLabel":  "Send answers",
				"restatedGoal": spec.RestatedGoal,
				"assumptions":  spec.Assumptions,
			},
			"children": children,
		}},
	}
	// REQ-100: attach the round badge envelope only when a non-zero round
	// token is present. Zero / missing → no badge (backward-compatible).
	if rt, ok := parseRoundTokenValue(consumed); ok && rt.N > 0 {
		env["round"] = map[string]any{"n": rt.N, "max": loadReassessConfig().MaxRounds}
	}
	return env, nil
}

// firstEscapeHatchSeed decodes the first token carrying an escape-hatch seed.
// Returns (seed, true) when the payload's outer JSON has an "escape_hatch"
// field. Silent on all other payloads.
func firstEscapeHatchSeed(consumed []cpn.Token) (escapeHatchSeed, bool) {
	for i := range consumed {
		s, ok := consumed[i].Payload.(string)
		if !ok {
			continue
		}
		if !strings.Contains(s, `"escape_hatch"`) {
			continue
		}
		var seed escapeHatchSeed
		if err := json.Unmarshal([]byte(s), &seed); err != nil {
			continue
		}
		if seed.Kind == "" {
			continue
		}
		return seed, true
	}
	return escapeHatchSeed{}, false
}

// parseRoundTokenValue is a []cpn.Token (by-value) adapter for the pointer
// form used by guards. Kept local to the topology package to avoid widening
// the guard API.
func parseRoundTokenValue(consumed []cpn.Token) (roundToken, bool) {
	return parseRoundToken(tokensByValue(consumed))
}

// buildClarifiedToken parses the user's structured submit response, merges it
// with the questionnaire spec, and returns a ColorString token that the
// downstream planner LLM consumes as a user message.
func buildClarifiedToken(consumed []cpn.Token, resp cpn.HITLResponse) (cpn.Token, error) {
	if resp.Action != cpn.HITLSubmit && resp.Action != cpn.HITLApprove {
		return cpn.Token{}, fmt.Errorf("unexpected HITL action %q for t-clarify", resp.Action)
	}

	spec, err := firstQuestionnaireFromTokens(consumed)
	if err != nil {
		return cpn.Token{}, err
	}

	answers := map[string]string{}
	if strings.TrimSpace(resp.Content) != "" {
		if err := json.Unmarshal([]byte(resp.Content), &answers); err != nil {
			return cpn.Token{}, fmt.Errorf("parse clarification answers: %w", err)
		}
	}

	// Render a deterministic, LLM-friendly summary of the answers, resolving
	// option ids back to their labels so the planner sees human text rather
	// than opaque ids.
	//
	// REQ-030 (spec-architecture-cpn-iterative-clarification-loop.md): the
	// suppressive "Produce the plan now — do not ask any further questions"
	// directive MUST NOT be emitted here. With t-reassess formally gating
	// the planner, the directive is redundant when another round is
	// warranted and harmful when we want the planner to receive stated
	// assumptions. Routing-aware suppression lives in buildPlannerPreamble
	// (PAT-003). A neutral prefix tells downstream consumers (t-reassess
	// history, planner preamble) that what follows is user-authored answers.
	var b strings.Builder
	b.WriteString("User answers follow.\n\n")
	if strings.TrimSpace(spec.RestatedGoal) != "" {
		fmt.Fprintf(&b, "Agreed goal: %s\n", spec.RestatedGoal)
	}
	if len(spec.Assumptions) > 0 {
		b.WriteString("Agreed assumptions:\n")
		for _, a := range spec.Assumptions {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if spec.RestatedGoal != "" || len(spec.Assumptions) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("Clarification answers:\n")
	for _, q := range spec.Questions {
		choiceID := answers[q.ID]
		label := choiceID
		for _, o := range q.Options {
			if o.ID == choiceID {
				label = o.Label
				break
			}
		}
		if label == "" {
			label = "(no answer)"
		}
		fmt.Fprintf(&b, "- %s: %s\n", q.Prompt, label)
	}

	return cpn.Token{
		Color:   cpn.ColorString,
		Payload: b.String(),
	}, nil
}
