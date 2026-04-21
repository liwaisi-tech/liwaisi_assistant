package awakens

import (
	"context"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

type fakeRegistry struct {
	calls    []cpn.ToolManifest
	dupAfter int
	errAfter int
}

func (f *fakeRegistry) RegisterManifest(_ context.Context, m cpn.ToolManifest) (cpn.ToolManifestResult, error) {
	f.calls = append(f.calls, m)
	idx := len(f.calls)
	if f.errAfter > 0 && idx > f.errAfter {
		return cpn.ToolManifestResult{}, errors.New("boom")
	}
	if f.dupAfter != 0 && idx > f.dupAfter {
		return cpn.ToolManifestResult{}, errors.New("tool already exists in registry")
	}
	return cpn.ToolManifestResult{
		QualifiedName: AwakeningNamespace + "/" + m.Name + "@" + m.Version,
		ID:            "id-" + m.Name,
	}, nil
}

func TestRegisterBatch_Happy(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "shell-exec", Basis: "sh"},
		{Name: "text-search", Basis: "grep"},
	}}
	got, err := RegisterBatch(context.Background(), reg, r, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 registered, got %d", len(got))
	}
	if len(reg.calls) != 2 {
		t.Fatalf("registry called %d times, want 2", len(reg.calls))
	}
	if reg.calls[0].Namespace != AwakeningNamespace {
		t.Errorf("namespace: got %q", reg.calls[0].Namespace)
	}
}

func TestRegisterBatch_IdempotentOnDup(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{dupAfter: -1} // every call (idx > -1) returns "already exists"
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "already-there", Basis: "sh"},
	}}
	got, err := RegisterBatch(context.Background(), reg, r, nil)
	if err != nil {
		t.Fatalf("dup must be silent no-op: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("dup should not appear in registered list, got %v", got)
	}
}

func TestRegisterBatch_CapBound(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	entries := make([]AwakeningToolRegister, MaxToolsToRegister+5)
	for i := range entries {
		entries[i] = AwakeningToolRegister{Name: "t" + string(rune('a'+i%26)), Basis: "x"}
	}
	r := AwakeningReport{ToolsRegister: entries}
	_, err := RegisterBatch(context.Background(), reg, r, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(reg.calls) > MaxToolsToRegister {
		t.Fatalf("CON-004 breach: %d registrations (cap %d)", len(reg.calls), MaxToolsToRegister)
	}
}

func TestRegisterBatch_NilRegistry(t *testing.T) {
	t.Parallel()
	_, err := RegisterBatch(context.Background(), nil, AwakeningReport{}, nil)
	if err == nil {
		t.Fatalf("want err for nil registry")
	}
}
