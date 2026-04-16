package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── buildPlannerPreamble ────────────────────────────────────────────────────

// TestBuildPlannerPreamble_VariantA_NaturalConvergence covers §4.5 Variant A:
// decision == "proceed_to_plan" with stated_assumptions — the preamble MUST
// include the verbatim assumptions and close the loop explicitly.
func TestBuildPlannerPreamble_VariantA_NaturalConvergence(t *testing.T) {
	rr := reassessResult{
		Decision: "proceed_to_plan",
		StatedAssumptions: []string{
			"Festival comunitario presencial en Bogotá.",
			"Presupuesto 1–10k USD.",
		},
	}
	rt := roundToken{N: 1}
	cfg := reassessConfig{MaxRounds: 3}

	got := buildPlannerPreamble(rr, rt, cfg)

	if !strings.Contains(got, "clarification loop is closed") {
		t.Errorf("Variant A preamble must close the loop; got:\n%s", got)
	}
	for _, a := range rr.StatedAssumptions {
		if !strings.Contains(got, a) {
			t.Errorf("Variant A preamble must include assumption %q; got:\n%s", a, got)
		}
	}
	if !strings.Contains(got, "Assumptions:") {
		t.Errorf("Variant A preamble must reference the Assumptions: header; got:\n%s", got)
	}
}

