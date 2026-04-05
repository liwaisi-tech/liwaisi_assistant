package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// helpers ────────────────────────────────────────────────────────────────────

func dummyExecutor(_ context.Context, in cpn.Token) (cpn.Token, error) {
	return in, nil
}

func anotherExecutor(_ context.Context, in cpn.Token) (cpn.Token, error) {
	return cpn.Token{Color: cpn.ColorJSON, Payload: "other"}, nil
}

func systemSchema(name string) *ToolSchema {
	return &ToolSchema{
		Name:        name,
		Namespace:   "system",
		Description: "system tool " + name,
		InputColor:  cpn.ColorString,
		OutputColor: cpn.ColorJSON,
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Version:     "1.0.0",
	}
}

func userSchema(name string) *ToolSchema {
	return &ToolSchema{
		Name:        name,
		Namespace:   "user",
		Description: "user tool " + name,
		InputColor:  cpn.ColorJSON,
		OutputColor: cpn.ColorArtifact,
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Version:     "0.1.0",
	}
}

// tests ─────────────────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(systemSchema("search"), dummyExecutor); err != nil {
		t.Fatalf("register system tool: %v", err)
	}
	if err := r.Register(userSchema("my-tool"), dummyExecutor); err != nil {
		t.Fatalf("register user tool: %v", err)
	}

	all := r.ListAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(all))
	}
}

func TestRegister_DuplicateReject(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(systemSchema("search"), dummyExecutor); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := r.Register(systemSchema("search"), anotherExecutor)
	if err == nil {
		t.Fatal("expected error for duplicate, got nil")
	}
	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf("expected ErrToolAlreadyRegistered, got %v", err)
	}
}

func TestRegister_SealedSystemReject(t *testing.T) {
	r := NewRegistry()
	r.Seal()

	err := r.Register(systemSchema("late-system"), dummyExecutor)
	if err == nil {
		t.Fatal("expected error for sealed system registration, got nil")
	}
	if !errors.Is(err, ErrSystemNamespaceSealed) {
		t.Fatalf("expected ErrSystemNamespaceSealed, got %v", err)
	}
}

func TestRegister_UserAfterSeal(t *testing.T) {
	r := NewRegistry()
	r.Seal()

	if err := r.Register(userSchema("allowed"), dummyExecutor); err != nil {
		t.Fatalf("user tool after seal should succeed: %v", err)
	}
}

func TestRegister_InvalidSchema(t *testing.T) {
	r := NewRegistry()

	cases := []struct {
		name   string
		schema *ToolSchema
	}{
		{"nil schema", nil},
		{"empty name", &ToolSchema{Namespace: "system"}},
		{"empty namespace", &ToolSchema{Name: "foo"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Register(tc.schema, dummyExecutor)
			if !errors.Is(err, ErrInvalidToolSchema) {
				t.Fatalf("expected ErrInvalidToolSchema, got %v", err)
			}
		})
	}
}

func TestResolve_Found(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)

	entry, ok := r.Resolve("system/search")
	if !ok {
		t.Fatal("expected to find system/search")
	}
	if entry.Schema.Name != "search" {
		t.Fatalf("expected name 'search', got %q", entry.Schema.Name)
	}
	if entry.Executor == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestResolve_NotFound(t *testing.T) {
	r := NewRegistry()

	_, ok := r.Resolve("system/nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestList_ByNamespace(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("a"), dummyExecutor)
	_ = r.Register(systemSchema("b"), dummyExecutor)
	_ = r.Register(userSchema("c"), dummyExecutor)

	sys := r.List("system")
	if len(sys) != 2 {
		t.Fatalf("expected 2 system tools, got %d", len(sys))
	}

	usr := r.List("user")
	if len(usr) != 1 {
		t.Fatalf("expected 1 user tool, got %d", len(usr))
	}

	empty := r.List("nonexistent")
	if len(empty) != 0 {
		t.Fatalf("expected 0 tools for unknown namespace, got %d", len(empty))
	}
}

func TestListAll(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("x"), dummyExecutor)
	_ = r.Register(userSchema("y"), dummyExecutor)

	all := r.ListAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(all))
	}
}

func TestAsLLMTools(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)
	_ = r.Register(userSchema("custom"), dummyExecutor)

	tools := r.AsLLMTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 LLMTools, got %d", len(tools))
	}

	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("tool %q has nil parameters", tool.Name)
		}
	}

	if !found["system/search"] {
		t.Error("missing system/search in LLMTools output")
	}
	if !found["user/custom"] {
		t.Error("missing user/custom in LLMTools output")
	}
}

func TestAsLLMTools_FilterByNamespace(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("a"), dummyExecutor)
	_ = r.Register(systemSchema("b"), dummyExecutor)
	_ = r.Register(userSchema("c"), dummyExecutor)

	sysOnly := r.AsLLMTools("system")
	if len(sysOnly) != 2 {
		t.Fatalf("expected 2 system LLMTools, got %d", len(sysOnly))
	}
	for _, tool := range sysOnly {
		if tool.Name != "system/a" && tool.Name != "system/b" {
			t.Errorf("unexpected tool in system filter: %s", tool.Name)
		}
	}

	usrOnly := r.AsLLMTools("user")
	if len(usrOnly) != 1 {
		t.Fatalf("expected 1 user LLMTool, got %d", len(usrOnly))
	}
}

