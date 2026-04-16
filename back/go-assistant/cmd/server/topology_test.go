package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestHITLTopology_HasExpectedPlacesAndTransitions(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	expectedPlaces := []string{"p-input", "p-plan", "p-reviewed", "p-output"}
	for _, id := range expectedPlaces {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place %q", id)
		}
	}

	expectedTransitions := []string{"t-plan", "t-review", "t-execute"}
	for _, id := range expectedTransitions {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition %q", id)
		}
	}
}

func TestHITLTopology_HITLTransitionHasConfig(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	tReview, ok := c.Transitions["t-review"]
	if !ok {
		t.Fatal("missing t-review transition")
	}

	if tReview.Kind != cpn.NodeKindHITL {
		t.Errorf("t-review kind = %q, want %q", tReview.Kind, cpn.NodeKindHITL)
	}

	if tReview.HITLConfig == nil {
		t.Fatal("t-review HITLConfig is nil")
	}

	// HITLConfig.Prompt is intentionally empty: the A2UI Review Required
	// card labels itself and a plain-text bubble would be visual noise.
	// See topologies.go for the rationale.

	// Channel must be nil — wired by SessionService.CreateSession.
	if tReview.HITLConfig.Channel != nil {
		t.Error("t-review HITLConfig.Channel should be nil (wired by session service)")
	}
}

func TestHITLTopology_PassesValidationAfterWiring(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	// Simulate what SessionService.CreateSession does: wire HITL channels.
	for _, tr := range c.Transitions {
		if tr.Kind == cpn.NodeKindHITL && tr.HITLConfig != nil {
			tr.HITLConfig.Channel = make(chan cpn.Token, 1)
		}
	}

	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Errorf("topology validation failed: %v", err)
	}
}

func TestHITLTopology_SpaceAssignment(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	tests := []struct {
		place string
		space cpn.SpaceKind
	}{
		{"p-input", cpn.SpaceSurface},
		{"p-plan", cpn.SpaceSurface},
		{"p-reviewed", cpn.SpaceComputation},
		{"p-output", cpn.SpaceSurface},
	}

	for _, tt := range tests {
		p, ok := c.Places[tt.place]
		if !ok {
			t.Errorf("missing place %q", tt.place)
			continue
		}
		if p.Space != tt.space {
			t.Errorf("place %q space = %q, want %q", tt.place, p.Space, tt.space)
		}
	}
}

// ── Unified Topology Tests ──────────────────────────────────────────────────

func TestUnifiedTopology_HasAllPlacesAndTransitions(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	expectedPlaces := []string{"p-input", "p-classified", "p-questions", "p-clarified", "p-plan", "p-reviewed", "p-output"}
	for _, id := range expectedPlaces {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place %q", id)
		}
	}

	expectedTransitions := []string{"t-classify", "t-direct", "t-ask", "t-clarify", "t-plan-direct", "t-plan-clarified", "t-review", "t-execute"}
	for _, id := range expectedTransitions {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition %q", id)
		}
	}
}

// Regression: SessionService.SendMessage calls CPN.Reset() before every
// user turn. Before SeedFunc existed, Reset wiped p-round (the iterative
// clarification counter) and never restored it. Downstream transitions
// t-followup / t-preplanner both consume p-round as a second input arc,
// so their CanFire check returned false and the CPN deadlocked silently
// after t-reassess completed — the frontend saw "Ejecutando → Inactivo"
// with no assistant bubble. This test pins the Reset → seed contract.
func TestUnifiedTopology_ResetPreservesPRoundSeed(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	round, ok := c.Places["p-round"]
	if !ok {
		t.Fatal("missing place p-round")
	}
	if got := round.Len(); got != 1 {
		t.Fatalf("initial p-round Len = %d, want 1 (seeded)", got)
	}

	c.Reset()

	if got := round.Len(); got != 1 {
		t.Fatalf("post-Reset p-round Len = %d, want 1 (re-seeded)", got)
	}
	snaps, _ := round.Peek()
	if len(snaps) == 0 {
		t.Fatal("post-Reset p-round has no token")
	}
	payload, _ := snaps[0].Payload.(string)
	if payload != `{"n":0,"reset":false}` {
		t.Fatalf("post-Reset p-round payload = %q, want seeded {n:0}", payload)
	}
}