// TestBuildPlannerPreamble_VariantB_HardCap covers §4.5 Variant B: the guard
// force-overrode the LLM's clarify_again at n+1 >= max. The preamble must
// acknowledge the exhausted budget and instruct the planner to proceed.
func TestBuildPlannerPreamble_VariantB_HardCap(t *testing.T) {
	rr := reassessResult{Decision: "clarify_again"}
	rt := roundToken{N: 2} // at cap for max=3
	cfg := reassessConfig{MaxRounds: 3}

	got := buildPlannerPreamble(rr, rt, cfg)

	if !strings.Contains(got, "clarification budget has been exhausted") {
		t.Errorf("Variant B preamble must mention exhausted budget; got:\n%s", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("Variant B preamble must mention the round count (3); got:\n%s", got)
	}
}

// TestBuildPlannerPreamble_VariantC_EscapeResolved covers §4.5 Variant C: the
// user picked "proceed" on the escape hatch. The preamble must frame the
// explicit-assumption mode.
func TestBuildPlannerPreamble_VariantC_EscapeResolved(t *testing.T) {
	rr := reassessResult{
		Decision:          "proceed_to_plan",
		FrustrationSignal: true,
	}
	rt := roundToken{N: 1}
	cfg := reassessConfig{MaxRounds: 3}

	got := buildPlannerPreamble(rr, rt, cfg)

	if !strings.Contains(got, "explicitly chose") {
		t.Errorf("Variant C preamble must frame user's explicit choice; got:\n%s", got)
	}
}

// TestBuildPlannerPreamble_MalformedFailsOpen covers AC-007: when the reassess
// result is a zero value (parse failed upstream), the preamble must synthesize
// a minimal assumption so the planner is not blind.
func TestBuildPlannerPreamble_MalformedFailsOpen(t *testing.T) {
	rr := reassessResult{} // zero value
	rt := roundToken{}
	cfg := reassessConfig{MaxRounds: 3}

	got := buildPlannerPreamble(rr, rt, cfg)

	if !strings.Contains(got, "parse error") && !strings.Contains(got, "best-effort") {
		t.Errorf("malformed-fallback preamble must mention parse-error / best-effort; got:\n%s", got)
	}
}

// ── buildFollowupToken (no topic shift) ─────────────────────────────────────

// TestBuildFollowupToken_IncrementsCounter covers REQ-006: t-followup
// deposits p-round with n incremented by 1.
func TestBuildFollowupToken_IncrementsCounter(t *testing.T) {
	rr := reassessResult{
		Decision: "clarify_again",
		NextQuestions: &questionnaireSpec{
			RestatedGoal: "g",
			Questions: []questionnaireQuestion{
				{ID: "q1", Prompt: "p", Options: []questionnaireOption{{ID: "a", Label: "A"}}},
			},
		},
	}
	rt := roundToken{N: 1}

	outs, err := buildFollowupDeposits(rr, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	roundTok, ok := outs["p-round"]
	if !ok {
		t.Fatalf("missing p-round deposit; got keys: %v", mapKeys(outs))
	}
	qTok, ok := outs["p-questions"]
	if !ok {
		t.Fatalf("missing p-questions deposit; got keys: %v", mapKeys(outs))
	}

	var rt2 roundToken
	s, _ := roundTok.Payload.(string)
	if err := json.Unmarshal([]byte(s), &rt2); err != nil {
		t.Fatalf("p-round payload not JSON: %v (%q)", err, s)
	}
	if rt2.N != 2 {
		t.Errorf("p-round N = %d, want 2 (incremented)", rt2.N)
	}
	if rt2.Reset {
		t.Errorf("p-round Reset = true, want false (no topic shift)")
	}

	// p-questions token carries the next questionnaire so t-clarify can
	// publish the A2UI surface.
	qs, _ := qTok.Payload.(string)
	if !strings.Contains(qs, `"q1"`) {
		t.Errorf("p-questions payload must carry next_questions JSON; got:\n%s", qs)
	}
}

// TestBuildFollowupToken_TopicShiftResetsCounter covers REQ-040/041: when
// t-reassess sets topic_shift, the deposited p-round token has n=0 AND
// reset=true.
func TestBuildFollowupToken_TopicShiftResetsCounter(t *testing.T) {
	rr := reassessResult{
		Decision:   "clarify_again",
		TopicShift: true,
		NextQuestions: &questionnaireSpec{
			Questions: []questionnaireQuestion{{ID: "q1", Prompt: "p"}},
		},
	}
	rt := roundToken{N: 2}

	outs, err := buildFollowupDeposits(rr, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got roundToken
	s, _ := outs["p-round"].Payload.(string)
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if got.N != 0 {
		t.Errorf("N = %d, want 0 (topic-shift reset)", got.N)
	}
	if !got.Reset {
		t.Errorf("Reset = false, want true (topic-shift observability flag)")
	}
}

// TestBuildFollowupToken_RequestUserChoiceSuppressesIncrement covers REQ-013:
// the binary escape-hatch surface does not consume a round budget, so the
// counter is NOT incremented on request_user_choice decisions.
func TestBuildFollowupToken_RequestUserChoiceSuppressesIncrement(t *testing.T) {
	rr := reassessResult{
		Decision: "request_user_choice",
		UserChoice: &userChoice{
			Prompt: "?",
			Options: []userChoiceOption{
				{ID: "drill", Label: "A"},
				{ID: "proceed", Label: "B"},
			},
		},
	}
	rt := roundToken{N: 1}

	outs, err := buildFollowupDeposits(rr, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got roundToken
	s, _ := outs["p-round"].Payload.(string)
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if got.N != 1 {
		t.Errorf("N = %d, want 1 (no increment on request_user_choice)", got.N)
	}
}

// ── buildClarifyA2UIPayload with round field ─────────────────────────────────

// TestBuildClarifyA2UIPayload_RoundField covers CON-005/REQ-100: the new
// optional `round` field appears on the envelope ONLY when the upstream
// consumed tokens carry a non-zero round token. Without it, the envelope
// stays backward-compatible.
func TestBuildClarifyA2UIPayload_RoundField(t *testing.T) {
	questionnaireJSON := `{
		"restated_goal": "g",
		"assumptions": ["a"],
		"questions": [{"id":"q1","prompt":"p","options":[{"id":"a","label":"A"}]}]
	}`

	cases := []struct {
		name      string
		consumed  []cpn.Token
		wantRound bool
		wantN     float64
	}{
		{
			name:      "no round token — no badge",
			consumed:  []cpn.Token{{Payload: questionnaireJSON}},
			wantRound: false,
		},
		{
			name: "n=0 round token — no badge",
			consumed: []cpn.Token{
				{Payload: questionnaireJSON},
				{Payload: `{"n":0,"reset":false}`},
			},
			wantRound: false,
		},
		{
			name: "n=2 round token — badge with n=2 max=3",
			consumed: []cpn.Token{
				{Payload: questionnaireJSON},
				{Payload: `{"n":2,"reset":false}`},
			},
			wantRound: true,
			wantN:     2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := buildClarifyA2UIPayload(tc.consumed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m, _ := payload.(map[string]any)
			round, has := m["round"]
			if has != tc.wantRound {
				t.Fatalf("round present=%v, want %v; envelope=%v", has, tc.wantRound, m)
			}
			if !tc.wantRound {
				return
			}
			rm, ok := round.(map[string]any)
			if !ok {
				t.Fatalf("round is not a map: %T", round)
			}
			// In-memory the builder emits int; through JSON roundtrip
			// the value becomes float64. Normalize before compare so
			// the test survives either representation.
			gotN := normalizeNumber(rm["n"])
			if gotN != tc.wantN {
				t.Errorf("round.n = %v (%T), want %v", rm["n"], rm["n"], tc.wantN)
			}
			if _, hasMax := rm["max"]; !hasMax {
				t.Errorf("round must include 'max' field; got %v", rm)
			}
		})
	}
}

// ── buildEscapeHatchA2UIPayload ─────────────────────────────────────────────

// TestBuildEscapeHatchA2UIPayload_Frustration covers §4.4 + REQ-121/122/123:
// the frustration variant emits a card with title "Quiero asegurarme..." and
// two buttons labelled "Una pregunta más" / "Sigue con supuestos".
//
// NOTE: the backend emits a neutral English label-key (not the Spanish copy)
// because i18n lives on the frontend — the card-variant tag and the
// canonical answer values ("drill" / "proceed") are the load-bearing
// backend contract. We assert the variant tag + action payloads here.
func TestBuildEscapeHatchA2UIPayload_Frustration(t *testing.T) {
	rr := reassessResult{
		Decision:          "request_user_choice",
		FrustrationSignal: true,
		UserChoice: &userChoice{
			Prompt: "Quiero asegurarme de no atascarte",
			Options: []userChoiceOption{
				{ID: "drill", Label: "Una pregunta más"},
				{ID: "proceed", Label: "Sigue con supuestos"},
			},
		},
	}
	rt := roundToken{N: 1}
	cfg := reassessConfig{MaxRounds: 3}

	payload, err := buildEscapeHatchA2UIPayload(rr, rt, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := json.Marshal(payload)
	s := string(body)

	if !strings.Contains(s, `"escape-frustration"`) {
		t.Errorf("frustration card must carry variant=escape-frustration; got:\n%s", s)
	}
	if !strings.Contains(s, `"drill"`) {
		t.Errorf("frustration card must include drill answer; got:\n%s", s)
	}
	if !strings.Contains(s, `"proceed"`) {
		t.Errorf("frustration card must include proceed answer; got:\n%s", s)
	}
	if !strings.Contains(s, `"round"`) || !strings.Contains(s, `"max"`) {
		t.Errorf("escape card must include round badge envelope; got:\n%s", s)
	}
}

// TestBuildEscapeHatchA2UIPayload_Contradiction covers REQ-124: the
// contradiction variant must use the two LLM-supplied options as equal-weight
// buttons and must NOT honour any `recommended` bias.
func TestBuildEscapeHatchA2UIPayload_Contradiction(t *testing.T) {
	rr := reassessResult{
		Decision:              "request_user_choice",
		ContradictionDetected: true,
		UserChoice: &userChoice{
			Prompt: "Hay dos caminos posibles",
			Options: []userChoiceOption{
				{ID: "founders", Label: "Fundadores"},
				{ID: "angels", Label: "Inversionistas ángeles"},
			},
		},
	}
	rt := roundToken{N: 1}
	cfg := reassessConfig{MaxRounds: 3}

	payload, err := buildEscapeHatchA2UIPayload(rr, rt, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := json.Marshal(payload)
	s := string(body)

	if !strings.Contains(s, `"escape-contradiction"`) {
		t.Errorf("contradiction card must carry variant=escape-contradiction; got:\n%s", s)
	}
	if !strings.Contains(s, `"founders"`) || !strings.Contains(s, `"angels"`) {
		t.Errorf("contradiction card must carry both options; got:\n%s", s)
	}
	// Verify both buttons are secondary (equal-weight) — neither is styled
	// as primary/recommended.
	if strings.Count(s, `"variant":"primary"`) > 0 {
		t.Errorf("contradiction card must not highlight a primary option; got:\n%s", s)
	}
}

// ── buildReassessContextBlock ───────────────────────────────────────────────

// TestBuildReassessContextBlock covers REQ-021: the t-reassess LLM receives a
// JSON context block with round, max_rounds, original_user_message,
// classifier, and round_history.
func TestBuildReassessContextBlock(t *testing.T) {
	classifier := classifierResult{Intent: "task", NeedsClarification: true, Missing: []string{"event_type"}}
	history := []clarifyRoundSnapshot{
		{
			RestatedGoal: "fest",
			Assumptions:  []string{"presencial"},
			Questions: []questionnaireQuestion{
				{ID: "q1", Prompt: "event_type?", Options: []questionnaireOption{{ID: "a", Label: "festival"}}},
			},
			Answers: map[string]string{"q1": "a"},
		},
	}
	cfg := reassessConfig{MaxRounds: 3}
	rt := roundToken{N: 1}

	got := buildReassessContextBlock(rt, cfg, "ayúdame a planear un fest", classifier, history)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("context block must be valid JSON: %v\n%s", err, got)
	}
	for _, k := range []string{"round", "max_rounds", "original_user_message", "classifier", "round_history"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing required key %q; got %v", k, parsed)
		}
	}
	if parsed["round"] != float64(2) { // 1-indexed per REQ-021
		t.Errorf("round = %v, want 2 (1-indexed current round)", parsed["round"])
	}
	if parsed["max_rounds"] != float64(3) {
		t.Errorf("max_rounds = %v, want 3", parsed["max_rounds"])
	}
}

// ── helpers for the tests above ─────────────────────────────────────────────

func mapKeys(m map[string]cpn.Token) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// normalizeNumber converts int / int64 / float64 to float64 so table-driven
// tests over post-JSON-roundtrip payloads don't flake on numeric typing.
func normalizeNumber(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	}
	return 0
}
