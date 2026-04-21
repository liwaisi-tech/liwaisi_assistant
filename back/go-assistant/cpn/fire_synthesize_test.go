package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// ── SafeRegistry stub used only for synth tests ───────────────────────────

type stubSafeRegistry struct{}

func (stubSafeRegistry) Lookup(name string) (string, bool) {
	switch name {
	case "exec-noop":
		return "executor", true
	case "guard-json-nonempty":
		return "guard", true
	}
	return "", false
}

func (stubSafeRegistry) Catalogue() json.RawMessage {
	return json.RawMessage(`[{"name":"exec-noop","kind":"executor"}]`)
}

func (stubSafeRegistry) Names() []string { return []string{"exec-noop", "guard-json-nonempty"} }

// ── AuthoredFlowRepository stub ───────────────────────────────────────────

type stubFlowRepo struct {
	mu        sync.Mutex
	saveCalls int
	byID      map[string]*AuthoredFlowRecord
	lastProv  AuthoredFlowProvenance
}

func newStubFlowRepo() *stubFlowRepo {
	return &stubFlowRepo{byID: make(map[string]*AuthoredFlowRecord)}
}

func (s *stubFlowRepo) SaveAuthored(_ context.Context, canonical json.RawMessage, summary string, sp, st int, refs []string, prov AuthoredFlowProvenance) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCalls++
	s.lastProv = prov
	flowID := "flow-" + canonicalHashFake(canonical)
	if _, ok := s.byID[flowID]; ok {
		return flowID, false, nil
	}
	s.byID[flowID] = &AuthoredFlowRecord{
		FlowID:          flowID,
		TopologyJSON:    canonical,
		Summary:         summary,
		SizePlaces:      sp,
		SizeTransitions: st,
		SafeLintPassed:  true,
		Provenance:      prov,
	}
	_ = refs
	return flowID, true, nil
}

func (s *stubFlowRepo) GetByID(_ context.Context, id string) (*AuthoredFlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return rec, nil
}

func (s *stubFlowRepo) Reject(_ context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return errors.New("not found")
	}
	rec.Rejected = true
	rec.RejectedReason = reason
	return nil
}

func (s *stubFlowRepo) ListAuthored(_ context.Context, _ int) ([]*AuthoredFlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*AuthoredFlowRecord, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, r)
	}
	return out, nil
}

// canonicalHashFake is a tiny deterministic digest so the stub's flow IDs
// are stable across identical inputs. The real hash is sha256 in
// production, but the test only needs determinism.
func canonicalHashFake(b []byte) string {
	var sum uint64 = 14695981039346656037
	for _, c := range b {
		sum ^= uint64(c)
		sum *= 1099511628211
	}
	return strings.ToLower(strings.TrimSpace(string(rune('a'+int(sum%26)))) + "-" + itoaSimple(int(sum%1000000)))
}

func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ── Test ──────────────────────────────────────────────────────────────────

