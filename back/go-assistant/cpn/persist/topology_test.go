package persist

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Shared function instances for registry + CPN construction (same pointer identity).
var (
	testGuardFn       = func(_ []*cpn.Token) bool { return true }
	testExecutorFn    = func(_ context.Context, t cpn.Token) (cpn.Token, error) { return t, nil }
	testRetryOnFn     = func(_ error, _ int) bool { return true }
	testFactoryFn     = func() *cpn.CPN { return &cpn.CPN{ID: "sub", Role: "worker"} }
	testEventFilterFn = func(e cpn.Event) bool { return e.TransitionKind == cpn.NodeKindLLM }
	testValidateFn    = func(_ any) error { return nil }
	testOnSuccessFn   = func(v any) any { return v }
	testSchemaFn      = func() any { return struct{ Name string }{} }
)

func setupTestRegistry() *FuncRegistry {
	r := NewFuncRegistry()
	r.RegisterGuard("always-true", testGuardFn)
	r.RegisterExecutor("echo-tool", testExecutorFn)
	r.RegisterRetryOn("retry-all", testRetryOnFn)
	r.RegisterSubNetFactory("test-subnet", testFactoryFn)
	r.RegisterEventFilter("llm-only", testEventFilterFn)
	r.RegisterValidateFunc("json-validator", testValidateFn)
	r.RegisterOnSuccess("extract", testOnSuccessFn)
	r.RegisterSchema("test-schema", testSchemaFn)
	return r
}

func makeTestCPN() *cpn.CPN {
	return &cpn.CPN{
		ID:    "root-cpn",
		Role:  "coordinator",
		Depth: 0,
		Mode:  cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p-in":  {ID: "p-in", Color: cpn.ColorString, Space: cpn.SpaceSurface},
			"p-out": {ID: "p-out", Color: cpn.ColorJSON, Space: cpn.SpaceComputation},
		},
		Transitions: map[string]*cpn.Transition{
			"t-tool": {
				ID:           "t-tool",
				Kind:         cpn.NodeKindTool,
				InputPlaces:  []string{"p-in"},
				OutputPlaces: []string{"p-out"},
				Guard:        testGuardFn,
				Executor:     testExecutorFn,
				ToolName:     "search",
			},
		},
	}
}

func TestMarshalCPN_Simple(t *testing.T) {
	reg := setupTestRegistry()
	c := makeTestCPN()

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN: %v", err)
	}

	if topo.ID != "root-cpn" {
		t.Fatalf("want root-cpn, got %s", topo.ID)
	}
	if topo.Role != "coordinator" {
		t.Fatalf("want coordinator, got %s", topo.Role)
	}
	if len(topo.Places) != 2 {
		t.Fatalf("want 2 places, got %d", len(topo.Places))
	}
	if len(topo.Transitions) != 1 {
		t.Fatalf("want 1 transition, got %d", len(topo.Transitions))
	}

	tt := topo.Transitions["t-tool"]
	if tt.GuardFunc != "always-true" {
		t.Fatalf("want always-true guard, got %q", tt.GuardFunc)
	}
	if tt.ExecutorFunc != "echo-tool" {
		t.Fatalf("want echo-tool executor, got %q", tt.ExecutorFunc)
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	if restored.ID != c.ID {
		t.Fatalf("round-trip: want %s, got %s", c.ID, restored.ID)
	}
	if restored.Transitions["t-tool"].Guard == nil {
		t.Fatal("round-trip: guard should be restored")
	}
	if restored.Transitions["t-tool"].Executor == nil {
		t.Fatal("round-trip: executor should be restored")
	}
}

