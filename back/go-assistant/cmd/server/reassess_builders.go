package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Planner preamble (REQ-031/032, §4.5) ────────────────────────────────────

// buildPlannerPreamble prepends a routing-aware directive to the planner
// token on every t-plan-clarified firing. Three variants:
//
//	A — natural convergence (decision == proceed_to_plan, no escape hatch).
//	B — hard cap (guard force-overrode the LLM's clarify_again).
//	C — escape hatch resolved as "proceed" (user bailed out of follow-ups).
//
// When the upstream reassess output is unparseable (zero-value rr), the
// preamble synthesizes a minimal assumption so the planner does not fire
// blind (AC-007).
func buildPlannerPreamble(rr reassessResult, rt roundToken, cfg reassessConfig) string {
	// Fail-open: zero-value reassess is the signature of an upstream parse
	// error. Prepend a synthetic assumption and let the planner proceed.
	if rr.Decision == "" && rr.ResidualAmbiguity == 0 && len(rr.StatedAssumptions) == 0 && len(rr.MissingDimensions) == 0 {
		return "The clarification reassessment could not be parsed. Proceed with best-effort interpretation of the user's original message. " +
			"State clearly under \"Assumptions:\" each non-trivial inference you had to make, including: " +
			"\"Proceeding with best-effort interpretation due to upstream parse error.\"\n\n"
	}

	// Variant B: hard cap — the guard force-overrode clarify_again at
	// n+1 >= max. This is independent of the LLM's decision field.
	if rt.N+1 >= cfg.MaxRounds && strings.EqualFold(rr.Decision, "clarify_again") {
		return fmt.Sprintf(
			"The clarification budget has been exhausted (%d rounds). "+
				"Produce the plan now with best-available information. "+
				"Do not ask further questions. State assumptions for any remaining gaps under \"Assumptions:\" and proceed.\n\n",
			cfg.MaxRounds,
		)
	}

	// Variant C: user picked "proceed" on the escape hatch (or we arrived
	// here from a frustration / contradiction resolution).
	if rr.FrustrationSignal || (rr.ContradictionDetected && strings.EqualFold(rr.Decision, "proceed_to_plan")) {
		var b strings.Builder
		b.WriteString("The user explicitly chose to proceed with assumptions instead of answering more questions. ")
		b.WriteString("Produce the plan now. State clearly under \"Assumptions:\" each non-trivial inference you had to make.\n\n")
		writeStatedAssumptions(&b, rr)
		return b.String()
	}

	// Variant A: natural convergence — the LLM judged the token set
	// sufficient. Fall through here for any explicit proceed_to_plan.
	var b strings.Builder
	b.WriteString("You have everything you need. Produce the plan now — do not ask any further questions and do not emit another clarification surface. The clarification loop is closed.\n\n")
	writeStatedAssumptions(&b, rr)
	return b.String()
}

func writeStatedAssumptions(b *strings.Builder, rr reassessResult) {
	if len(rr.StatedAssumptions) == 0 {
		return
	}
	b.WriteString("The following assumptions were derived from a residual-ambiguity scan and MUST appear verbatim under the \"Assumptions:\" header of the plan (translated into the user's language):\n")
	for _, a := range rr.StatedAssumptions {
		fmt.Fprintf(b, "- %s\n", a)
	}
	b.WriteString("\n")
}

// ── t-followup deposits (REQ-006/007/013/040) ───────────────────────────────

// buildFollowupDeposits is the deterministic, no-LLM handler behind
// t-followup. Given a parsed reassess result and the current round token, it
// returns the per-output-place tokens: p-questions (the next questionnaire
// JSON — or the escape-hatch surface seed) and p-round (incremented counter,
// or reset on topic_shift, or unchanged on request_user_choice).
//
// The map's keys align with t-followup's OutputPlaces: "p-questions" and
// "p-round". fireToolHandler deposits each key into the same-named place.
func buildFollowupDeposits(rr reassessResult, rt roundToken) (map[string]cpn.Token, error) {
	out := make(map[string]cpn.Token, 2)

	// ── p-questions: either the deeper questionnaire OR the escape-hatch
	// seed (the same downstream t-clarify payload builder sniffs the shape
	// at render time; see buildClarifyA2UIPayload dispatch).
	var qPayload string
	switch {
	case strings.EqualFold(rr.Decision, "request_user_choice"):
		b, err := json.Marshal(escapeHatchSeed{
			Kind:       escapeHatchKind(rr),
			UserChoice: rr.UserChoice,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal escape-hatch seed: %w", err)
		}
		qPayload = string(b)
	default:
		if rr.NextQuestions == nil {
			return nil, fmt.Errorf("buildFollowupDeposits: decision=%q but next_questions is nil", rr.Decision)
		}
		b, err := json.Marshal(rr.NextQuestions)
		if err != nil {
			return nil, fmt.Errorf("marshal next_questions: %w", err)
		}
		qPayload = string(b)
	}
	out["p-questions"] = cpn.Token{Color: cpn.ColorJSON, Payload: qPayload}

	// ── p-round: increment, reset, or suppress.
	next := rt
	switch {
	case rr.TopicShift:
		next = roundToken{N: 0, Reset: true}
	case strings.EqualFold(rr.Decision, "request_user_choice"):
		// Binary surface — do NOT consume a round budget (REQ-013).
		next.Reset = false
	default:
		next.N = rt.N + 1
		next.Reset = false
	}
	rb, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("marshal round token: %w", err)
	}
	out["p-round"] = cpn.Token{Color: cpn.ColorJSON, Payload: string(rb)}

	return out, nil
}

