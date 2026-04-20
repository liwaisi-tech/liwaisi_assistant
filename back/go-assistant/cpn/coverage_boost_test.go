package cpn

import (
	"context"
	"errors"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────
// write_intent.go: WithWriteIntent (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestWithWriteIntent_StoresAndRetrieves(t *testing.T) {
	ctx := WithWriteIntent(context.Background(), "intent-value")
	if got := ctx.Value(WriteIntentKey); got != "intent-value" {
		t.Fatalf("got %v", got)
	}
}

func TestWithWriteIntent_NilContext(t *testing.T) {
	var ctx context.Context //nolint:staticcheck // SA1012: intentional — covers nil-context guard.
	if got := WithWriteIntent(ctx, "x"); got != nil {
		t.Fatalf("nil ctx must return nil, got %v", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// host_snapshot.go: PeekHostSnapshot / SeedHostSnapshot (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestHostSnapshot_NilCPN(t *testing.T) {
	if _, ok := PeekHostSnapshot(nil); ok {
		t.Fatal("nil CPN should peek false")
	}
	// SeedHostSnapshot on nil is a no-op (shouldn't panic).
	SeedHostSnapshot(nil, "payload")
}

func TestHostSnapshot_NilSnap(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	SeedHostSnapshot(c, nil) // no-op
	if _, ok := PeekHostSnapshot(c); ok {
		t.Fatal("peek should be false when snap was nil")
	}
}

func TestHostSnapshot_Roundtrip(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	SeedHostSnapshot(c, "snap-1")
	got, ok := PeekHostSnapshot(c)
	if !ok {
		t.Fatal("expected snapshot")
	}
	if got.(string) != "snap-1" {
		t.Fatalf("got %v", got)
	}
	// Idempotent: seeding again just deposits another token — Peek still
	// returns a non-empty place.
	SeedHostSnapshot(c, "snap-2")
	got, _ = PeekHostSnapshot(c)
	if got == nil {
		t.Fatal("peek returned nil after reseed")
	}
}

func TestHostSnapshot_PlaceMissing(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	// No place configured, and no seed → peek returns false.
	if _, ok := PeekHostSnapshot(c); ok {
		t.Fatal("expected false")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// cpn.go: PublishProcessEvent, PublishHostApprovalSurface, PublishHITLRequested,
// SessionEventSink (all 0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestPublishProcessEvent_NilSafe(t *testing.T) {
	var c *CPN
	// Should not panic.
	c.PublishProcessEvent(EventProcessStdout, ProcessOutputPayload{})
}

func TestPublishProcessEvent_EmitsThroughBus(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	ch := make(chan Event, 4)
	c.EventEmitter = ch
	c.PublishProcessEvent(EventProcessStdout, ProcessOutputPayload{SessionID: "s1", Line: "hi"})
	select {
	case e := <-ch:
		if e.Type != EventProcessStdout {
			t.Fatalf("type=%s", e.Type)
		}
	default:
		t.Fatal("no event emitted")
	}
}

// denyAllLimiter + countingMetrics exercise the rate-limited-drop branch.
type denyAllLimiter struct{}

func (denyAllLimiter) Allow(string) bool { return false }

type countingMetrics struct {
	emits, drops int
}

func (c *countingMetrics) OnEmit(string, EventType)         { c.emits++ }
func (c *countingMetrics) OnDrop(string, EventType, string) { c.drops++ }

func TestPublishProcessEvent_RateLimited_BumpsDrop(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	m := &countingMetrics{}
	c.HostRuntime = &HostRuntime{RateLimiter: denyAllLimiter{}, Metrics: m}
	c.PublishProcessEvent(EventProcessStdout, ProcessOutputPayload{SessionID: "s1"})
	if m.drops != 1 || m.emits != 0 {
		t.Fatalf("drops=%d emits=%d", m.drops, m.emits)
	}
}

func TestPublishHostApprovalSurface(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	ch := make(chan Event, 2)
	c.EventEmitter = ch
	c.PublishHostApprovalSurface("t-1", A2UIMarker+`{"schema":"host.approval"}`)
	e := <-ch
	if e.Type != EventStreamChunk {
		t.Fatalf("type=%s", e.Type)
	}
	sc, ok := e.Payload.(StreamChunk)
	if !ok || !sc.Done {
		t.Fatalf("payload=%+v", e.Payload)
	}
}

func TestPublishHostApprovalSurface_NilSafe(t *testing.T) {
	var c *CPN
	c.PublishHostApprovalSurface("t", "x")
}

func TestPublishHITLRequested(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	ch := make(chan Event, 2)
	c.EventEmitter = ch
	c.PublishHITLRequested("t-x", HITLRequestedPayload{Prompt: "go?", CustomSurface: true})
	e := <-ch
	if e.Type != EventHITLRequested || e.TransitionID != "t-x" {
		t.Fatalf("e=%+v", e)
	}
}

func TestPublishHITLRequested_NilSafe(t *testing.T) {
	var c *CPN
	c.PublishHITLRequested("t", HITLRequestedPayload{})
}

func TestSessionEventSink_InvokesPublishProcessEvent(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	ch := make(chan Event, 2)
	c.EventEmitter = ch
	sink := c.SessionEventSink()
	sink(Event{Type: EventProcessStdout, Payload: ProcessOutputPayload{SessionID: "s1", Line: "x"}})
	e := <-ch
	if e.Type != EventProcessStdout {
		t.Fatal("not emitted")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// host.go: HostError.Error and Unwrap branches
// ──────────────────────────────────────────────────────────────────────────

func TestHostError_ErrorAndUnwrap(t *testing.T) {
	var nilErr *HostError
	if nilErr.Error() != "" {
		t.Fatal("nil Error")
	}
	if nilErr.Unwrap() != nil {
		t.Fatal("nil Unwrap")
	}
	he := &HostError{Code: "x"}
	if he.Error() != "x" {
		t.Fatalf("code-only: %q", he.Error())
	}
	he = &HostError{Code: "x", Message: "m"}
	if he.Error() != "x: m" {
		t.Fatalf("msg: %q", he.Error())
	}
	wrapped := &HostError{Code: "x", Message: "m", Cause: errors.New("root")}
	if wrapped.Error() == "" {
		t.Fatal("with cause")
	}
	if wrapped.Unwrap() == nil {
		t.Fatal("unwrap")
	}
}

// NoOp / AllowAll helpers (coverage for 100-percent targets — assert
// contract so refactors don't regress).
func TestNoOpMetrics(t *testing.T) {
	var m NoOpProcessEventMetrics
	m.OnEmit("s", EventProcessStdout)
	m.OnDrop("s", EventProcessStdout, "x")
}

func TestAllowAllRateLimiter(t *testing.T) {
	var rl AllowAllRateLimiter
	if !rl.Allow("anything") {
		t.Fatal("should allow")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// tool_registry_port.go: ToManifest nil and populated
// ──────────────────────────────────────────────────────────────────────────

func TestRegisterToolConfig_ToManifest(t *testing.T) {
	var nilCfg *RegisterToolConfig
	if m := nilCfg.ToManifest(); m.Name != "" {
		t.Fatal("nil cfg must return zero")
	}
	c := &RegisterToolConfig{Namespace: "ns", Name: "n", Version: "1", BinaryPath: "/bin/x"}
	m := c.ToManifest()
	if m.Namespace != "ns" || m.Name != "n" || m.BinaryPath != "/bin/x" {
		t.Fatalf("manifest=%+v", m)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// session.go: RegisterToolHITL (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestRegisterToolHITL_HappyAndDuplicate(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{
		"t-1": {ID: "t-1", Kind: NodeKindLLM},
	})
	s := NewSession("sess", "u", ChannelWeb, c)
	ch := make(chan Token, 1)
	if err := s.RegisterToolHITL("t-1", ch); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Duplicate → ErrHITLAlreadyRegistered.
	if err := s.RegisterToolHITL("t-1", ch); !errors.Is(err, ErrHITLAlreadyRegistered) {
		t.Fatalf("dup err = %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// fire_bash.go: splitForEmit branches
// ──────────────────────────────────────────────────────────────────────────

func TestSplitForEmit_EmptyReturnsNil(t *testing.T) {
	if got := splitForEmit(nil, nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSplitForEmit_PerLineDefault(t *testing.T) {
	out := splitForEmit([]byte("a\nb\nc"), nil)
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestSplitForEmit_PerChunk_CustomSize(t *testing.T) {
	in := []byte("0123456789")
	out := splitForEmit(in, &BashConfig{EmitMode: EmitPerChunk, EmitChunkBytes: 4})
	if len(out) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(out))
	}
	if string(out[0]) != "0123" || string(out[2]) != "89" {
		t.Fatalf("chunks=%v", out)
	}
}

func TestSplitForEmit_PerChunk_DefaultSize(t *testing.T) {
	// EmitChunkBytes=0 uses DefaultEmitChunkBytes.
	in := make([]byte, DefaultEmitChunkBytes+5)
	for i := range in {
		in[i] = 'x'
	}
	out := splitForEmit(in, &BashConfig{EmitMode: EmitPerChunk})
	if len(out) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(out))
	}
}

func TestTransitionID_NilAndNonNil(t *testing.T) {
	if transitionID(nil) != "" {
		t.Fatal("nil")
	}
	if transitionID(&Transition{ID: "t"}) != "t" {
		t.Fatal("non-nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// selfEventBus + emit drop accounting: force a saturated self-bus to ensure
// the drop-metric branch fires. Pre-fill selfBus to capacity and then emit
// one more process event.
// ──────────────────────────────────────────────────────────────────────────

// ──────────────────────────────────────────────────────────────────────────
// synthesis_config.go: Resolved (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestSizeCap_Resolved(t *testing.T) {
	def := DefaultSizeCap()
	got := SizeCap{}.Resolved()
	if got != def {
		t.Fatalf("zero Resolved = %+v, want %+v", got, def)
	}
	custom := SizeCap{MaxPlaces: 7}.Resolved()
	if custom.MaxPlaces != 7 || custom.MaxTransitions != def.MaxTransitions {
		t.Fatalf("partial = %+v", custom)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// synthesis_handles.go: passThroughLint.Err (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestPassThroughLint(t *testing.T) {
	var p passThroughLint
	if !p.Passed() {
		t.Fatal("Passed")
	}
	if p.Err() != nil {
		t.Fatal("Err")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// fire_synthesize.go: firstJSONObject (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestFirstJSONObject(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"no braces":            "",
		`prefix {"a":1} suf`:   `{"a":1}`,
		`{"nested":{"k":"v"}}`: `{"nested":{"k":"v"}}`,
		`"quoted{not a brace"`: "",
		`{"s":"has\"brace{"}`:  `{"s":"has\"brace{"}`,
		`{"esc":"\\"}`:         `{"esc":"\\"}`,
	}
	for in, want := range cases {
		if got := firstJSONObject(in); got != want {
			t.Errorf("firstJSONObject(%q) = %q, want %q", in, got, want)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────
// a2ui_model_admin.go: NewSurfaceIDForTest (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestNewSurfaceIDForTest(t *testing.T) {
	id := NewSurfaceIDForTest("models")
	if id == "" {
		t.Fatal("empty")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// host.go: HostErrorCode, Is edge cases
// ──────────────────────────────────────────────────────────────────────────

func TestHostErrorCode(t *testing.T) {
	if HostErrorCode(nil) != "" {
		t.Fatal("nil")
	}
	if HostErrorCode(errors.New("plain")) != "" {
		t.Fatal("plain err")
	}
	if HostErrorCode(NewHostError("x", "m", nil)) != "x" {
		t.Fatal("host err")
	}
}

func TestHostError_Is_NilReceiver(t *testing.T) {
	var e *HostError
	if e.Is(NewHostError("x", "", nil)) {
		t.Fatal("nil receiver must return false")
	}
	ok := NewHostError("x", "m", nil)
	if ok.Is(errors.New("random")) {
		t.Fatal("non-host target must return false")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// token.go: Snapshot, formatPayloadPreview branches
// ──────────────────────────────────────────────────────────────────────────

func TestToken_Snapshot_Nil(t *testing.T) {
	var tok *Token
	snap := tok.Snapshot()
	if snap.Color != "" {
		t.Fatalf("nil snap = %+v", snap)
	}
}

func TestFormatPayloadPreview(t *testing.T) {
	if got := formatPayloadPreview(nil); got != "" {
		t.Fatalf("nil preview = %q", got)
	}
	if got := formatPayloadPreview("hello"); got != "hello" {
		t.Fatalf("string = %q", got)
	}
	if got := formatPayloadPreview([]byte("bytes")); got != "bytes" {
		t.Fatalf("bytes = %q", got)
	}
	// struct → JSON
	if got := formatPayloadPreview(map[string]int{"a": 1}); got == "" {
		t.Fatal("map preview empty")
	}
	// Truncation
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'x'
	}
	got := formatPayloadPreview(string(long))
	if len(got) != maxPayloadPreviewLen+3 {
		t.Fatalf("truncate len=%d", len(got))
	}
}

// ──────────────────────────────────────────────────────────────────────────
// memory.go: CompressSubNetSummary + findPrinciple
// ──────────────────────────────────────────────────────────────────────────

func TestCompressSubNetSummary_EmptyTokens(t *testing.T) {
	msg, err := CompressSubNetSummary("child-1", "writer", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != RoleObserver {
		t.Fatalf("role=%s", msg.Role)
	}
	if msg.Content == "" {
		t.Fatal("empty content")
	}
}

func TestCompressSubNetSummary_WithTokens(t *testing.T) {
	toks := []Token{
		{Color: ColorString, Payload: "hello"},
		{Color: ColorString, Payload: "world"},
	}
	msg, err := CompressSubNetSummary("c", "r", 1, toks)
	if err != nil {
		t.Fatal(err)
	}
	if msg.CPNID != "c" {
		t.Fatal("cpn id")
	}
}

func TestPersonality_FindPrinciple_Miss(t *testing.T) {
	p := &Personality{}
	if got := p.findPrinciple(PrincipleEtica); got != nil {
		t.Fatal("missing principle must return nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// cpn.go: HasApprovedFlow + MarkFlowApproved nil safety
// ──────────────────────────────────────────────────────────────────────────

func TestFlowApproval_NilAndEmpty(t *testing.T) {
	var c *CPN
	if c.HasApprovedFlow("x") {
		t.Fatal("nil cpn")
	}
	c.MarkFlowApproved("x")

	c = NewCPN("c", "t", 0, ModeMAS, "s", map[string]*Place{}, map[string]*Transition{})
	c.MarkFlowApproved("") // no-op
	if c.HasApprovedFlow("") {
		t.Fatal("empty must not be approved")
	}
	c.MarkFlowApproved("flow-1")
	if !c.HasApprovedFlow("flow-1") {
		t.Fatal("flow-1 must be approved")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Base: emit saturated self-bus branch
// ──────────────────────────────────────────────────────────────────────────

func TestEmit_SelfBusFull_BumpsDropMetric(t *testing.T) {
	c := NewCPN("c", "test", 0, ModeMAS, "s1", map[string]*Place{}, map[string]*Transition{})
	m := &countingMetrics{}
	c.HostRuntime = &HostRuntime{Metrics: m}
	// Force lazy allocation and fully saturate.
	bus := c.selfEventBus()
	for i := 0; i < cap(bus); i++ {
		bus <- Event{Type: EventProcessStdout}
	}
	// Emitting a process event now drops on self-bus.
	c.emit(&Event{Type: EventProcessStdout, Payload: ProcessOutputPayload{SessionID: "s1"}})
	if m.drops == 0 {
		t.Fatal("expected self_bus_full drop to be counted")
	}
}
