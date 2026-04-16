package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// These tests drive the clarification-loop transitions directly against the
// unified topology factory. They avoid a full CPN.Run() which would require
// mocking HITL channels, LLM clients, context-window assembly, and the event
// bus. Instead each scenario (fest convergence, frustration escape,
// contradiction escape, hard cap) exercises the routing + handler pipeline
// end-to-end: seed p-reassessed + p-round, invoke the guard, call the tool
// handler, assert the deposits. This matches spec-architecture §9.1..§9.4.

// ── Scenario 1 — fest natural convergence (AC-001 → AC-002) ─────────────────

func TestReassessScenario_FestNaturalConvergence(t *testing.T) {
	// Round 1 reassess output: needs another round.
	r1 := `{
		"residual_ambiguity": 0.45,
		"convergence_delta": 0.35,
		"missing_dimensions": ["audience_size", "success_metric"],
		"resolved_dimensions": ["event_type", "budget_band"],
		"decision": "clarify_again",
		"next_questions": {
			"restated_goal": "Festival comunitario",
			"assumptions": ["Presupuesto 1-10k USD"],
			"questions": [
				{"id":"q3","prompt":"¿audiencia?","options":[{"id":"a","label":"100-300"}]}
			]
		}
	}`
	// Round 2 reassess output: loop converged.
	r2 := `{
		"residual_ambiguity": 0.15,
		"convergence_delta": 0.30,
		"decision": "proceed_to_plan",
		"stated_assumptions": [
			"Festival comunitario presencial en Bogotá.",
			"Presupuesto 1-10k USD.",
			"Asistencia objetivo 100-300 fundadores.",
			"Optimizado para networking, no inversión ni prensa."
		]
	}`

	// At round 1 (n=0 incoming), the guard must route to t-followup.
	toks1 := []*cpn.Token{{Payload: r1}, {Payload: `{"n":0}`}}
	if !guardResidualAmbiguous(toks1) {
		t.Fatal("round 1: guardResidualAmbiguous = false, want true (clarify_again with missing dims)")
	}
	if guardResidualResolved(toks1) {
		t.Fatal("round 1: guardResidualResolved = true, want false (pair must partition)")
	}

	// t-followup handler: consumed [reassess, round] → deposits
	// {p-questions, p-round} with n incremented.
	c := unifiedTopologyFactory("scenario-fest")
	tf := c.Transitions["t-followup"]
	if tf == nil || tf.ToolHandler == nil {
		t.Fatal("missing t-followup / ToolHandler")
	}
	out, err := tf.ToolHandler(context.Background(), []cpn.Token{
		{Payload: r1}, {Payload: `{"n":0}`},
	})
	if err != nil {
		t.Fatalf("t-followup handler err: %v", err)
	}
	// Verify counter incremented to 1.
	var rt roundToken
	if err := json.Unmarshal([]byte(out["p-round"].Payload.(string)), &rt); err != nil {
		t.Fatalf("bad p-round payload: %v", err)
	}
	if rt.N != 1 {
		t.Errorf("round 1 → n = %d, want 1", rt.N)
	}

	// At round 2 (n=1 incoming, decision=proceed_to_plan), the guard must
	// route to t-preplanner.
	toks2 := []*cpn.Token{{Payload: r2}, {Payload: `{"n":1}`}}
	if guardResidualAmbiguous(toks2) {
		t.Fatal("round 2: guardResidualAmbiguous = true, want false (proceed_to_plan)")
	}
	if !guardResidualResolved(toks2) {
		t.Fatal("round 2: guardResidualResolved = false, want true")
	}

	// t-preplanner: builds Variant A preamble with assumptions.
	tp := c.Transitions["t-preplanner"]
	if tp == nil || tp.ToolHandler == nil {
		t.Fatal("missing t-preplanner / ToolHandler")
	}
	out, err = tp.ToolHandler(context.Background(), []cpn.Token{
		{Payload: r2}, {Payload: `{"n":1}`},
	})
	if err != nil {
		t.Fatalf("t-preplanner handler err: %v", err)
	}
	preamble, _ := out["p-planner-input"].Payload.(string)
	if !strings.Contains(preamble, "clarification loop is closed") {
		t.Errorf("Variant A preamble expected; got:\n%s", preamble)
	}
	for _, a := range []string{
		"Festival comunitario presencial en Bogotá.",
		"Asistencia objetivo 100-300 fundadores.",
	} {
		if !strings.Contains(preamble, a) {
			t.Errorf("preamble missing assumption %q", a)
		}
	}
}

// ── Scenario 2 — frustration escape at round 2 (AC-004) ─────────────────────

