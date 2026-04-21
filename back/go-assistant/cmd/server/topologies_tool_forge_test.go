package main

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestBuildToolForgeTopology_Structure(t *testing.T) {
	c := BuildToolForgeTopology("test-session", ToolForgeDeps{})
	if c == nil {
		t.Fatal("BuildToolForgeTopology returned nil")
	}

	requiredPlaces := []string{
		PlaceForgeRequest,
		PlaceForgeCompilerChoice,
		PlaceForgeSpec,
		PlaceForgeSource,
		PlaceForgeSourceWritten,
		PlaceForgeCompiledBinary,
		PlaceForgeHelpText,
		PlaceForgeManPage,
		PlaceForgeSmokeOK,
		PlaceForgeManifest,
		PlaceForgeRegistered,
		PlaceForgeErrors,
	}
	for _, pid := range requiredPlaces {
		if _, ok := c.Places[pid]; !ok {
			t.Errorf("missing place: %s", pid)
		}
	}

	requiredTransitions := []string{
		"t-probe-compiler",
		"t-design-spec",
		"t-emit-source",
		"t-write-source",
		"t-compile",
		"t-smoke-test",
		"t-write-helptext",
		"t-write-manpage",
		"t-build-manifest",
		"t-register",
	}
	for _, tid := range requiredTransitions {
		if _, ok := c.Transitions[tid]; !ok {
			t.Errorf("missing transition: %s", tid)
		}
	}
}

func TestBuildToolForgeTopology_NodeKinds(t *testing.T) {
	c := BuildToolForgeTopology("test-session", ToolForgeDeps{})

	cases := []struct {
		id   string
		kind cpn.NodeKind
	}{
		{"t-probe-compiler", cpn.NodeKindTool},
		{"t-design-spec", cpn.NodeKindLLM},
		{"t-emit-source", cpn.NodeKindLLM},
		{"t-write-source", cpn.NodeKindBash},
		{"t-compile", cpn.NodeKindBash},
		{"t-smoke-test", cpn.NodeKindBash},
		{"t-write-helptext", cpn.NodeKindLLM},
		{"t-write-manpage", cpn.NodeKindLLM},
		{"t-build-manifest", cpn.NodeKindTool},
		{"t-register", cpn.NodeKindRegisterTool},
	}
	for _, tc := range cases {
		tr, ok := c.Transitions[tc.id]
		if !ok {
			t.Errorf("transition %s missing", tc.id)
			continue
		}
		if tr.Kind != tc.kind {
			t.Errorf("%s: want kind %s, got %s", tc.id, tc.kind, tr.Kind)
		}
	}
}

func TestBuildToolForgeTopology_ArcWiring(t *testing.T) {
	c := BuildToolForgeTopology("test-session", ToolForgeDeps{})

	// t-probe-compiler consumes p-forge-request and outputs p-compiler-choice + p-forge-request.
	tPC := c.Transitions["t-probe-compiler"]
	if !containsStr(tPC.InputPlaces, PlaceForgeRequest) {
		t.Errorf("t-probe-compiler: missing input %s", PlaceForgeRequest)
	}
	if !containsStr(tPC.OutputPlaces, PlaceForgeCompilerChoice) {
		t.Errorf("t-probe-compiler: missing output %s", PlaceForgeCompilerChoice)
	}

	// t-design-spec consumes p-forge-request and p-compiler-choice.
	tDS := c.Transitions["t-design-spec"]
	if !containsStr(tDS.InputPlaces, PlaceForgeRequest) {
		t.Errorf("t-design-spec: missing input %s", PlaceForgeRequest)
	}
	if !containsStr(tDS.InputPlaces, PlaceForgeCompilerChoice) {
		t.Errorf("t-design-spec: missing input %s", PlaceForgeCompilerChoice)
	}
	if !containsStr(tDS.OutputPlaces, PlaceForgeSpec) {
		t.Errorf("t-design-spec: missing output %s", PlaceForgeSpec)
	}

	// t-register outputs p-registered.
	tReg := c.Transitions["t-register"]
	if !containsStr(tReg.OutputPlaces, PlaceForgeRegistered) {
		t.Errorf("t-register: missing output %s", PlaceForgeRegistered)
	}
}

func TestBuildToolForgeTopology_LLMConfigs(t *testing.T) {
	c := BuildToolForgeTopology("test-session", ToolForgeDeps{})

	llmTransitions := []string{"t-design-spec", "t-emit-source", "t-write-helptext", "t-write-manpage"}
	for _, tid := range llmTransitions {
		tr := c.Transitions[tid]
		if tr.LLMConfig == nil {
			t.Errorf("%s: LLMConfig is nil", tid)
			continue
		}
		if tr.LLMConfig.StreamOutput {
			t.Errorf("%s: StreamOutput should be false per REQ-032", tid)
		}
	}

	// Design-spec and emit-source must use RequireJSON.
	for _, tid := range []string{"t-design-spec", "t-emit-source"} {
		tr := c.Transitions[tid]
		if tr.LLMConfig == nil || !tr.LLMConfig.RequireJSON {
			t.Errorf("%s: RequireJSON must be true per REQ-032", tid)
		}
	}
}

func TestBuildToolForgeTopology_BashTimeouts(t *testing.T) {
	c := BuildToolForgeTopology("test-session", ToolForgeDeps{})

	tCompile := c.Transitions["t-compile"]
	if tCompile.BashConfig == nil {
		t.Fatal("t-compile: BashConfig is nil")
	}
	if tCompile.BashConfig.Timeout != compileTimeout {
		t.Errorf("t-compile: want timeout %v, got %v", compileTimeout, tCompile.BashConfig.Timeout)
	}

	tSmoke := c.Transitions["t-smoke-test"]
	if tSmoke.BashConfig == nil {
		t.Fatal("t-smoke-test: BashConfig is nil")
	}
	if tSmoke.BashConfig.Timeout != smokeTimeout {
		t.Errorf("t-smoke-test: want timeout %v, got %v", smokeTimeout, tSmoke.BashConfig.Timeout)
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
