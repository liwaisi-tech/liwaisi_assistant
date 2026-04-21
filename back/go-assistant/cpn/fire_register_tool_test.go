package cpn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeToolRegistry is an in-process ToolRegistry double for fire_register_tool
// tests. It records manifests and returns canned errors.
type fakeToolRegistry struct {
	manifests []ToolManifest
	nextErr   error
	nextID    string
}

func (f *fakeToolRegistry) RegisterManifest(_ context.Context, m ToolManifest) (ToolManifestResult, error) {
	f.manifests = append(f.manifests, m)
	if f.nextErr != nil {
		return ToolManifestResult{}, f.nextErr
	}
	id := f.nextID
	if id == "" {
		id = "auto-id"
	}
	return ToolManifestResult{
		QualifiedName: m.Namespace + "/" + m.Name + "@" + m.Version,
		ID:            id,
	}, nil
}

// fakeLedger satisfies FirstRunLedger.
type fakeLedger struct{ err error }

func (f *fakeLedger) Approve(_ context.Context, _, _ string) error { return f.err }

func newRegisterToolHarness(t *testing.T, reg ToolRegistry, ledger FirstRunLedger) (*CPN, *Transition, *Place, *Place) {
	t.Helper()
	in := NewPlace("p-in", ColorToolManifest, SpaceComputation)
	out := NewPlace("p-out", ColorArtifact, SpaceComputation)
	errP := NewPlace("p-err", ColorError, SpaceComputation)
	tr := NewTransition("t-reg", NodeKindRegisterTool, []string{"p-in"}, []string{"p-out"})
	tr.ErrorPlace = "p-err"

	c := NewCPN("cpn-test", "authoring", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-in": in, "p-out": out, "p-err": errP},
		map[string]*Transition{"t-reg": tr},
	)
	c.ToolRegistry = reg
	c.FirstRunLedger = ledger
	return c, tr, out, errP
}

func TestFireRegisterTool_HappyPath(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{nextID: "uuid-123"}
	c, tr, out, _ := newRegisterToolHarness(t, reg, nil)

	manifest := ToolManifest{
		Namespace: "brae",
		Name:      "http-get",
		Version:   "1.0.0",
		Schema:    json.RawMessage(`{"type":"object"}`),
		Origin:    "agent-authored",
	}
	inTok := Token{Color: ColorToolManifest, Payload: manifest, Space: SpaceComputation}

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(reg.manifests) != 1 {
		t.Fatalf("registry called %d times, want 1", len(reg.manifests))
	}
	if len(out.Tokens) != 1 {
		t.Fatalf("output place has %d tokens, want 1", len(out.Tokens))
	}
	result, ok := out.Tokens[0].Payload.(ToolManifestResult)
	if !ok {
		t.Fatalf("output payload = %T, want ToolManifestResult", out.Tokens[0].Payload)
	}
	if result.QualifiedName != "brae/http-get@1.0.0" || result.ID != "uuid-123" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFireRegisterTool_DuplicateBubblesUp(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{nextErr: errors.New("ErrDuplicate: brae/http-get@1.0.0")}
	c, tr, _, _ := newRegisterToolHarness(t, reg, nil)

	inTok := Token{
		Color: ColorToolManifest,
		Payload: ToolManifest{
			Namespace: "brae", Name: "http-get", Version: "1.0.0",
			Schema: json.RawMessage(`{}`), Origin: "agent-authored",
		},
		Space: SpaceComputation,
	}

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFireRegisterTool_ConfigOverridesTokenPayload(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{}
	c, tr, _, _ := newRegisterToolHarness(t, reg, nil)

	tr.RegisterToolConfig = &RegisterToolConfig{
		Namespace: "cfg", Name: "tool", Version: "1.0.0",
		Schema: json.RawMessage(`{}`), Origin: "agent-authored",
	}
	inTok := Token{
		Color: ColorToolManifest,
		Payload: ToolManifest{
			Namespace: "other", Name: "other-tool", Version: "2.0.0",
		},
		Space: SpaceComputation,
	}

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if reg.manifests[0].Namespace != "cfg" || reg.manifests[0].Name != "tool" {
		t.Fatalf("config not used: %+v", reg.manifests[0])
	}
}

func TestFireRegisterTool_FirstRunLedgerRejects(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{}
	ledger := &fakeLedger{err: errors.New("hitl pending")}
	c, tr, _, _ := newRegisterToolHarness(t, reg, ledger)

	dir := t.TempDir()
	bin := filepath.Join(dir, "tool.bin")
	_ = os.WriteFile(bin, []byte("x"), 0o644)
	sum := sha256.Sum256([]byte("x"))

	inTok := Token{
		Color: ColorToolManifest,
		Payload: ToolManifest{
			Namespace: "brae", Name: "bin-tool", Version: "1.0.0",
			Schema:       json.RawMessage(`{}`),
			Origin:       "agent-authored",
			BinaryPath:   bin,
			BinarySHA256: hex.EncodeToString(sum[:]),
		},
		Space: SpaceComputation,
	}

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err == nil {
		t.Fatal("expected ledger rejection, got nil")
	}
	if len(reg.manifests) != 0 {
		t.Fatalf("registry should not be called when ledger rejects (was called %d times)", len(reg.manifests))
	}
}

func TestFireRegisterTool_FirstRunLedgerMissingFallsOpen(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{}
	c, tr, _, _ := newRegisterToolHarness(t, reg, nil) // no ledger

	inTok := Token{
		Color: ColorToolManifest,
		Payload: ToolManifest{
			Namespace: "brae", Name: "bin-tool", Version: "1.0.0",
			Schema:       json.RawMessage(`{}`),
			Origin:       "agent-authored",
			BinaryPath:   "/tmp/x",
			BinarySHA256: "deadbeef",
		},
		Space: SpaceComputation,
	}

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err != nil {
		t.Fatalf("expected fall-open, got %v", err)
	}
	if len(reg.manifests) != 1 {
		t.Fatalf("expected registry call after fall-open, got %d", len(reg.manifests))
	}
}

func TestFireRegisterTool_NoRegistry(t *testing.T) {
	ctx := context.Background()
	c, tr, _, _ := newRegisterToolHarness(t, nil, nil)
	c.ToolRegistry = nil

	_, _, err := fireRegisterTool(ctx, tr, c, []Token{{Color: ColorToolManifest, Payload: ToolManifest{}}})
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
}

func TestFireRegisterTool_JSONBytePayload(t *testing.T) {
	ctx := context.Background()
	reg := &fakeToolRegistry{}
	c, tr, _, _ := newRegisterToolHarness(t, reg, nil)

	manifest := ToolManifest{
		Namespace: "brae", Name: "json-tool", Version: "1.0.0",
		Origin: "agent-authored",
	}
	raw, _ := json.Marshal(manifest)

	inTok := Token{Color: ColorToolManifest, Payload: json.RawMessage(raw), Space: SpaceComputation}
	_, _, err := fireRegisterTool(ctx, tr, c, []Token{inTok})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if reg.manifests[0].Name != "json-tool" {
		t.Fatalf("got %+v", reg.manifests[0])
	}
}
