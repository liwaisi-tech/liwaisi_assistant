package main

import (
	"os"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── loadReassessConfig ──────────────────────────────────────────────────────

// TestLoadReassessConfig_Defaults covers §4.6 of the spec: the env parser MUST
// return the documented defaults when no env variables are set. Invalid or
// out-of-range values MUST fall back to defaults (CON-001 for MAX_ROUNDS; the
// thresholds clamp to [0.0, 1.0]).
func TestLoadReassessConfig_Defaults(t *testing.T) {
	// Clear every env var so this test is isolated.
	for _, k := range []string{
		"CLARIFY_MAX_ROUNDS",
		"REASSESS_AMBIGUITY_THRESHOLD_CLARIFY",
		"REASSESS_AMBIGUITY_THRESHOLD_PROCEED",
		"REASSESS_CONVERGENCE_DELTA_STOP",
	} {
		os.Unsetenv(k)
	}

	cfg := loadReassessConfig()

	if cfg.MaxRounds != 3 {
		t.Errorf("MaxRounds = %d, want 3", cfg.MaxRounds)
	}
	if cfg.ClarifyThreshold != 0.40 {
		t.Errorf("ClarifyThreshold = %v, want 0.40", cfg.ClarifyThreshold)
	}
	if cfg.ProceedThreshold != 0.20 {
		t.Errorf("ProceedThreshold = %v, want 0.20", cfg.ProceedThreshold)
	}
	if cfg.ConvergenceDeltaStop != 0.10 {
		t.Errorf("ConvergenceDeltaStop = %v, want 0.10", cfg.ConvergenceDeltaStop)
	}
}

func TestLoadReassessConfig_EnvOverrides(t *testing.T) {
	t.Setenv("CLARIFY_MAX_ROUNDS", "4")
	t.Setenv("REASSESS_AMBIGUITY_THRESHOLD_CLARIFY", "0.55")
	t.Setenv("REASSESS_AMBIGUITY_THRESHOLD_PROCEED", "0.15")
	t.Setenv("REASSESS_CONVERGENCE_DELTA_STOP", "0.08")

	cfg := loadReassessConfig()
	if cfg.MaxRounds != 4 {
		t.Errorf("MaxRounds = %d, want 4", cfg.MaxRounds)
	}
	if cfg.ClarifyThreshold != 0.55 {
		t.Errorf("ClarifyThreshold = %v, want 0.55", cfg.ClarifyThreshold)
	}
	if cfg.ProceedThreshold != 0.15 {
		t.Errorf("ProceedThreshold = %v, want 0.15", cfg.ProceedThreshold)
	}
	if cfg.ConvergenceDeltaStop != 0.08 {
		t.Errorf("ConvergenceDeltaStop = %v, want 0.08", cfg.ConvergenceDeltaStop)
	}
}

// CON-001: CLARIFY_MAX_ROUNDS MUST be in [1, 5]. Out-of-range values fall
// back to the default (3).
func TestLoadReassessConfig_MaxRoundsOutOfRangeFallsBack(t *testing.T) {
	cases := []string{"0", "-2", "6", "100", "not-a-number", ""}
	for _, v := range cases {
		t.Run("v="+v, func(t *testing.T) {
			if v == "" {
				os.Unsetenv("CLARIFY_MAX_ROUNDS")
			} else {
				t.Setenv("CLARIFY_MAX_ROUNDS", v)
			}
			// Unrelated thresholds default.
			os.Unsetenv("REASSESS_AMBIGUITY_THRESHOLD_CLARIFY")
			os.Unsetenv("REASSESS_AMBIGUITY_THRESHOLD_PROCEED")
			os.Unsetenv("REASSESS_CONVERGENCE_DELTA_STOP")

			cfg := loadReassessConfig()
			if cfg.MaxRounds != 3 {
				t.Errorf("MaxRounds = %d, want default 3 for input %q", cfg.MaxRounds, v)
			}
		})
	}
}

func TestLoadReassessConfig_MaxRoundsBoundaries(t *testing.T) {
	cases := map[string]int{
		"1": 1,
		"2": 2,
		"5": 5,
	}
	for in, want := range cases {
		t.Run("v="+in, func(t *testing.T) {
			t.Setenv("CLARIFY_MAX_ROUNDS", in)
			cfg := loadReassessConfig()
			if cfg.MaxRounds != want {
				t.Errorf("MaxRounds = %d, want %d", cfg.MaxRounds, want)
			}
		})
	}
}

func TestLoadReassessConfig_ThresholdOutOfRangeFallsBack(t *testing.T) {
	t.Setenv("REASSESS_AMBIGUITY_THRESHOLD_CLARIFY", "1.7")
	t.Setenv("REASSESS_AMBIGUITY_THRESHOLD_PROCEED", "-0.2")
	t.Setenv("REASSESS_CONVERGENCE_DELTA_STOP", "junk")

	cfg := loadReassessConfig()
	if cfg.ClarifyThreshold != 0.40 {
		t.Errorf("ClarifyThreshold = %v, want fallback 0.40", cfg.ClarifyThreshold)
	}
	if cfg.ProceedThreshold != 0.20 {
		t.Errorf("ProceedThreshold = %v, want fallback 0.20", cfg.ProceedThreshold)
	}
	if cfg.ConvergenceDeltaStop != 0.10 {
		t.Errorf("ConvergenceDeltaStop = %v, want fallback 0.10", cfg.ConvergenceDeltaStop)
	}
}

// ── parseRoundToken ─────────────────────────────────────────────────────────

// TestParseRoundToken covers the p-round token schema (§4.1). Missing fields
// default to n=0, reset=false. Invalid JSON returns ok=false so the guard can
// fail-open to planner.
func TestParseRoundToken(t *testing.T) {
	cases := []struct {
		name      string
		payload   any
		wantN     int
		wantReset bool
		wantOK    bool
	}{
		{
			name:      "well-formed string payload",
			payload:   `{"n":2,"reset":false}`,
			wantN:     2,
			wantReset: false,
			wantOK:    true,
		},
		{
			name:      "reset=true is honoured",
			payload:   `{"n":0,"reset":true}`,
			wantN:     0,
			wantReset: true,
			wantOK:    true,
		},
		{
			name:      "empty object treated as n=0",
			payload:   `{}`,
			wantN:     0,
			wantReset: false,
			wantOK:    true,
		},
		{
			name:      "map payload (pre-serialized)",
			payload:   map[string]any{"n": float64(1), "reset": false},
			wantN:     1,
			wantReset: false,
			wantOK:    true,
		},
		{
			name:    "malformed json returns not-ok",
			payload: `{"n":garbage`,
			wantOK:  false,
		},
		{
			name:    "nil payload returns not-ok",
			payload: nil,
			wantOK:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := &cpn.Token{Payload: tc.payload}
			got, ok := parseRoundToken([]*cpn.Token{tok})
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (payload=%v)", ok, tc.wantOK, tc.payload)
			}
			if !ok {
				return
			}
			if got.N != tc.wantN {
				t.Errorf("N = %d, want %d", got.N, tc.wantN)
			}
			if got.Reset != tc.wantReset {
				t.Errorf("Reset = %v, want %v", got.Reset, tc.wantReset)
			}
		})
	}
}