func TestMarshalCPN_FullTopology(t *testing.T) {
	reg := setupTestRegistry()

	strict := true
	allowFB := false
	enabled := true
	exclude := false

	c := &cpn.CPN{
		ID:    "full",
		Role:  "orchestrator",
		Depth: 0,
		Mode:  cpn.ModeCentaurian,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
			"p2": {ID: "p2", Color: cpn.ColorJSON, Space: cpn.SpaceComputation},
			"p3": {ID: "p3", Color: cpn.ColorError, Space: cpn.SpaceObservation},
		},
		Transitions: map[string]*cpn.Transition{
			"t-llm": {
				ID:           "t-llm",
				Kind:         cpn.NodeKindLLM,
				InputPlaces:  []string{"p1"},
				OutputPlaces: []string{"p2"},
				ErrorPlace:   "p3",
				SystemPrompt: "You are a helpful assistant.",
				LLMTools:     []string{"t-tool-1", "t-tool-2"},
				LLMConfig: &cpn.LLMConfig{
					Model:          "gpt-4",
					FallbackModels: []string{"gpt-3.5"},
					Endpoint:       "chat",
					MaxTokens:      1000,
					Temperature:    0.7,
					StreamOutput:   true,
					RequireJSON:    true,
					Budget:         0.50,
					SkipHistory:    true,
					JSONSchema: &cpn.JSONSchemaConfig{
						Name: "response", Description: "structured response",
						Schema: json.RawMessage(`{"type":"object"}`), Strict: &strict,
					},
					Provider: &cpn.ProviderConfig{
						Order: []string{"openai"}, Sort: "price",
						AllowFallbacks: &allowFB, ZDR: true,
						MaxPrice: &cpn.ProviderMaxPrice{Prompt: "0.01", Completion: "0.02"},
					},
					Trace:   &cpn.TraceConfig{TraceID: "tr1", TraceName: "test"},
					Plugins: []string{"web", "moderation"},
					Reasoning: &cpn.ReasoningConfig{
						Effort: "high", MaxTokens: 500, Enabled: &enabled, Exclude: &exclude,
					},
					CacheControl: &cpn.CacheControlConfig{TTL: "3600"},
				},
				Retry: &cpn.RetryPolicy{
					MaxAttempts: 3,
					InitialWait: 100 * time.Millisecond,
					MaxWait:     5 * time.Second,
					Multiplier:  2.0,
					RetryOn:     testRetryOnFn,
					CircuitBreaker: &cpn.CircuitBreakerConfig{
						FailureThreshold: 5,
						OpenDuration:     30 * time.Second,
					},
				},
			},
			"t-hitl": {
				ID:          "t-hitl",
				Kind:        cpn.NodeKindHITL,
				InputPlaces: []string{"p2"},
				HITLConfig: &cpn.HITLConfig{
					Prompt:          "Review this?",
					RevisionLoop:    true,
					CorrectionLLMID: "t-llm",
					MaxRevisions:    3,
				},
			},
			"t-validate": {
				ID:          "t-validate",
				Kind:        cpn.NodeKindValidate,
				InputPlaces: []string{"p2"},
				ValidateConfig: &cpn.ValidateConfig{
					ValidateFunc:    testValidateFn,
					OnSuccess:       testOnSuccessFn,
					MaxCorrections:  5,
					CorrectionLLMID: "t-llm",
				},
			},
			"t-observer": {
				ID:            "t-observer",
				Kind:          cpn.NodeKindObserver,
				ObservedCPNID: "sub-cpn-1",
				EventFilter:   testEventFilterFn,
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN full: %v", err)
	}

	// Verify LLM config preserved
	llmTopo := topo.Transitions["t-llm"]
	if llmTopo.LLMConfig == nil {
		t.Fatal("LLMConfig should be present")
	}
	if llmTopo.LLMConfig.Model != "gpt-4" {
		t.Fatalf("want gpt-4, got %s", llmTopo.LLMConfig.Model)
	}
	if llmTopo.LLMConfig.Budget != 0.50 {
		t.Fatalf("want 0.50, got %f", llmTopo.LLMConfig.Budget)
	}
	if llmTopo.SystemPrompt != "You are a helpful assistant." {
		t.Fatalf("SystemPrompt not preserved")
	}
	if len(llmTopo.LLMTools) != 2 {
		t.Fatalf("want 2 LLMTools, got %d", len(llmTopo.LLMTools))
	}

	// Verify HITL config
	hitlTopo := topo.Transitions["t-hitl"]
	if hitlTopo.HITLConfig == nil {
		t.Fatal("HITLConfig should be present")
	}
	if hitlTopo.HITLConfig.Prompt != "Review this?" {
		t.Fatalf("HITL prompt: %s", hitlTopo.HITLConfig.Prompt)
	}
	if hitlTopo.HITLConfig.MaxRevisions != 3 {
		t.Fatalf("HITL max revisions: %d", hitlTopo.HITLConfig.MaxRevisions)
	}

	// Verify Validate config
	valTopo := topo.Transitions["t-validate"]
	if valTopo.ValidateConfig == nil {
		t.Fatal("ValidateConfig should be present")
	}
	if valTopo.ValidateConfig.MaxCorrections != 5 {
		t.Fatalf("MaxCorrections: %d", valTopo.ValidateConfig.MaxCorrections)
	}

	// Verify Observer
	obsTopo := topo.Transitions["t-observer"]
	if obsTopo.ObservedCPNID != "sub-cpn-1" {
		t.Fatalf("ObservedCPNID: %s", obsTopo.ObservedCPNID)
	}
	if obsTopo.EventFilterFunc != "llm-only" {
		t.Fatalf("EventFilterFunc: %s", obsTopo.EventFilterFunc)
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN full: %v", err)
	}

	if restored.Transitions["t-llm"].LLMConfig.Model != "gpt-4" {
		t.Fatal("round-trip: LLMConfig.Model not preserved")
	}
	if restored.Transitions["t-hitl"].HITLConfig.Prompt != "Review this?" {
		t.Fatal("round-trip: HITLConfig.Prompt not preserved")
	}
	if restored.Transitions["t-hitl"].HITLConfig.Channel != nil {
		t.Fatal("round-trip: HITLConfig.Channel should be nil")
	}
	if restored.Transitions["t-observer"].EventFilter == nil {
		t.Fatal("round-trip: EventFilter should be restored")
	}
}

func TestMarshalCPN_LLMConfig(t *testing.T) {
	reg := setupTestRegistry()

	c := &cpn.CPN{
		ID: "llm-test", Role: "llm", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindLLM, InputPlaces: []string{"p1"},
				LLMConfig: &cpn.LLMConfig{
					Model: "claude-3", MaxTokens: 4096, Temperature: 0.3,
					StreamOutput: true, RequireJSON: true, Budget: 1.0,
					FallbackModels: []string{"gpt-4", "gemini"},
				},
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN: %v", err)
	}

	lc := topo.Transitions["t1"].LLMConfig
	if lc.Model != "claude-3" {
		t.Fatalf("Model: %s", lc.Model)
	}
	if lc.MaxTokens != 4096 {
		t.Fatalf("MaxTokens: %d", lc.MaxTokens)
	}
	if len(lc.FallbackModels) != 2 {
		t.Fatalf("FallbackModels: %v", lc.FallbackModels)
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	rc := restored.Transitions["t1"].LLMConfig
	if rc.Model != "claude-3" || rc.MaxTokens != 4096 {
		t.Fatal("LLMConfig round-trip failed")
	}
}

func TestMarshalCPN_ValidateConfig(t *testing.T) {
	reg := setupTestRegistry()

	c := &cpn.CPN{
		ID: "val-test", Role: "validator", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorJSON, Space: cpn.SpaceComputation},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindValidate, InputPlaces: []string{"p1"},
				ValidateConfig: &cpn.ValidateConfig{
					ValidateFunc:    testValidateFn,
					OnSuccess:       testOnSuccessFn,
					MaxCorrections:  3,
					CorrectionLLMID: "t-llm",
				},
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN: %v", err)
	}

	vc := topo.Transitions["t1"].ValidateConfig
	if vc.ValidateFunc != "json-validator" {
		t.Fatalf("ValidateFunc: %s", vc.ValidateFunc)
	}
	if vc.OnSuccessFunc != "extract" {
		t.Fatalf("OnSuccessFunc: %s", vc.OnSuccessFunc)
	}
	if vc.MaxCorrections != 3 {
		t.Fatalf("MaxCorrections: %d", vc.MaxCorrections)
	}
}

func TestMarshalCPN_HITLConfig(t *testing.T) {
	reg := setupTestRegistry()

	c := &cpn.CPN{
		ID: "hitl-test", Role: "hitl", Mode: cpn.ModeCentaurian,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorHuman, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindHITL, InputPlaces: []string{"p1"},
				HITLConfig: &cpn.HITLConfig{
					Channel:         make(chan cpn.Token),
					Prompt:          "Approve?",
					RevisionLoop:    true,
					CorrectionLLMID: "t-llm",
					MaxRevisions:    5,
				},
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN: %v", err)
	}

	hc := topo.Transitions["t1"].HITLConfig
	if hc.Prompt != "Approve?" {
		t.Fatalf("Prompt: %s", hc.Prompt)
	}
	if !hc.RevisionLoop {
		t.Fatal("RevisionLoop should be true")
	}
	if hc.MaxRevisions != 5 {
		t.Fatalf("MaxRevisions: %d", hc.MaxRevisions)
	}

	// Round-trip: Channel should be nil
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	if restored.Transitions["t1"].HITLConfig.Channel != nil {
		t.Fatal("Channel should be nil after unmarshal")
	}
}

func TestMarshalCPN_RetryWithCB(t *testing.T) {
	reg := setupTestRegistry()

	c := &cpn.CPN{
		ID: "retry-test", Role: "retry", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindLLM, InputPlaces: []string{"p1"},
				Retry: &cpn.RetryPolicy{
					MaxAttempts: 3,
					InitialWait: 100 * time.Millisecond,
					MaxWait:     10 * time.Second,
					Multiplier:  2.0,
					RetryOn:     testRetryOnFn,
					CircuitBreaker: &cpn.CircuitBreakerConfig{
						FailureThreshold: 5,
						OpenDuration:     30 * time.Second,
					},
				},
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN: %v", err)
	}

	rp := topo.Transitions["t1"].Retry
	if rp.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts: %d", rp.MaxAttempts)
	}
	if rp.InitialWaitMs != 100 {
		t.Fatalf("InitialWaitMs: %d", rp.InitialWaitMs)
	}
	if rp.CircuitBreaker == nil {
		t.Fatal("CircuitBreaker should be present")
	}
	if rp.CircuitBreaker.FailureThreshold != 5 {
		t.Fatalf("FailureThreshold: %d", rp.CircuitBreaker.FailureThreshold)
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	rrp := restored.Transitions["t1"].Retry
	if rrp.MaxAttempts != 3 {
		t.Fatal("MaxAttempts not preserved")
	}
	if rrp.InitialWait != 100*time.Millisecond {
		t.Fatalf("InitialWait: %v", rrp.InitialWait)
	}
	if rrp.CircuitBreaker.FailureThreshold != 5 {
		t.Fatal("CircuitBreaker not preserved")
	}
}

func TestMarshalCPN_UnregisteredFunc(t *testing.T) {
	reg := NewFuncRegistry() // empty registry

	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindTool, InputPlaces: []string{"p1"},
				Guard: func(_ []*cpn.Token) bool { return true },
			},
		},
	}

	_, err := MarshalCPN(c, reg)
	if err == nil {
		t.Fatal("should error on unregistered guard func")
	}
	if !strings.Contains(err.Error(), "guard func not registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarshalCPN_NilFunc(t *testing.T) {
	reg := NewFuncRegistry()

	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindTool, InputPlaces: []string{"p1"},
				// All funcs nil — should succeed
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN with nil funcs: %v", err)
	}
	if topo.Transitions["t1"].GuardFunc != "" {
		t.Fatal("nil guard should produce empty string")
	}
}