// TestUnifiedTopology_ClassifierConfig asserts REQ-CFG-003/004: the
// authored LLMConfig expresses intent via Role, NOT Model. The Model field
// is stamped by applyUserModelPreferences at session-resolve time and MUST
// remain empty at topology-factory time.
func TestUnifiedTopology_ClassifierConfig(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	tc := c.Transitions["t-classify"]
	if tc.Kind != cpn.NodeKindLLM {
		t.Errorf("t-classify kind = %q, want llm", tc.Kind)
	}
	if tc.LLMConfig == nil {
		t.Fatal("t-classify LLMConfig is nil")
	}
	if tc.LLMConfig.Model != "" {
		t.Errorf("t-classify Model = %q, want empty (resolver writes it per REQ-CFG-003)", tc.LLMConfig.Model)
	}
	if tc.LLMConfig.Role != "classifier" {
		t.Errorf("t-classify Role = %q, want 'classifier' (REQ-CFG-004)", tc.LLMConfig.Role)
	}
	if !tc.LLMConfig.RequireJSON {
		t.Error("t-classify should have RequireJSON=true")
	}
	if tc.LLMConfig.StreamOutput {
		t.Error("t-classify should have StreamOutput=false")
	}
	if tc.LLMConfig.MaxTokens != 128 {
		t.Errorf("t-classify MaxTokens = %d, want 128", tc.LLMConfig.MaxTokens)
	}
}

// TestUnifiedTopology_TAskConfig asserts REQ-CFG-003/004 and REQ-PAR-004
// for t-ask: Role="structured", Model="", ResponseFmtRequired=true.
func TestUnifiedTopology_TAskConfig(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	tAsk, ok := c.Transitions["t-ask"]
	if !ok {
		t.Fatal("missing t-ask transition")
	}
	if tAsk.LLMConfig == nil {
		t.Fatal("t-ask LLMConfig is nil")
	}
	if tAsk.LLMConfig.Model != "" {
		t.Errorf("t-ask Model = %q, want empty (resolver writes it)", tAsk.LLMConfig.Model)
	}
	if tAsk.LLMConfig.Role != "structured" {
		t.Errorf("t-ask Role = %q, want 'structured'", tAsk.LLMConfig.Role)
	}
	if !tAsk.LLMConfig.RequireJSON {
		t.Error("t-ask should have RequireJSON=true")
	}
	if !tAsk.LLMConfig.ResponseFmtRequired {
		t.Error("t-ask should have ResponseFmtRequired=true (REQ-PAR-004)")
	}
}

func TestUnifiedTopology_DirectGuardMatchesConversation(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	tDirect := c.Transitions["t-direct"]
	if tDirect.Guard == nil {
		t.Fatal("t-direct guard is nil")
	}

	conv := &cpn.Token{Payload: `{"intent":"conversation"}`}
	task := &cpn.Token{Payload: `{"intent":"task"}`}

	if !tDirect.Guard([]*cpn.Token{conv}) {
		t.Error("t-direct guard should match conversation intent")
	}
	if tDirect.Guard([]*cpn.Token{task}) {
		t.Error("t-direct guard should NOT match task intent")
	}
}

func TestUnifiedTopology_PlanDirectGuardMatchesFullySpecifiedTask(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	tPlan := c.Transitions["t-plan-direct"]
	if tPlan.Guard == nil {
		t.Fatal("t-plan-direct guard is nil")
	}

	task := &cpn.Token{Payload: `{"intent":"task"}`}
	conv := &cpn.Token{Payload: `{"intent":"conversation"}`}

	if !tPlan.Guard([]*cpn.Token{task}) {
		t.Error("t-plan-direct guard should match task intent")
	}
	if tPlan.Guard([]*cpn.Token{conv}) {
		t.Error("t-plan-direct guard should NOT match conversation intent")
	}
}