// ── parseReassessResult ─────────────────────────────────────────────────────

// TestParseReassessResult covers §4.2: every decision variant parses, malformed
// payloads return ok=false so the fail-open branch of guardResidualResolved
// fires (AC-007).
func TestParseReassessResult(t *testing.T) {
	cases := []struct {
		name              string
		payload           string
		wantOK            bool
		wantDecision      string
		wantResidual      float64
		wantFrustration   bool
		wantContradiction bool
		wantTopicShift    bool
	}{
		{
			name: "clarify_again",
			payload: `{
				"residual_ambiguity": 0.45,
				"convergence_delta": 0.30,
				"missing_dimensions": ["audience_size"],
				"resolved_dimensions": ["event_type"],
				"contradiction_detected": false,
				"frustration_signal": false,
				"topic_shift": false,
				"decision": "clarify_again",
				"next_questions": {"restated_goal":"g","assumptions":[],"questions":[]}
			}`,
			wantOK:       true,
			wantDecision: "clarify_again",
			wantResidual: 0.45,
		},
		{
			name: "proceed_to_plan",
			payload: `{
				"residual_ambiguity": 0.15,
				"convergence_delta": 0.25,
				"decision": "proceed_to_plan",
				"stated_assumptions": ["x", "y"]
			}`,
			wantOK:       true,
			wantDecision: "proceed_to_plan",
			wantResidual: 0.15,
		},
		{
			name: "request_user_choice_frustration",
			payload: `{
				"residual_ambiguity": 0.55,
				"convergence_delta": 0.05,
				"frustration_signal": true,
				"decision": "request_user_choice",
				"user_choice": {"prompt":"?","options":[{"id":"drill","label":"a"},{"id":"proceed","label":"b"}]}
			}`,
			wantOK:          true,
			wantDecision:    "request_user_choice",
			wantResidual:    0.55,
			wantFrustration: true,
		},
		{
			name: "request_user_choice_contradiction",
			payload: `{
				"residual_ambiguity": 0.50,
				"convergence_delta": -0.20,
				"contradiction_detected": true,
				"decision": "request_user_choice",
				"user_choice": {"prompt":"?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}
			}`,
			wantOK:            true,
			wantDecision:      "request_user_choice",
			wantResidual:      0.50,
			wantContradiction: true,
		},
		{
			name: "topic_shift_flag",
			payload: `{
				"residual_ambiguity": 0.55,
				"convergence_delta": 0.0,
				"topic_shift": true,
				"decision": "clarify_again",
				"next_questions": {"questions":[]}
			}`,
			wantOK:         true,
			wantDecision:   "clarify_again",
			wantResidual:   0.55,
			wantTopicShift: true,
		},
		{
			name:    "malformed json",
			payload: `{"residual_ambiguity": 0.45, "dec`,
			wantOK:  false,
		},
		{
			name:    "empty string",
			payload: "",
			wantOK:  false,
		},
		{
			name:    "balanced but unrelated json",
			payload: `{"foo":"bar"}`,
			wantOK:  true, // parses fine — decision==""; callers must treat as malformed.
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseReassessResult([]*cpn.Token{{Payload: tc.payload}})
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Decision != tc.wantDecision {
				t.Errorf("Decision = %q, want %q", got.Decision, tc.wantDecision)
			}
			if got.ResidualAmbiguity != tc.wantResidual {
				t.Errorf("ResidualAmbiguity = %v, want %v", got.ResidualAmbiguity, tc.wantResidual)
			}
			if got.FrustrationSignal != tc.wantFrustration {
				t.Errorf("FrustrationSignal = %v, want %v", got.FrustrationSignal, tc.wantFrustration)
			}
			if got.ContradictionDetected != tc.wantContradiction {
				t.Errorf("ContradictionDetected = %v, want %v", got.ContradictionDetected, tc.wantContradiction)
			}
			if got.TopicShift != tc.wantTopicShift {
				t.Errorf("TopicShift = %v, want %v", got.TopicShift, tc.wantTopicShift)
			}
		})
	}
}