func TestUnmarshalCPN_MissingFunc(t *testing.T) {
	reg := NewFuncRegistry()

	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{
			"p1": {ID: "p1", Color: "STRING", Space: "surface"},
		},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "tool", InputPlaces: []string{"p1"},
				GuardFunc: "nonexistent-guard",
			},
		},
	}

	_, err := UnmarshalCPN(topo, reg)
	if err == nil {
		t.Fatal("should error on missing guard")
	}
	if !strings.Contains(err.Error(), "not found in registry") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalCPN_RuntimeFields(t *testing.T) {
	reg := setupTestRegistry()

	topo := &CPNTopology{
		ID: "test", Mode: "centaurian",
		Places: map[string]PlaceTopology{
			"p1": {ID: "p1", Color: "HUMAN", Space: "surface"},
		},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "hitl", InputPlaces: []string{"p1"},
				HITLConfig: &HITLConfigTopology{
					Prompt: "test", RevisionLoop: true, MaxRevisions: 3,
				},
			},
		},
	}

	c, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}

	// cbState should not be set (it's runtime-only)
	// HITLConfig.Channel should be nil
	if c.Transitions["t1"].HITLConfig.Channel != nil {
		t.Fatal("HITLConfig.Channel should be nil")
	}
}

func TestTopologyHash_Stable(t *testing.T) {
	topo := &CPNTopology{
		ID: "test", Role: "coordinator", Mode: "mas",
		Places: map[string]PlaceTopology{
			"p1": {ID: "p1", Color: "STRING", Space: "surface"},
			"p2": {ID: "p2", Color: "JSON", Space: "computation"},
		},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "llm", InputPlaces: []string{"p1"}, OutputPlaces: []string{"p2"},
				GuardFunc: "always-true",
			},
		},
	}

	h1 := TopologyHash(topo)
	h2 := TopologyHash(topo)
	if h1 != h2 {
		t.Fatalf("hash not stable: %s != %s", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 hex
		t.Fatalf("hash length: %d", len(h1))
	}
}