func TestUnifiedTopology_GuardsDefaultToDirectOnAmbiguity(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	tDirect := c.Transitions["t-direct"]
	tPlan := c.Transitions["t-plan-direct"]

	// Non-string payload — ambiguous: should default to direct (safe).
	ambiguous := &cpn.Token{Payload: 42}
	if !tDirect.Guard([]*cpn.Token{ambiguous}) {
		t.Error("t-direct guard should default to true on ambiguous input")
	}
	if tPlan.Guard([]*cpn.Token{ambiguous}) {
		t.Error("t-plan-direct guard should default to false on ambiguous input")
	}

	// Classifier returns a synonym like "greeting" — should route to direct.
	greeting := &cpn.Token{Payload: `{"intent":"greeting"}`}
	if !tDirect.Guard([]*cpn.Token{greeting}) {
		t.Error("t-direct guard should match non-task intents like greeting")
	}
	if tPlan.Guard([]*cpn.Token{greeting}) {
		t.Error("t-plan-direct guard should NOT match non-task intents like greeting")
	}
}

// ── Clarification routing ──────────────────────────────────────────────────

func TestUnifiedTopology_ClarificationGuardMatrix(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	tAsk := c.Transitions["t-ask"]
	tPlanDirect := c.Transitions["t-plan-direct"]
	tPlanClarified := c.Transitions["t-plan-clarified"]

	if tAsk == nil || tPlanDirect == nil || tPlanClarified == nil {
		t.Fatal("expected t-ask, t-plan-direct, t-plan-clarified transitions")
	}

	cases := []struct {
		name           string
		payload        string
		wantAsk        bool
		wantPlanDirect bool
	}{
		{
			name:           "fully_specified_task_bypasses_clarification",
			payload:        `{"intent":"task","needs_clarification":false,"missing":[]}`,
			wantAsk:        false,
			wantPlanDirect: true,
		},
		{
			name:           "missing_info_routes_through_t_ask",
			payload:        `{"intent":"task","needs_clarification":true,"missing":["audience","language"]}`,
			wantAsk:        true,
			wantPlanDirect: false,
		},
		{
			// Defensive: an invalid classifier shape (needs_clarification=true
			// with empty missing[]) MUST NOT silently fall through to the
			// direct-plan path. The clarification guard fires; the planner
			// stays closed. See spec REQ-002 / AC-003.
			name:           "needs_clarification_true_with_empty_missing_does_not_bypass",
			payload:        `{"intent":"task","needs_clarification":true,"missing":[]}`,
			wantAsk:        true,
			wantPlanDirect: false,
		},
		{
			name:           "conversation_skips_both",
			payload:        `{"intent":"conversation","needs_clarification":false,"missing":[]}`,
			wantAsk:        false,
			wantPlanDirect: false,
		},
		{
			name:           "legacy_classifier_without_new_fields_still_plans_direct",
			payload:        `{"intent":"task"}`,
			wantAsk:        false,
			wantPlanDirect: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := &cpn.Token{Payload: tc.payload}
			if got := tAsk.Guard([]*cpn.Token{tok}); got != tc.wantAsk {
				t.Errorf("t-ask guard = %v, want %v", got, tc.wantAsk)
			}
			if got := tPlanDirect.Guard([]*cpn.Token{tok}); got != tc.wantPlanDirect {
				t.Errorf("t-plan-direct guard = %v, want %v", got, tc.wantPlanDirect)
			}
		})
	}

	// t-plan-clarified is unconditional — once a token reaches p-clarified
	// the user has already submitted answers and the planner should fire.
	if !tPlanClarified.Guard([]*cpn.Token{{Payload: "any"}}) {
		t.Error("t-plan-clarified guard should always allow firing")
	}
}