func TestFireSynthesize_PersistsAndDepositsFlowRef(t *testing.T) {
	// Install the real linter / canonicaliser / digest — the minimal
	// json.RawMessage form is sufficient.
	resetSynthesisHooks(t)

	SetLintTopology(func(raw json.RawMessage, _ SafeRegistryPort, _ SizeCap) LintResultPort {
		return passThroughLint{}
	})
	SetCanonicaliseTopology(func(raw json.RawMessage) ([]byte, error) {
		return append([]byte(nil), raw...), nil
	})
	SetTopologyDigest(func(raw json.RawMessage) TopologyDigest {
		return TopologyDigest{Name: "echo-topology", Role: "echo", SizePlaces: 2, SizeTransitions: 1}
	})

	topologyJSON := `{"id":"echo","role":"echo","places":{"p-in":{"id":"p-in","color":"STRING","space":"computation"},"p-out":{"id":"p-out","color":"ARTIFACT","space":"computation"}},"transitions":{"t":{"id":"t","kind":"tool","inputPlaces":["p-in"],"outputPlaces":["p-out"],"executorFunc":"exec-noop"}}}`
	llmResponse := "<topology>" + topologyJSON + "</topology>"

	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: llmResponse}, nil
		},
	}
	repo := newStubFlowRepo()

	in := NewPlace("p-task", ColorString, SpaceComputation)
	out := NewPlace("p-flowref", ColorFlowRef, SpaceComputation)
	tr := NewTransition("t-synth", NodeKindSynthesize, []string{"p-task"}, []string{"p-flowref"})
	tr.SynthesizeConfig = &SynthesizeConfig{
		Model:       "test-model",
		MaxTokens:   200,
		Temperature: 0,
	}
	c := NewCPN("synth-cpn", "synth", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-task": in, "p-flowref": out},
		map[string]*Transition{"t-synth": tr},
	)
	c.LLMClient = mock
	c.FlowRepository = repo
	c.SafeRegistry = stubSafeRegistry{}

	tok := Token{Color: ColorString, Payload: "retrieve URL title", Space: SpaceComputation}

	snaps, _, err := fireSynthesize(context.Background(), tr, c, []Token{tok})
	if err != nil {
		t.Fatalf("fireSynthesize: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	if repo.saveCalls != 1 {
		t.Fatalf("want 1 SaveAuthored call, got %d", repo.saveCalls)
	}
	if repo.lastProv.AuthoredByCPNID != "synth-cpn" {
		t.Errorf("provenance CPN id mismatch: %q", repo.lastProv.AuthoredByCPNID)
	}
	if len(out.Tokens) != 1 {
		t.Fatalf("want 1 token on output, got %d", len(out.Tokens))
	}
	fr, ok := out.Tokens[0].Payload.(FlowRef)
	if !ok {
		t.Fatalf("output payload is %T, want FlowRef", out.Tokens[0].Payload)
	}
	if fr.FlowID == "" {
		t.Errorf("empty flow id")
	}
}

func TestFireSynthesize_LintFailureRoutesError(t *testing.T) {
	resetSynthesisHooks(t)
	SetLintTopology(func(raw json.RawMessage, _ SafeRegistryPort, _ SizeCap) LintResultPort {
		return failingLint{}
	})
	SetCanonicaliseTopology(func(raw json.RawMessage) ([]byte, error) {
		return raw, nil
	})

	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `<topology>{"id":"bad"}</topology>`}, nil
		},
	}

	in := NewPlace("p-task", ColorString, SpaceComputation)
	out := NewPlace("p-flowref", ColorFlowRef, SpaceComputation)
	tr := NewTransition("t-synth", NodeKindSynthesize, []string{"p-task"}, []string{"p-flowref"})
	tr.SynthesizeConfig = &SynthesizeConfig{MaxTokens: 200}
	c := NewCPN("synth-cpn", "synth", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-task": in, "p-flowref": out},
		map[string]*Transition{"t-synth": tr},
	)
	c.LLMClient = mock
	c.FlowRepository = newStubFlowRepo()
	c.SafeRegistry = stubSafeRegistry{}

	_, _, err := fireSynthesize(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "task"}})
	if err == nil {
		t.Fatalf("expected lint failure, got nil")
	}
	if !errors.Is(err, ErrUnsafePrimitive) {
		t.Errorf("expected wrapped ErrUnsafePrimitive, got %v", err)
	}
}

type failingLint struct{}

func (failingLint) Passed() bool { return false }
func (failingLint) Err() error   { return ErrUnsafePrimitive }

// resetSynthesisHooks restores the package-level hooks between tests so
// ordering does not leak state.
func resetSynthesisHooks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetLintTopology(nil)
		SetCanonicaliseTopology(nil)
		SetMaterialiseTopology(nil)
		SetTopologyDigest(nil)
	})
}