func TestTopologyHash_Changes(t *testing.T) {
	topo1 := &CPNTopology{
		ID: "test", Mode: "mas",
		Places:      map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {ID: "t1", Kind: "llm"}},
	}

	topo2 := &CPNTopology{
		ID: "test", Mode: "mas",
		Places:      map[string]PlaceTopology{"p1": {ID: "p1", Color: "JSON", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {ID: "t1", Kind: "llm"}},
	}

	if TopologyHash(topo1) == TopologyHash(topo2) {
		t.Fatal("different topologies should produce different hashes")
	}
}

func TestTopologyHash_IgnoresRuntime(t *testing.T) {
	topo1 := &CPNTopology{
		ID: "test", Mode: "mas",
		Places:      map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {ID: "t1", Kind: "llm"}},
	}

	topo2 := &CPNTopology{
		ID: "test", Mode: "centaurian", // different mode
		Places:      map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {ID: "t1", Kind: "llm"}},
	}

	if TopologyHash(topo1) != TopologyHash(topo2) {
		t.Fatal("mode change should not affect hash")
	}
}

func TestTopologyHash_IncludesConfigs(t *testing.T) {
	base := &CPNTopology{
		ID: "test", Mode: "mas",
		Places:      map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {ID: "t1", Kind: "llm"}},
	}

	withLLM := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {
			ID: "t1", Kind: "llm",
			LLMConfig: &LLMConfigTopology{Model: "gpt-4", MaxTokens: 1000},
		}},
	}

	withHITL := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1", Color: "STRING", Space: "surface"}},
		Transitions: map[string]TransitionTopology{"t1": {
			ID: "t1", Kind: "hitl",
			HITLConfig: &HITLConfigTopology{Prompt: "approve?"},
		}},
	}

	h1 := TopologyHash(base)
	h2 := TopologyHash(withLLM)
	h3 := TopologyHash(withHITL)

	if h1 == h2 {
		t.Fatal("LLMConfig should affect hash")
	}
	if h1 == h3 {
		t.Fatal("HITLConfig should affect hash")
	}
	if h2 == h3 {
		t.Fatal("different configs should produce different hashes")
	}
}