func TestReassessScenario_FrustrationEscape(t *testing.T) {
	reassess := `{
		"residual_ambiguity": 0.55,
		"convergence_delta": 0.05,
		"frustration_signal": true,
		"decision": "request_user_choice",
		"user_choice": {
			"prompt": "Quiero asegurarme de no atascarte",
			"options": [
				{"id":"drill","label":"Una pregunta más"},
				{"id":"proceed","label":"Sigue con supuestos"}
			]
		}
	}`
	// Counter is at n=1 (second round).
	toks := []*cpn.Token{{Payload: reassess}, {Payload: `{"n":1}`}}

	// Request_user_choice with headroom routes through t-followup
	// (re-firing t-clarify with the escape-hatch seed).
	if !guardResidualAmbiguous(toks) {
		t.Fatal("guardResidualAmbiguous = false, want true (request_user_choice with budget)")
	}

	c := unifiedTopologyFactory("scenario-frustration")
	tf := c.Transitions["t-followup"]
	out, err := tf.ToolHandler(context.Background(), []cpn.Token{
		{Payload: reassess}, {Payload: `{"n":1}`},
	})
	if err != nil {
		t.Fatalf("t-followup err: %v", err)
	}

	// Counter MUST NOT increment on request_user_choice (REQ-013).
	var rt roundToken
	_ = json.Unmarshal([]byte(out["p-round"].Payload.(string)), &rt)
	if rt.N != 1 {
		t.Errorf("n after request_user_choice = %d, want 1 (no increment)", rt.N)
	}

	// p-questions must carry the escape-hatch seed, NOT a questionnaire.
	qs, _ := out["p-questions"].Payload.(string)
	if !strings.Contains(qs, `"escape_hatch":"frustration"`) {
		t.Errorf("p-questions must carry escape_hatch=frustration seed; got:\n%s", qs)
	}

	// Downstream: buildClarifyA2UIPayload should render the escape card
	// (frustration variant) from the seed.
	consumed := []cpn.Token{
		{Payload: qs, Color: cpn.ColorJSON},
		{Payload: `{"n":1}`, Color: cpn.ColorJSON},
	}
	payload, err := buildClarifyA2UIPayload(consumed)
	if err != nil {
		t.Fatalf("buildClarifyA2UIPayload err: %v", err)
	}
	body, _ := json.Marshal(payload)
	if !strings.Contains(string(body), "escape-frustration") {
		t.Errorf("card variant missing; got:\n%s", body)
	}
	if !strings.Contains(string(body), `"drill"`) || !strings.Contains(string(body), `"proceed"`) {
		t.Errorf("escape-hatch buttons missing; got:\n%s", body)
	}
}

// ── Scenario 3 — contradiction escape at round 2 (AC-005) ────────────────────

func TestReassessScenario_ContradictionEscape(t *testing.T) {
	reassess := `{
		"residual_ambiguity": 0.50,
		"convergence_delta": -0.20,
		"contradiction_detected": true,
		"decision": "request_user_choice",
		"user_choice": {
			"prompt": "Hay dos caminos posibles",
			"options": [
				{"id":"founders","label":"Fundadores"},
				{"id":"angels","label":"Inversionistas ángeles"}
			]
		}
	}`
	// First contradiction at n=0 must stay in the loop (one chance to
	// recover) per guardResidualAmbiguous.
	toks := []*cpn.Token{{Payload: reassess}, {Payload: `{"n":0}`}}
	if !guardResidualAmbiguous(toks) {
		t.Fatal("first contradiction (n=0): guardResidualAmbiguous = false, want true")
	}

	// Second contradiction at n=1 MUST terminate the loop.
	toks2 := []*cpn.Token{{Payload: reassess}, {Payload: `{"n":1}`}}
	if guardResidualAmbiguous(toks2) {
		t.Fatal("second contradiction (n=1): guardResidualAmbiguous = true, want false")
	}
	if !guardResidualResolved(toks2) {
		t.Fatal("second contradiction (n=1): guardResidualResolved = false, want true")
	}

	// t-followup on first contradiction still emits the escape-hatch seed
	// (contradiction variant).
	c := unifiedTopologyFactory("scenario-contradiction")
	tf := c.Transitions["t-followup"]
	out, err := tf.ToolHandler(context.Background(), []cpn.Token{
		{Payload: reassess}, {Payload: `{"n":0}`},
	})
	if err != nil {
		t.Fatalf("t-followup err: %v", err)
	}
	qs, _ := out["p-questions"].Payload.(string)
	if !strings.Contains(qs, `"escape_hatch":"contradiction"`) {
		t.Errorf("escape_hatch seed wrong kind; got:\n%s", qs)
	}

	payload, _ := buildClarifyA2UIPayload([]cpn.Token{
		{Payload: qs, Color: cpn.ColorJSON},
	})
	body, _ := json.Marshal(payload)
	if !strings.Contains(string(body), "escape-contradiction") {
		t.Errorf("card variant missing; got:\n%s", body)
	}
	// REQ-124: contradiction card MUST NOT highlight a primary option.
	if strings.Contains(string(body), `"variant":"primary"`) {
		t.Errorf("contradiction card leaked primary variant; got:\n%s", body)
	}
}

