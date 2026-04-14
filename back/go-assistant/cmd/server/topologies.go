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
		Prompt: "",
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
		"p-plan":       cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed":   cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":     cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	// t-classify: fast intent classifier using lightweight model.
	tClassify := cpn.NewTransition("t-classify", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-classified"})
	tClassify.SystemPrompt = envOr("PROMPT_CLASSIFIER", `You are an intent classifier. Read the user's latest message and emit a single JSON object. No prose, no code fences, no explanation.

The message may be in any human language. Classify identically regardless of language. Do not translate the message.

A "USER CONTEXT — REGIONAL REGISTER" preamble may be prepended to this prompt at runtime. When present, use the listed idiom glosses to resolve ambiguous imperatives in the user's regional register (e.g., a verb that looks like a command in standard usage but is conversational in that variant). Do not enumerate the idioms in your output; let them inform the conversation/task decision in Step 1.

Schema (all keys required):
{"intent":"conversation"|"task","needs_clarification":bool,"missing":[string,...],"confidence":0.0}

Decision procedure — run these steps mentally, then emit JSON.

Step 0. Meta-question short-circuit (HIGHEST PRIORITY).
A "meta-question" asks about the assistant itself: its identity, name, purpose, mission, capabilities, scope, persona, who built it, what it can do, how it works, how to use it, or what it knows. Meta-questions are ALWAYS:
  {"intent":"conversation","needs_clarification":false,"missing":[],"confidence":0.98}
Detect meta-questions by SEMANTICS, not by keyword. Any phrasing in any language that interrogates the assistant's nature qualifies.

Step 1. Intent.
- "conversation": greetings, small talk, thanks, acknowledgements, emotional reactions, single factual questions answerable in one short paragraph, follow-up questions about something already said, opinion questions, and ALL meta-questions (see Step 0).
- "task": the user explicitly asks the assistant to PRODUCE a multi-step deliverable — build, design, plan, implement, write a document or code, analyze a dataset, research a topic in depth, refactor, teach a multi-step procedure, or otherwise hand back a structured artifact that a reasonable person would review before shipping.
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
- needs_clarification == true  ⇒ intent MUST be "task" AND missing MUST have 1..4 entries.
- needs_clarification == false ⇒ missing MUST be [].

Step 5. Confidence (float in [0.0, 1.0]).
Your certainty about BOTH the intent choice AND (for tasks) the specification judgment. NOT your confidence you could answer the user. Calibration: 0.99 = certain, 0.85 = clearly right, 0.7 = more likely right than not, 0.5 = coin flip. Be honest; do not inflate.

Output contract: respond with ONLY the JSON object. The first character of your response MUST be "{" and the last character MUST be "}". No preamble, no code fences, no trailing text.`)
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
	tDirect.SystemPrompt = envOr("PROMPT_DIRECT", braeIdentity+`
Respond naturally and concisely. Be warm but engineer-minded — direct, practical, no fluff.

`+langRule)
	tDirect.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}
	tDirect.Guard = guardDirectConversation

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

	// t-plan-clarified: fires after the user has answered the clarification
	// questionnaire. Reads the merged classifier+answers payload from p-clarified.
	tPlanClarified := cpn.NewTransition("t-plan-clarified", cpn.NodeKindLLM,
		[]string{"p-clarified"}, []string{"p-plan"})
	tPlanClarified.SystemPrompt = planClarifiedSystemPrompt
	tPlanClarified.LLMConfig = planLLMConfig()
	tPlanClarified.Guard = guardPlanTaskClarified

	// t-ask: when classifier flags missing info, generate a small JSON questionnaire
	// using the missing[] list as hints. Output is consumed by t-clarify (HITL).
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
		Model:        "structured",
		MaxTokens:    envInt("MAX_TOKENS_ASK", 1024),
		Temperature:  0.3,
		RequireJSON:  true,
		StreamOutput: false,
		// SkipHistory=true mirrors the t-classify precedent at ~line 348:
		// t-ask's raw JSON is routing metadata consumed only by
		// t-clarify's A2UIPayloadBuilder (buildClarifyA2UIPayload) and
		// buildClarifiedToken, never by downstream conversational
		// transitions. Persisting it would surface the raw questionnaire
		// JSON above the $$a2ui: interactive bubble on rehydration. See
		// spec-process-bugfix-a2ui-hitl-rehydration REQ-016 / INV-004.
		SkipHistory: true,
		// JSON questionnaire generator: schema is rigid and the regional
		// register adds no signal — opt out to save tokens (CON-004).
		SkipRegionalPreamble: true,
	}
	tAsk.Guard = guardNeedsClarification

	// t-clarify: HITL transition that publishes the questionnaire as an A2UI
	// surface and waits for the user's structured "submit" response. On
	// resolve it merges the answers into the classifier JSON and deposits a
	// ColorJSON token into p-clarified.
	tClarify := cpn.NewTransition("t-clarify", cpn.NodeKindHITL,
		[]string{"p-questions"}, []string{"p-clarified"})
	tClarify.HITLConfig = &cpn.HITLConfig{
		Prompt:             "Answer the questionnaire to clarify your request.",
		RevisionLoop:       false,
		A2UIPayloadBuilder: buildClarifyA2UIPayload,
		OutputBuilder:      buildClarifiedToken,
	}

	// t-review: HITL gate — waits for user approval. The prompt text is
	// intentionally empty: the A2UI Review Required card already labels
	// itself, and a duplicate plain-text bubble was visual noise.
	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "",
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

// extractJSONObject pulls the outermost {...} object out of a string that may
// be wrapped in chatty prose, fenced code blocks, or other preamble. Models
// asked for JSON-only sometimes prepend "Claro, aquí tienes:" or similar; this
// makes the parser tolerant without weakening the schema.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
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
				return s[start : i+1]
			}
		}
	}
	return ""
}

// firstQuestionnaireFromTokens decodes the first ColorJSON / string-payload
// token in consumed as a questionnaireSpec. Tolerates leading/trailing prose
// around the JSON object.
func firstQuestionnaireFromTokens(consumed []cpn.Token) (questionnaireSpec, error) {
	for i := range consumed {
		s, ok := consumed[i].Payload.(string)
		if !ok {
			continue
		}
		jsonStr := extractJSONObject(s)
		if jsonStr == "" {
			return questionnaireSpec{}, fmt.Errorf("decode questionnaire: no JSON object found in payload")
		}
		var spec questionnaireSpec
		if err := json.Unmarshal([]byte(jsonStr), &spec); err != nil {
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
				"id":            q.ID,
				"label":         q.Prompt,
				"recommended":   q.Recommended,
				"quoteFromUser": q.QuoteFromUser,
				"whyItMatters":  q.WhyItMatters,
				"options":       opts,
			},
		})
	}

	return map[string]any{
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
	// than opaque ids. The leading directive reinforces the system prompt so
	// the planner cannot loop back into another clarification round.
	var b strings.Builder
	b.WriteString("I have answered your clarification questions. Produce the plan now — do not ask any further questions. If anything is still unspecified, state a reasonable assumption under \"Assumptions:\" and proceed.\n\n")
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