func TestMarshalCPN_SubNet(t *testing.T) {
	reg := setupTestRegistry()

	subCPN := &cpn.CPN{
		ID: "sub", Role: "worker", Depth: 1, Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"sp1": {ID: "sp1", Color: cpn.ColorString, Space: cpn.SpaceSurface},
		},
		Transitions: map[string]*cpn.Transition{
			"st1": {ID: "st1", Kind: cpn.NodeKindLLM, InputPlaces: []string{"sp1"}},
		},
	}

	c := &cpn.CPN{
		ID: "parent", Role: "coordinator", Depth: 0, Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorCPN, Space: cpn.SpaceComputation},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindSubNet, InputPlaces: []string{"p1"},
				SubNet: subCPN,
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN with SubNet: %v", err)
	}

	tt := topo.Transitions["t1"]
	if tt.SubNetTopology == nil {
		t.Fatal("SubNetTopology should be present")
	}
	if tt.SubNetTopology.ID != "sub" {
		t.Fatalf("SubNet ID: %s", tt.SubNetTopology.ID)
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	if restored.Transitions["t1"].SubNet == nil {
		t.Fatal("SubNet should be restored")
	}
	if restored.Transitions["t1"].SubNet.ID != "sub" {
		t.Fatal("SubNet ID not preserved")
	}
}