// ── Scenario 4 — hard cap at round 3 (AC-003) ───────────────────────────────

func TestReassessScenario_HardCap(t *testing.T) {
	reassess := `{
		"residual_ambiguity": 0.80,
		"missing_dimensions": ["success_metric"],
		"decision": "clarify_again",
		"next_questions": {"questions":[]}
	}`
	// n=2 with default max=3 → n+1 == max → cap.
	toks := []*cpn.Token{{Payload: reassess}, {Payload: `{"n":2}`}}
	if guardResidualAmbiguous(toks) {
		t.Fatal("hard cap: guardResidualAmbiguous = true, want false")
	}
	if !guardResidualResolved(toks) {
		t.Fatal("hard cap: guardResidualResolved = false, want true")
	}

	c := unifiedTopologyFactory("scenario-hardcap")
	tp := c.Transitions["t-preplanner"]
	out, err := tp.ToolHandler(context.Background(), []cpn.Token{
		{Payload: reassess}, {Payload: `{"n":2}`},
	})
	if err != nil {
		t.Fatalf("t-preplanner err: %v", err)
	}
	preamble, _ := out["p-planner-input"].Payload.(string)
	if !strings.Contains(preamble, "clarification budget has been exhausted") {
		t.Errorf("Variant B preamble expected; got:\n%s", preamble)
	}
	if !strings.Contains(preamble, "3") {
		t.Errorf("Variant B preamble must name the round count; got:\n%s", preamble)
	}
}

// ── Scenario 5 — concurrent session isolation (AC-008) ──────────────────────

// TestReassessScenario_ConcurrentSessionIsolation spins up 8 unified topology
// instances in parallel and runs the fest-scenario routing + t-followup
// handler through each. The test asserts that each session's p-round counter
// progresses independently with no cross-contamination — a strong signal
// that the guard + handler pipeline is free of package-global state.
func TestReassessScenario_ConcurrentSessionIsolation(t *testing.T) {
	t.Parallel()

	const sessions = 8
	r1 := `{
		"residual_ambiguity": 0.6,
		"missing_dimensions": ["a"],
		"decision": "clarify_again",
		"next_questions": {"questions":[{"id":"q1","prompt":"?"}]}
	}`

	var wg sync.WaitGroup
	errs := make(chan error, sessions*4)

	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Each goroutine gets its own CPN instance.
			c := unifiedTopologyFactory("session-" + strings.Repeat("s", idx+1))
			tf := c.Transitions["t-followup"]
			if tf == nil {
				errs <- fmtErr("session %d: missing t-followup", idx)
				return
			}
			// Simulate three fest rounds (n=0 → 1 → 2) and verify each
			// increment happens in isolation.
			for round := 0; round < 2; round++ {
				inRound := map[string]any{"n": round}
				inBytes, _ := json.Marshal(inRound)
				consumed := []cpn.Token{
					{Payload: r1},
					{Payload: string(inBytes)},
				}
				out, err := tf.ToolHandler(context.Background(), consumed)
				if err != nil {
					errs <- fmtErr("session %d round %d: handler err %v", idx, round, err)
					return
				}
				var rt roundToken
				_ = json.Unmarshal([]byte(out["p-round"].Payload.(string)), &rt)
				if rt.N != round+1 {
					errs <- fmtErr("session %d round %d: n = %d, want %d", idx, round, rt.N, round+1)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ── Scenario 6 — malformed reassess JSON (AC-007) ───────────────────────────

func TestReassessScenario_MalformedJSONFailsOpen(t *testing.T) {
	// Unparseable reassess payload: the guard pair MUST route to
	// t-preplanner with the AC-007 fail-open preamble.
	toks := []*cpn.Token{{Payload: `{"residual_ambiguity":0.5,`}, {Payload: `{"n":0}`}}
	if guardResidualAmbiguous(toks) {
		t.Error("malformed reassess: ambiguous guard should not fire")
	}
	if !guardResidualResolved(toks) {
		t.Error("malformed reassess: resolved guard should fail-open")
	}

	c := unifiedTopologyFactory("scenario-malformed")
	tp := c.Transitions["t-preplanner"]
	out, err := tp.ToolHandler(context.Background(), []cpn.Token{
		{Payload: `{"residual_ambiguity":0.5,`},
		{Payload: `{"n":0}`},
	})
	if err != nil {
		t.Fatalf("t-preplanner err on malformed input: %v", err)
	}
	preamble, _ := out["p-planner-input"].Payload.(string)
	if !strings.Contains(preamble, "parse error") && !strings.Contains(preamble, "best-effort") {
		t.Errorf("fail-open preamble must cite parse error / best-effort; got:\n%s", preamble)
	}
}

// ── Test helpers ────────────────────────────────────────────────────────────

// fmtErr is a tiny shim so concurrent goroutines can queue errors without
// juggling error types at every call site.
func fmtErr(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}
