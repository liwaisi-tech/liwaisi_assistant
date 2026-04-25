package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"slices"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

type stubUserToolRegistry struct {
	entries []*tools.ToolEntry
}

func (s *stubUserToolRegistry) InjectIntoCPN(*cpn.CPN)                  {}
func (s *stubUserToolRegistry) Resolve(string) (*tools.ToolEntry, bool) { return nil, false }
func (s *stubUserToolRegistry) ListUserAuthored() []*tools.ToolEntry    { return s.entries }
func (s *stubUserToolRegistry) RegisterManifest(context.Context, cpn.ToolManifest) (cpn.ToolManifestResult, error) {
	return cpn.ToolManifestResult{}, nil
}

type fakeExecAdapter struct {
	lastReq cpn.ExecRequest
	result  cpn.ExecResult
}

func (f *fakeExecAdapter) Exec(_ context.Context, req cpn.ExecRequest) (cpn.ExecResult, error) {
	f.lastReq = req
	return f.result, nil
}
func (f *fakeExecAdapter) SpawnPTY(context.Context, cpn.PTYRequest) (cpn.PTYHandle, error) {
	return cpn.PTYHandle{}, nil
}
func (f *fakeExecAdapter) KillPID(context.Context, int, cpn.Signal) error { return nil }
func (f *fakeExecAdapter) ReadFile(context.Context, string) ([]byte, error) {
	return nil, nil
}
func (f *fakeExecAdapter) WriteFile(context.Context, string, []byte, fs.FileMode) error {
	return nil
}
func (f *fakeExecAdapter) Stat(context.Context, string) (cpn.FileInfo, error) {
	return cpn.FileInfo{}, nil
}

func newLLMHost() *cpn.Transition {
	t := cpn.NewTransition("t-direct", cpn.NodeKindLLM, []string{"p-in"}, []string{"p-out"})
	t.LLMTools = []string{"bash_exec"}
	return t
}

func newRoot() *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-in":  cpn.NewPlace("p-in", cpn.ColorString, cpn.SpaceComputation),
		"p-out": cpn.NewPlace("p-out", cpn.ColorString, cpn.SpaceComputation),
	}
	transitions := map[string]*cpn.Transition{
		"t-direct":  newLLMHost(),
		"bash_exec": cpn.NewTransition("bash_exec", cpn.NodeKindTool, []string{}, []string{}),
	}
	return cpn.NewCPN("cpn-test", "test-flow", 0, cpn.ModeMAS, "sess-1", places, transitions)
}

func TestMaterialiseUserTools_AttachesTransitionsAndExtendsLLMTools(t *testing.T) {
	root := newRoot()
	reg := &stubUserToolRegistry{entries: []*tools.ToolEntry{
		{
			Namespace:  "user",
			Name:       "brae-monitor",
			Version:    "0.1.0",
			Origin:     tools.OriginUserAuthored,
			BinaryPath: "/home/test/.local/bin/brae-monitor",
			HelpText:   "Monitor dinámico",
		},
		{
			Namespace:  "user",
			Name:       "deprecated-tool",
			Origin:     tools.OriginUserAuthored,
			BinaryPath: "/home/test/.local/bin/dep",
			Deprecated: true,
		},
	}}
	rt := &cpn.HostRuntime{Adapter: &fakeExecAdapter{}}

	attached := materialiseUserTools(root, reg, rt, slog.Default())

	if len(attached) != 1 || attached[0] != "brae-monitor" {
		t.Fatalf("expected [brae-monitor], got %v", attached)
	}
	tr, ok := root.Transitions["brae-monitor"]
	if !ok {
		t.Fatal("brae-monitor transition not attached to root")
	}
	if tr.ToolName != "brae-monitor" {
		t.Fatalf("ToolName = %q", tr.ToolName)
	}
	if tr.ToolMeta == nil || !tr.ToolMeta.RequiresHITL {
		t.Fatal("ToolMeta missing or RequiresHITL=false")
	}
	if tr.Executor == nil {
		t.Fatal("executor not attached")
	}
	llm := root.Transitions["t-direct"]
	if !slices.Contains(llm.LLMTools, "brae-monitor") {
		t.Fatalf("t-direct.LLMTools = %v, want it to include brae-monitor", llm.LLMTools)
	}
	if slices.Contains(llm.LLMTools, "deprecated-tool") {
		t.Fatal("deprecated tool leaked into LLMTools")
	}
}

func TestMaterialiseUserTools_ExecutorForwardsArgsToBinary(t *testing.T) {
	root := newRoot()
	adapter := &fakeExecAdapter{result: cpn.ExecResult{ExitCode: 0, Stdout: []byte("ok")}}
	reg := &stubUserToolRegistry{entries: []*tools.ToolEntry{{
		Namespace:  "user",
		Name:       "brae-monitor",
		Origin:     tools.OriginUserAuthored,
		BinaryPath: "/usr/local/bin/brae-monitor",
	}}}
	rt := &cpn.HostRuntime{Adapter: adapter}

	_ = materialiseUserTools(root, reg, rt, slog.Default())

	args, _ := json.Marshal(map[string]any{"args": []string{"--json"}, "timeout_seconds": 5})
	_, err := root.Transitions["brae-monitor"].Executor(context.Background(),
		cpn.Token{Payload: string(args)})
	if err != nil {
		t.Fatalf("executor err: %v", err)
	}
	if adapter.lastReq.Command != "/usr/local/bin/brae-monitor" {
		t.Fatalf("Command = %q, want the registered binary_path", adapter.lastReq.Command)
	}
	if len(adapter.lastReq.Args) != 1 || adapter.lastReq.Args[0] != "--json" {
		t.Fatalf("Args = %v, want [--json]", adapter.lastReq.Args)
	}
}

func TestMaterialiseUserTools_NoEntriesNoOp(t *testing.T) {
	root := newRoot()
	reg := &stubUserToolRegistry{}
	rt := &cpn.HostRuntime{Adapter: &fakeExecAdapter{}}

	attached := materialiseUserTools(root, reg, rt, slog.Default())

	if len(attached) != 0 {
		t.Fatalf("expected no attachments, got %v", attached)
	}
	if _, has := root.Transitions["brae-monitor"]; has {
		t.Fatal("unexpected transition added when registry empty")
	}
}