func TestMarshalCPN_SubNetFactory(t *testing.T) {
	reg := setupTestRegistry()

	c := &cpn.CPN{
		ID: "parent", Role: "coordinator", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{
			"p1": {ID: "p1", Color: cpn.ColorCPN, Space: cpn.SpaceComputation},
		},
		Transitions: map[string]*cpn.Transition{
			"t1": {
				ID: "t1", Kind: cpn.NodeKindSubNet, InputPlaces: []string{"p1"},
				SubNetFactory: testFactoryFn,
			},
		},
	}

	topo, err := MarshalCPN(c, reg)
	if err != nil {
		t.Fatalf("MarshalCPN with factory: %v", err)
	}

	tt := topo.Transitions["t1"]
	if tt.FactoryFunc != "test-subnet" {
		t.Fatalf("FactoryFunc: %s", tt.FactoryFunc)
	}
	if tt.SubNetTopology != nil {
		t.Fatal("SubNetTopology should be nil when factory is used")
	}

	// Round-trip
	restored, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}
	if restored.Transitions["t1"].SubNetFactory == nil {
		t.Fatal("SubNetFactory should be restored")
	}
}

func TestUnmarshalCPN_MissingExecutor(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "tool", ExecutorFunc: "missing"},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingFactory(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "subnet", FactoryFunc: "missing"},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingEventFilter(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "observer", EventFilterFunc: "missing"},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingValidateFunc(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "validate",
				ValidateConfig: &ValidateConfigTopology{ValidateFunc: "missing"},
			},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingOnSuccess(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "validate",
				ValidateConfig: &ValidateConfigTopology{OnSuccessFunc: "missing"},
			},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingSchema(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "validate",
				ValidateConfig: &ValidateConfigTopology{SchemaFunc: "missing"},
			},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestUnmarshalCPN_MissingRetryOn(t *testing.T) {
	reg := NewFuncRegistry()
	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "llm",
				Retry: &RetryPolicyTopology{RetryOnFunc: "missing"},
			},
		},
	}
	_, err := UnmarshalCPN(topo, reg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredExecutor(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindTool,
				Executor: func(_ context.Context, t cpn.Token) (cpn.Token, error) { return t, nil },
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "executor func not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredEventFilter(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindObserver,
				EventFilter: func(_ cpn.Event) bool { return true },
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "event filter func not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredValidateFunc(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindValidate,
				ValidateConfig: &cpn.ValidateConfig{
					ValidateFunc: func(_ any) error { return nil },
				},
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "validateFunc not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredOnSuccess(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindValidate,
				ValidateConfig: &cpn.ValidateConfig{
					OnSuccess: func(v any) any { return v },
				},
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "onSuccess func not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredRetryOn(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindLLM,
				Retry: &cpn.RetryPolicy{
					RetryOn: func(_ error, _ int) bool { return true },
				},
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "retryOn func not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestMarshalCPN_UnregisteredSubNetFactory(t *testing.T) {
	reg := NewFuncRegistry()
	c := &cpn.CPN{
		ID: "test", Mode: cpn.ModeMAS,
		Places: map[string]*cpn.Place{"p1": {ID: "p1"}},
		Transitions: map[string]*cpn.Transition{
			"t1": {ID: "t1", Kind: cpn.NodeKindSubNet,
				SubNetFactory: func() *cpn.CPN { return nil },
			},
		},
	}
	_, err := MarshalCPN(c, reg)
	if err == nil || !strings.Contains(err.Error(), "subnet factory func not registered") {
		t.Fatalf("want unregistered error, got %v", err)
	}
}

func TestUnmarshalCPN_ValidateConfigWithSchema(t *testing.T) {
	reg := setupTestRegistry()

	topo := &CPNTopology{
		ID: "test", Mode: "mas",
		Places: map[string]PlaceTopology{"p1": {ID: "p1"}},
		Transitions: map[string]TransitionTopology{
			"t1": {ID: "t1", Kind: "validate",
				ValidateConfig: &ValidateConfigTopology{
					SchemaFunc:      "test-schema",
					ValidateFunc:    "json-validator",
					OnSuccessFunc:   "extract",
					MaxCorrections:  5,
					CorrectionLLMID: "t-llm",
				},
			},
		},
	}

	c, err := UnmarshalCPN(topo, reg)
	if err != nil {
		t.Fatalf("UnmarshalCPN: %v", err)
	}

	vc := c.Transitions["t1"].ValidateConfig
	if vc.Schema == nil {
		t.Fatal("Schema should be populated from factory")
	}
	if vc.ValidateFunc == nil {
		t.Fatal("ValidateFunc should be restored")
	}
	if vc.OnSuccess == nil {
		t.Fatal("OnSuccess should be restored")
	}
	if vc.MaxCorrections != 5 {
		t.Fatalf("MaxCorrections: %d", vc.MaxCorrections)
	}
}
