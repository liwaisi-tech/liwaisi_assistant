package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// defaultClassifierConfidenceThreshold is the minimum Confidence the classifier
// must emit for a task flagged as fully-specified to skip the clarification
// questionnaire. Below this, the safety net routes through t-ask even if
// needs_clarification=false. Overridable via CLASSIFIER_CONFIDENCE_THRESHOLD.
const defaultClassifierConfidenceThreshold = 0.7

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

// guardDirectConversation fires t-direct when the classifier output does NOT contain "task".
func guardDirectConversation(tokens []*cpn.Token) bool {
	r, ok := parseClassified(tokens)
	if !ok {
		return true // default to conversation on parse failure
	}
	return !strings.EqualFold(r.Intent, "task")
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
	// Confidence gate: require the classifier to be sufficiently sure before
	// skipping clarification. Keeps ambiguous-but-overconfident prompts out of
	// the direct-plan path. See guardNeedsClarification for the complement.
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
	tLLM.SystemPrompt = envOr("PROMPT_DIRECT", "You are a helpful assistant. Be concise.")
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
	tPlan.SystemPrompt = envOr("PROMPT_PLAN", "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. "+
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\"")
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}

	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", "You are a helpful assistant. The user approved the following plan. "+
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
func unifiedTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":      cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-classified": cpn.NewPlace("p-classified", cpn.ColorJSON, cpn.SpaceSurface),
		"p-questions":  cpn.NewPlace("p-questions", cpn.ColorJSON, cpn.SpaceSurface),
		"p-clarified":  cpn.NewPlace("p-clarified", cpn.ColorString, cpn.SpaceSurface),
		"p-plan":       cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed":   cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":     cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	// t-classify: fast intent classifier using lightweight model.
	tClassify := cpn.NewTransition("t-classify", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-classified"})
	tClassify.SystemPrompt = envOr("PROMPT_CLASSIFIER", `You are an intent classifier. You read the user's latest message and emit a single JSON object. No prose, no code fences, no explanation.

Schema (all keys required):
{"intent":"conversation"|"task","needs_clarification":bool,"missing":[string,...],"confidence":0.0}

Decision procedure — run these steps mentally, then emit JSON.

Step 1. Intent.
- "conversation": greetings, small talk, thanks, acknowledgements, a single factual question answerable in one short paragraph, or a meta-question about you.
- "task": anything that asks you to produce, build, design, plan, implement, write, analyze, research, refactor, teach step-by-step, or otherwise deliver an artifact or multi-step result.
- When genuinely torn between the two, pick "task".

Step 2. If intent == "task", judge specification-completeness with this rule:
  A task is FULLY SPECIFIED if a senior practitioner in the relevant field could
  produce a concrete, non-generic plan without making more than ONE significant
  assumption about an unnamed parameter. Otherwise it is UNDER-SPECIFIED.

  Assumptions that silently change scope, audience, stack, deliverable shape,
  or success criteria count as missing information. Prefer asking over guessing.

Step 3. Mental checklist — for the task at hand, which of these are load-bearing
(i.e. a different answer would produce a meaningfully different plan)?
  - audience / who it is for
  - success criterion / what "done" looks like
  - stack, medium, or format (language, framework, channel, document type, ...)
  - scope boundary (what is in, what is out, how big)
  - timeline or effort budget
  - constraints (compliance, budget, tooling, environment)
Count how many load-bearing items the user did NOT name. If zero or one, the
task is fully specified. If two or more, it is under-specified.

Step 4. Fill the fields:
- needs_clarification = true  ⇒ under-specified. "missing" MUST list 1..4 short
  snake_case field names drawn from the load-bearing items you identified. Use
  names that make sense for the actual domain; do not invent keys you cannot
  defend. If you cannot name at least one concrete missing field, the task is
  not actually under-specified — set needs_clarification=false and missing=[].
- needs_clarification = false ⇒ fully specified. "missing" MUST be [].
- intent == "conversation"    ⇒ needs_clarification=false, missing=[].

Step 5. Confidence (float in [0.0, 1.0]).
- Reflects your certainty about BOTH the intent choice AND, for tasks, the
  specification-completeness judgment. It is NOT how confident you are that
  you could answer the user.
- Calibration anchors:
    1.0 = certain, no reasonable reading of the message contradicts your call.
    0.85 = clearly right, minor edge cases exist.
    0.7 = more likely right than not, but you can imagine a plausible alternative reading.
    0.5 = coin flip.
    <0.5 = you are guessing.
- Below 0.7 means meaningfully unsure; downstream will treat it as a safety-net
  trigger for clarification. Be honest — do not inflate.

Examples (shape and edge cases; do not pattern-match on domain):

User: "hey, how are you?"
→ {"intent":"conversation","needs_clarification":false,"missing":[],"confidence":0.99}

User: "Write a 500-word beginner-friendly blog post in English explaining what a semaphore is, with one code example in Python, for publication on our engineering blog tomorrow."
→ {"intent":"task","needs_clarification":false,"missing":[],"confidence":0.92}

User: "help me with my project"
→ {"intent":"task","needs_clarification":true,"missing":["project_topic","goal","scope","deadline"],"confidence":0.95}

User: "plan a workshop about our new feature"
→ {"intent":"task","needs_clarification":true,"missing":["audience","duration","format","success_criteria"],"confidence":0.9}

Respond ONLY with the JSON object.`)
	tClassify.LLMConfig = &cpn.LLMConfig{
		Model:        "classifier",
		MaxTokens:    128,
		Temperature:  0.0,
		RequireJSON:  true,
		StreamOutput: false,
		SkipHistory:  true,
	}

	// t-direct: fires for conversation intent — direct streaming response.
	tDirect := cpn.NewTransition("t-direct", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-output"})
	tDirect.SystemPrompt = envOr("PROMPT_DIRECT", "You are a helpful, friendly assistant. Respond naturally and concisely.")
	tDirect.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}
	tDirect.Guard = guardDirectConversation

	planSystemPrompt := envOr("PROMPT_PLAN", "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. "+
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\"")
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
	tPlanDirect.SystemPrompt = planSystemPrompt
	tPlanDirect.LLMConfig = planLLMConfig()
	tPlanDirect.Guard = guardPlanTaskDirect

	// t-plan-clarified: fires after the user has answered the clarification
	// questionnaire. Reads the merged classifier+answers payload from p-clarified.
	tPlanClarified := cpn.NewTransition("t-plan-clarified", cpn.NodeKindLLM,
		[]string{"p-clarified"}, []string{"p-plan"})
	tPlanClarified.SystemPrompt = planSystemPrompt
	tPlanClarified.LLMConfig = planLLMConfig()
	tPlanClarified.Guard = guardPlanTaskClarified

	// t-ask: when classifier flags missing info, generate a small JSON questionnaire
	// using the missing[] list as hints. Output is consumed by t-clarify (HITL).
	tAsk := cpn.NewTransition("t-ask", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-questions"})
	tAsk.SystemPrompt = envOr("PROMPT_ASK", `You generate a short multiple-choice questionnaire to clarify a task request before planning.

Input: a JSON object with the user's classified intent and a "missing" array of field names that need clarification.

Output: JSON only, matching this exact schema:
{"questions":[{"id":"q1","prompt":"...","recommended":"opt-a","options":[{"id":"opt-a","label":"..."},{"id":"opt-b","label":"..."}]}]}

CORE PRINCIPLE: each question asks for ONE decision. The "options" are concrete,
mutually-exclusive candidate ANSWERS the user can literally pick — never
rephrasings of the question, never meta-questions, never placeholders.

Each option "label":
- Is a short noun-phrase or sentence the user could say as an answer.
- Is NOT a question and does NOT end with "?".
- Is at most ~60 characters.
- Is distinct from the other options (they partition the reasonable answer space).

Correct vs WRONG illustration (neutral domain — writing a tutorial):
  Question prompt: "Who is the primary reader of the tutorial?"
  Correct options: ["Complete beginners", "Intermediate developers", "Experienced engineers", "Technical decision-makers"]
  WRONG options:   ["Who will read it?", "What skill level?", "Target audience?"]
The wrong set just rewords the question; the correct set gives pickable answers.

Rules:
- One question per item in "missing" (max 4 questions total).
- Each question MUST have 2-4 option objects. Use ids opt-a, opt-b, opt-c, opt-d.
- Options must cover the likely answer space with 2-4 plausible, distinct buckets.
  They need not be exhaustive — the UI offers a free-text "Other" fallback — but
  every listed option must be a real candidate answer.
- "recommended" is the option id that is the most sensible default GIVEN THE
  USER'S ORIGINAL REQUEST. Pick the one that best fits the context; never leave
  it as a placeholder, never pick at random. Always set it.
- Keep each "prompt" concise (under 80 characters) and phrased as a real question.
- Do not include any keys other than "questions".

Language: produce every "prompt" and every option "label" in the SAME LANGUAGE
as the user's original request. If the user wrote in Spanish, answer in Spanish;
if in English, English; if in another language, mirror that language. Do not
translate, do not mix languages within a question.

Fallback (empty or missing "missing" array): the upstream classifier was unsure
but did not enumerate fields. Derive 2-4 load-bearing dimensions from the user's
original request — choose from: audience, success criterion, scope boundary,
stack/medium/format, timeline — and emit one question per dimension. Each
question must still follow every rule above: concrete pickable answer options,
no meta-questions, sensible "recommended", user's language.`)
	tAsk.LLMConfig = &cpn.LLMConfig{
		Model:        "classifier",
		MaxTokens:    envInt("MAX_TOKENS_ASK", 1024),
		Temperature:  0.2,
		RequireJSON:  true,
		StreamOutput: false,
		SkipHistory:  true,
	}
	tAsk.Guard = guardNeedsClarification

	// t-clarify: HITL transition that publishes the questionnaire as an A2UI
	// surface and waits for the user's structured "submit" response. On
	// resolve it merges the answers into the classifier JSON and deposits a
	// ColorJSON token into p-clarified.
	tClarify := cpn.NewTransition("t-clarify", cpn.NodeKindHITL,
		[]string{"p-questions"}, []string{"p-clarified"})
	tClarify.HITLConfig = &cpn.HITLConfig{
		Prompt:       "Answer the questionnaire to clarify your request.",
		RevisionLoop: false,
		A2UIPayloadBuilder: func(consumed []cpn.Token) (any, error) {
			return buildClarifyA2UIPayload(consumed)
		},
		OutputBuilder: func(consumed []cpn.Token, resp cpn.HITLResponse) (cpn.Token, error) {
			return buildClarifiedToken(consumed, resp)
		},
	}

	// t-review: HITL gate — waits for user approval.
	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	// t-execute: fires after approval — executes the plan.
	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", "You are a helpful assistant. The user approved the following plan. "+
		"Execute it thoroughly and provide the final result.")
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-classify":       tClassify,
		"t-direct":         tDirect,
		"t-ask":            tAsk,
		"t-clarify":        tClarify,
		"t-plan-direct":    tPlanDirect,
		"t-plan-clarified": tPlanClarified,
		"t-review":         tReview,
		"t-execute":        tExecute,
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

// ── Clarification helpers ───────────────────────────────────────────────────

// questionnaireSpec mirrors the JSON shape produced by t-ask. Only the fields
// we need for A2UI rendering and answer-merging are decoded.
type questionnaireSpec struct {
	Questions []questionnaireQuestion `json:"questions"`
}

type questionnaireQuestion struct {
	ID          string                `json:"id"`
	Prompt      string                `json:"prompt"`
	Recommended string                `json:"recommended,omitempty"`
	Options     []questionnaireOption `json:"options"`
}

type questionnaireOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// firstQuestionnaireFromTokens decodes the first ColorJSON / string-payload
// token in consumed as a questionnaireSpec.
func firstQuestionnaireFromTokens(consumed []cpn.Token) (questionnaireSpec, error) {
	for _, tok := range consumed {
		s, ok := tok.Payload.(string)
		if !ok {
			continue
		}
		var spec questionnaireSpec
		if err := json.Unmarshal([]byte(s), &spec); err != nil {
			return questionnaireSpec{}, fmt.Errorf("decode questionnaire: %w", err)
		}
		return spec, nil
	}
	return questionnaireSpec{}, fmt.Errorf("no questionnaire token found")
}

// buildClarifyA2UIPayload converts the t-ask questionnaire JSON into the A2UI
// component payload that the React renderer understands.
func buildClarifyA2UIPayload(consumed []cpn.Token) (any, error) {
	spec, err := firstQuestionnaireFromTokens(consumed)
	if err != nil {
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
				"id":          q.ID,
				"label":       q.Prompt,
				"recommended": q.Recommended,
				"options":     opts,
			},
		})
	}

	return map[string]any{
		"components": []map[string]any{{
			"type": "questionnaire",
			"props": map[string]any{
				"id":          "t-clarify",
				"submitLabel": "Send answers",
			},
			"children": children,
		}},
	}, nil
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
	var b strings.Builder
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