// TestUnifiedTopology_ConfidenceSafetyNet verifies the confidence-based
// routing rules: a task the classifier claims is fully specified still routes
// through clarification if its self-reported confidence is below threshold.
// Conversation intents are never blocked by low confidence.
func TestUnifiedTopology_ConfidenceSafetyNet(t *testing.T) {
	// Make the threshold explicit so the test does not depend on env state.
	t.Setenv("CLASSIFIER_CONFIDENCE_THRESHOLD", "0.7")

	cases := []struct {
		name           string
		payload        string
		wantAsk        bool
		wantPlanDirect bool
		wantDirect     bool
	}{
		{
			name:           "high_confidence_fully_specified_task_plans_direct",
			payload:        `{"intent":"task","needs_clarification":false,"missing":[],"confidence":0.9}`,
			wantAsk:        false,
			wantPlanDirect: true,
		},
		{
			name:           "low_confidence_fully_specified_task_routes_to_ask",
			payload:        `{"intent":"task","needs_clarification":false,"missing":[],"confidence":0.5}`,
			wantAsk:        true,
			wantPlanDirect: false,
		},
		{
			name:           "high_confidence_needs_clarification_routes_to_ask",
			payload:        `{"intent":"task","needs_clarification":true,"missing":["audience"],"confidence":0.9}`,
			wantAsk:        true,
			wantPlanDirect: false,
		},
		{
			name:       "low_confidence_conversation_still_direct",
			payload:    `{"intent":"conversation","needs_clarification":false,"missing":[],"confidence":0.3}`,
			wantDirect: true,
		},
		{
			// Exactly at threshold: plan-direct is allowed (>= threshold).
			name:           "threshold_boundary_exact_match_plans_direct",
			payload:        `{"intent":"task","needs_clarification":false,"missing":[],"confidence":0.7}`,
			wantAsk:        false,
			wantPlanDirect: true,
		},
		{
			// Legacy classifier without confidence: treated as 1.0.
			name:           "legacy_missing_confidence_plans_direct",
			payload:        `{"intent":"task","needs_clarification":false,"missing":[]}`,
			wantAsk:        false,
			wantPlanDirect: true,
		},
	}

	c := unifiedTopologyFactory("test-session")
	tAsk := c.Transitions["t-ask"]
	tPlanDirect := c.Transitions["t-plan-direct"]
	tDirect := c.Transitions["t-direct"]

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := &cpn.Token{Payload: tc.payload}
			if got := tAsk.Guard([]*cpn.Token{tok}); got != tc.wantAsk {
				t.Errorf("t-ask guard = %v, want %v", got, tc.wantAsk)
			}
			if got := tPlanDirect.Guard([]*cpn.Token{tok}); got != tc.wantPlanDirect {
				t.Errorf("t-plan-direct guard = %v, want %v", got, tc.wantPlanDirect)
			}
			if tc.wantDirect {
				if got := tDirect.Guard([]*cpn.Token{tok}); !got {
					t.Errorf("t-direct guard = false, want true (conversation must not be blocked by confidence)")
				}
			}
		})
	}
}

// TestClassifierConfidenceThreshold_EnvOverride verifies the env var plumbing.
func TestClassifierConfidenceThreshold_EnvOverride(t *testing.T) {
	t.Setenv("CLASSIFIER_CONFIDENCE_THRESHOLD", "0.85")
	if got := classifierConfidenceThreshold(); got != 0.85 {
		t.Errorf("threshold = %v, want 0.85", got)
	}

	os.Unsetenv("CLASSIFIER_CONFIDENCE_THRESHOLD")
	if got := classifierConfidenceThreshold(); got != 0.7 {
		t.Errorf("default threshold = %v, want 0.7", got)
	}

	t.Setenv("CLASSIFIER_CONFIDENCE_THRESHOLD", "not-a-number")
	if got := classifierConfidenceThreshold(); got != 0.7 {
		t.Errorf("invalid env threshold should fall back to 0.7, got %v", got)
	}
}

func TestUnifiedTopology_ClarifyHasA2UIBuilder(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	tClarify, ok := c.Transitions["t-clarify"]
	if !ok {
		t.Fatal("missing t-clarify transition")
	}
	if tClarify.HITLConfig == nil {
		t.Fatal("t-clarify HITLConfig is nil")
	}
	if tClarify.HITLConfig.A2UIPayloadBuilder == nil {
		t.Error("t-clarify HITLConfig.A2UIPayloadBuilder must be set")
	}
	if tClarify.HITLConfig.OutputBuilder == nil {
		t.Error("t-clarify HITLConfig.OutputBuilder must be set")
	}
}

func TestUnifiedTopology_PassesValidationAfterWiring(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	for _, tr := range c.Transitions {
		if tr.Kind == cpn.NodeKindHITL && tr.HITLConfig != nil {
			tr.HITLConfig.Channel = make(chan cpn.Token, 1)
		}
	}

	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Errorf("topology validation failed: %v", err)
	}
}

func TestDefaultTopology_StillWorks(t *testing.T) {
	c := defaultTopologyFactory("test-session")

	if _, ok := c.Places["p-input"]; !ok {
		t.Error("missing p-input")
	}
	if _, ok := c.Places["p-output"]; !ok {
		t.Error("missing p-output")
	}
	if _, ok := c.Transitions["t-llm"]; !ok {
		t.Error("missing t-llm")
	}

	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Errorf("topology validation failed: %v", err)
	}
}