func TestInjectIntoFuncRegistry(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("web.search"), dummyExecutor)
	_ = r.Register(userSchema("my.tool"), anotherExecutor)

	fr := persist.NewFuncRegistry()
	r.InjectIntoFuncRegistry(fr)

	// "system/web.search" -> "tool-exec-system-web-search"
	exec1, ok := fr.LookupExecutor("tool-exec-system-web-search")
	if !ok {
		t.Fatal("expected tool-exec-system-web-search in FuncRegistry")
	}
	if exec1 == nil {
		t.Fatal("executor is nil")
	}

	// "user/my.tool" -> "tool-exec-user-my-tool"
	exec2, ok := fr.LookupExecutor("tool-exec-user-my-tool")
	if !ok {
		t.Fatal("expected tool-exec-user-my-tool in FuncRegistry")
	}
	if exec2 == nil {
		t.Fatal("executor is nil")
	}
}

func TestInjectIntoFuncRegistry_NilExecutorSkipped(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("no-exec"), nil)

	fr := persist.NewFuncRegistry()
	r.InjectIntoFuncRegistry(fr)

	_, ok := fr.LookupExecutor("tool-exec-system-no-exec")
	if ok {
		t.Fatal("nil executor should not be injected")
	}
}

func TestQualifiedName(t *testing.T) {
	s := &ToolSchema{Name: "search", Namespace: "system"}
	if got := s.QualifiedName(); got != "system/search" {
		t.Fatalf("expected 'system/search', got %q", got)
	}
}

func TestIsSealed(t *testing.T) {
	r := NewRegistry()
	if r.IsSealed() {
		t.Fatal("new registry should not be sealed")
	}
	r.Seal()
	if !r.IsSealed() {
		t.Fatal("registry should be sealed after Seal()")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*3)

	// Concurrent registrations (unique names per goroutine).
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := &ToolSchema{
				Name:      "tool-" + itoa(idx),
				Namespace: "user",
				Version:   "1.0.0",
			}
			if err := r.Register(s, dummyExecutor); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent reads while registrations are happening.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.ListAll()
			_ = r.List("user")
			_ = r.AsLLMTools()
			_, _ = r.Resolve("user/tool-0")
			_ = r.IsSealed()
		}()
	}

	// Concurrent seal.
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Seal()
	}()

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

// ── InjectIntoCPN tests ─────────────────────────────────────────────────────

func TestInjectIntoCPN_PopulatesToolMeta(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)

	c := &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"t-search": {
				ID:       "t-search",
				Kind:     cpn.NodeKindTool,
				ToolName: "system/search",
			},
		},
	}

	r.InjectIntoCPN(c)

	tr := c.Transitions["t-search"]
	if tr.ToolMeta == nil {
		t.Fatal("expected ToolMeta to be populated")
	}
	if tr.ToolMeta.Description != "system tool search" {
		t.Errorf("description = %q, want %q", tr.ToolMeta.Description, "system tool search")
	}
	if tr.ToolMeta.Namespace != "system" {
		t.Errorf("namespace = %q, want %q", tr.ToolMeta.Namespace, "system")
	}
	if tr.ToolMeta.Parameters == nil {
		t.Error("expected non-nil Parameters")
	}
}

func TestInjectIntoCPN_SkipsNonToolTransitions(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)

	c := &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"t-llm": {
				ID:   "t-llm",
				Kind: cpn.NodeKindLLM,
				// No ToolName — this is an LLM transition.
			},
		},
	}

	r.InjectIntoCPN(c)

	tr := c.Transitions["t-llm"]
	if tr.ToolMeta != nil {
		t.Error("expected nil ToolMeta for non-tool transition")
	}
}

func TestInjectIntoCPN_PopulatesNilExecutor(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)

	c := &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"t-search": {
				ID:       "t-search",
				Kind:     cpn.NodeKindTool,
				ToolName: "system/search",
				// Executor is nil — should be populated from registry.
			},
		},
	}

	r.InjectIntoCPN(c)

	tr := c.Transitions["t-search"]
	if tr.Executor == nil {
		t.Fatal("expected Executor to be populated from registry")
	}
}

func TestInjectIntoCPN_PreservesExistingExecutor(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(systemSchema("search"), dummyExecutor)

	existingCalled := false
	existingExec := func(_ context.Context, in cpn.Token) (cpn.Token, error) {
		existingCalled = true
		return in, nil
	}

	c := &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"t-search": {
				ID:       "t-search",
				Kind:     cpn.NodeKindTool,
				ToolName: "system/search",
				Executor: existingExec,
			},
		},
	}

	r.InjectIntoCPN(c)

	tr := c.Transitions["t-search"]
	// Call the executor — it should be the original one.
	_, _ = tr.Executor(context.Background(), cpn.Token{})
	if !existingCalled {
		t.Error("expected existing executor to be preserved, but it was replaced")
	}

	// ToolMeta should still be populated even when Executor is preserved.
	if tr.ToolMeta == nil {
		t.Fatal("expected ToolMeta to be populated even with existing executor")
	}
}

// itoa avoids importing strconv for a tiny helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