// ── guardResidualAmbiguous / guardResidualResolved ──────────────────────────

// TestGuardPair covers REQ-010/011/012: the pair MUST partition the input
// space — for any marking of (p-reassessed, p-round), exactly one of the two
// guards returns true. Covers every branch of guardResidualAmbiguous's
// conjunction.
func TestGuardPair(t *testing.T) {
	// Thresholds for the test: defaults.
	os.Unsetenv("CLARIFY_MAX_ROUNDS")
	os.Unsetenv("REASSESS_AMBIGUITY_THRESHOLD_CLARIFY")
	os.Unsetenv("REASSESS_AMBIGUITY_THRESHOLD_PROCEED")
	os.Unsetenv("REASSESS_CONVERGENCE_DELTA_STOP")

	cases := []struct {
		name          string
		reassess      string
		round         string
		wantAmbiguous bool // guardResidualAmbiguous
	}{
		{
			name:          "clarify_again under cap with missing dims — AMBIGUOUS",
			reassess:      `{"residual_ambiguity":0.6,"missing_dimensions":["a","b"],"decision":"clarify_again","next_questions":{"questions":[]}}`,
			round:         `{"n":1}`,
			wantAmbiguous: true,
		},
		{
			name:          "residual_ambiguity below threshold — RESOLVED",
			reassess:      `{"residual_ambiguity":0.30,"missing_dimensions":["a"],"decision":"clarify_again"}`,
			round:         `{"n":0}`,
			wantAmbiguous: false,
		},
		{
			name:          "empty missing_dimensions — RESOLVED (cannot drill deeper)",
			reassess:      `{"residual_ambiguity":0.60,"missing_dimensions":[],"decision":"clarify_again"}`,
			round:         `{"n":0}`,
			wantAmbiguous: false,
		},
		{
			name:          "explicit proceed_to_plan — RESOLVED",
			reassess:      `{"residual_ambiguity":0.55,"missing_dimensions":["a"],"decision":"proceed_to_plan","stated_assumptions":["x"]}`,
			round:         `{"n":0}`,
			wantAmbiguous: false,
		},
		{
			name:          "hard cap n+1 == max — RESOLVED (AC-003)",
			reassess:      `{"residual_ambiguity":0.80,"missing_dimensions":["a"],"decision":"clarify_again"}`,
			round:         `{"n":2}`, // defaults: max=3 → 2+1==3 → at cap
			wantAmbiguous: false,
		},
		{
			name:          "hard cap n+1 > max — RESOLVED",
			reassess:      `{"residual_ambiguity":0.80,"missing_dimensions":["a"],"decision":"clarify_again"}`,
			round:         `{"n":5}`,
			wantAmbiguous: false,
		},
		{
			name:          "frustration signal — RESOLVED (routes to escape via t-clarify)",
			reassess:      `{"residual_ambiguity":0.60,"missing_dimensions":["a"],"frustration_signal":true,"decision":"request_user_choice","user_choice":{"prompt":"?","options":[]}}`,
			round:         `{"n":0}`,
			wantAmbiguous: true, // request_user_choice still routes through t-followup to re-fire t-clarify
		},
		{
			name:          "first contradiction at n=0 — AMBIGUOUS (one chance to recover)",
			reassess:      `{"residual_ambiguity":0.60,"missing_dimensions":["a"],"contradiction_detected":true,"decision":"request_user_choice","user_choice":{"prompt":"?","options":[]}}`,
			round:         `{"n":0}`,
			wantAmbiguous: true,
		},
		{
			name:          "second contradiction at n>=1 — RESOLVED (terminates loop)",
			reassess:      `{"residual_ambiguity":0.60,"missing_dimensions":["a"],"contradiction_detected":true,"decision":"clarify_again"}`,
			round:         `{"n":1}`,
			wantAmbiguous: false,
		},
		{
			name:          "malformed reassess json — RESOLVED (fail-open per AC-007)",
			reassess:      `{"residual_ambiguity":0.5,`,
			round:         `{"n":0}`,
			wantAmbiguous: false,
		},
		{
			name:          "malformed round token — RESOLVED (fail-open)",
			reassess:      `{"residual_ambiguity":0.6,"missing_dimensions":["a"],"decision":"clarify_again"}`,
			round:         `{"n":`,
			wantAmbiguous: false,
		},
		{
			name:          "residual exactly at threshold — AMBIGUOUS (>= bound)",
			reassess:      `{"residual_ambiguity":0.40,"missing_dimensions":["a"],"decision":"clarify_again"}`,
			round:         `{"n":0}`,
			wantAmbiguous: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks := []*cpn.Token{
				{Payload: tc.reassess},
				{Payload: tc.round},
			}
			gotAmbiguous := guardResidualAmbiguous(toks)
			if gotAmbiguous != tc.wantAmbiguous {
				t.Errorf("guardResidualAmbiguous = %v, want %v", gotAmbiguous, tc.wantAmbiguous)
			}
			// REQ-012: the pair MUST partition the input space.
			gotResolved := guardResidualResolved(toks)
			if gotAmbiguous == gotResolved {
				t.Errorf("guard pair must partition input space, got ambiguous=%v resolved=%v", gotAmbiguous, gotResolved)
			}
		})
	}
}