// TestUnifiedTopology_TAskDualFlagHistory asserts the dual-flag config
// applied to t-ask per REQ-104 of
// spec-process-bugfix-a2ui-rehydration-completion.md:
//
//	SkipHistory       = false  → input-side: t-ask MUST read the user's
//	                             message from c.History.
//	SkipOutputHistory = true   → output-side: t-ask's raw JSON
//	                             questionnaire MUST NOT be appended to
//	                             c.History (consumed downstream via tokens
//	                             only). Prevents the raw JSON from rendering
//	                             as a stray bubble above the $$a2ui:
//	                             surface on rehydration (INV-101).
func TestUnifiedTopology_TAskDualFlagHistory(t *testing.T) {
	c := unifiedTopologyFactory("test-session")

	tAsk, ok := c.Transitions["t-ask"]
	if !ok {
		t.Fatal("missing t-ask transition in unified topology")
	}
	if tAsk.LLMConfig == nil {
		t.Fatal("t-ask LLMConfig is nil")
	}
	if tAsk.LLMConfig.SkipHistory {
		t.Error("t-ask LLMConfig.SkipHistory = true would blind the transition to the user message in c.History (input-side regression)")
	}
	if !tAsk.LLMConfig.SkipOutputHistory {
		t.Error("t-ask LLMConfig.SkipOutputHistory = false would re-pollute the transcript with raw JSON (REQ-104, INV-101)")
	}
}

// ── t-review A2UIPayloadBuilder wiring ──────────────────────────────────────
// spec-process-bugfix-treview-surface-and-locked-parser.md REQ-BE-001/002.
// Both topology factories that include t-review MUST attach a non-nil
// A2UIPayloadBuilder that produces an A2UI payload containing all three
// action-type strings. Missing builder → surface cannot flow through
// c.History → legacy WARN branch fires → INV-005 violated.

func assertTReviewBuilderEmitsActions(t *testing.T, factoryName string, c *cpn.CPN) {
	t.Helper()

	tReview, ok := c.Transitions["t-review"]
	if !ok {
		t.Fatalf("%s: missing t-review transition", factoryName)
	}
	if tReview.HITLConfig == nil {
		t.Fatalf("%s: t-review HITLConfig is nil", factoryName)
	}
	if tReview.HITLConfig.A2UIPayloadBuilder == nil {
		t.Fatalf("%s: t-review A2UIPayloadBuilder is nil (INV-005 regression — the legacy WARN branch would fire)", factoryName)
	}
	if tReview.HITLConfig.Prompt != "" {
		t.Errorf("%s: t-review Prompt = %q, want empty string", factoryName, tReview.HITLConfig.Prompt)
	}

	payload, err := tReview.HITLConfig.A2UIPayloadBuilder(nil)
	if err != nil {
		t.Fatalf("%s: A2UIPayloadBuilder returned error: %v", factoryName, err)
	}
	if payload == nil {
		t.Fatalf("%s: A2UIPayloadBuilder returned nil payload", factoryName)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("%s: json.Marshal(payload) failed: %v", factoryName, err)
	}
	body := string(data)
	for _, needle := range []string{"hitl:approve", "hitl:revise", "hitl:reject"} {
		if !strings.Contains(body, needle) {
			t.Errorf("%s: A2UI payload missing %q; body=%s", factoryName, needle, body)
		}
	}
}

func TestDefaultTopologyFactory_TReview_BuilderEmitsAllActions(t *testing.T) {
	// defaultTopologyFactory does not include t-review; skip if so. The hitl
	// topology factory is the "default" surface that exposes t-review in
	// the non-unified path (spec references "default topology" at line
	// 241-245 of topologies.go — that block is inside hitlTopologyFactory).
	c := hitlTopologyFactory("test-session")
	assertTReviewBuilderEmitsActions(t, "hitlTopologyFactory", c)
}

func TestUnifiedTopologyFactory_TReview_BuilderEmitsAllActions(t *testing.T) {
	c := unifiedTopologyFactory("test-session")
	assertTReviewBuilderEmitsActions(t, "unifiedTopologyFactory", c)
}