// escapeHatchSeed is a small envelope written to p-questions when t-reassess
// routes through the escape hatch. buildClarifyA2UIPayload sniffs for the
// "escape_hatch" field and emits the card surface instead of a questionnaire.
type escapeHatchSeed struct {
	Kind       string      `json:"escape_hatch"` // "frustration" | "contradiction"
	UserChoice *userChoice `json:"user_choice"`
}

func escapeHatchKind(rr reassessResult) string {
	if rr.ContradictionDetected {
		return "contradiction"
	}
	return "frustration"
}

// ── Reassess context block (REQ-021) ────────────────────────────────────────

// clarifyRoundSnapshot is one row of round_history handed to t-reassess.
type clarifyRoundSnapshot struct {
	RestatedGoal    string                  `json:"restated_goal"`
	Assumptions     []string                `json:"assumptions"`
	Questions       []questionnaireQuestion `json:"questions"`
	Answers         map[string]string       `json:"answers"`
	AnswersResolved map[string]string       `json:"answers_resolved,omitempty"`
}

// buildReassessContextBlock returns a JSON object carrying every signal
// t-reassess needs (REQ-021). The block is prepended to the transition's
// user-visible message payload as the system-level context.
func buildReassessContextBlock(
	rt roundToken,
	cfg reassessConfig,
	originalUserMessage string,
	classifier classifierResult,
	history []clarifyRoundSnapshot,
) string {
	if history == nil {
		history = []clarifyRoundSnapshot{}
	}
	ctx := map[string]any{
		// REQ-021: 1-indexed current round (n starts at 0 for round 1).
		"round":                 rt.N + 1,
		"max_rounds":            cfg.MaxRounds,
		"original_user_message": originalUserMessage,
		"classifier":            classifier,
		"round_history":         history,
	}
	b, err := json.Marshal(ctx)
	if err != nil {
		// Fallback: defensive — an un-marshal-able context is vanishingly
		// unlikely here (all inputs are string/int/slice). Return a
		// minimal-but-valid placeholder so the LLM still sees structured
		// input.
		return fmt.Sprintf(`{"round":%d,"max_rounds":%d,"classifier":{},"round_history":[]}`, rt.N+1, cfg.MaxRounds)
	}
	return string(b)
}

// ── Escape-hatch A2UI card (§4.4) ───────────────────────────────────────────

// buildEscapeHatchA2UIPayload produces the two-button card (frustration or
// contradiction variant). The card carries the `round` envelope so the
// frontend renders the same follow-up badge it does on questionnaires.
//
// REQ-124: the contradiction variant sets BOTH buttons to `variant:
// secondary` so neither is highlighted, even if the LLM marks one as
// recommended. The frontend is expected to ignore any `recommended` field on
// this surface.
func buildEscapeHatchA2UIPayload(rr reassessResult, rt roundToken, cfg reassessConfig) (any, error) {
	if rr.UserChoice == nil {
		return nil, fmt.Errorf("buildEscapeHatchA2UIPayload: nil user_choice")
	}
	variant := "escape-frustration"
	title := "Quiero asegurarme de no atascarte"
	if rr.ContradictionDetected {
		variant = "escape-contradiction"
		title = "Hay dos caminos posibles"
	}

	children := make([]any, 0, 1+len(rr.UserChoice.Options))
	children = append(children, map[string]any{
		"type": "text",
		"props": map[string]any{
			"content": rr.UserChoice.Prompt,
		},
	})
	for _, opt := range rr.UserChoice.Options {
		btnVariant := "secondary"
		// Frustration variant: primary highlight on the "proceed" button so
		// the "continue with assumptions" path is the low-friction default.
		if !rr.ContradictionDetected && opt.ID == "proceed" {
			btnVariant = "primary"
		}
		children = append(children, map[string]any{
			"type": "button",
			"props": map[string]any{
				"label":      opt.Label,
				"actionType": "hitl:submit",
				"payload":    map[string]any{"answer": opt.ID},
				"size":       "lg",
				"variant":    btnVariant,
				"id":         "t-clarify",
			},
		})
	}

	env := map[string]any{
		"components": []any{
			map[string]any{
				"type":     "card",
				"props":    map[string]any{"title": title, "variant": variant},
				"children": children,
			},
		},
	}
	// Round badge on escape card (frontend contract).
	if rt.N > 0 {
		env["round"] = map[string]any{"n": rt.N, "max": cfg.MaxRounds}
	}
	return env, nil
}

