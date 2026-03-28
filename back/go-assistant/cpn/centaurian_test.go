package cpn

import (
	"context"
	"sync"
	"testing"
	"time"
)

// --- Test helpers ---

// humanToken creates a Token with ColorHuman origin.
func humanToken() *Token {
	return &Token{Color: ColorHuman, OriginKind: NodeKindHITL, Space: SpaceComputation}
}

// aiToken creates a Token with AI origin (not human).
func aiToken() *Token {
	return &Token{Color: ColorArtifact, OriginKind: NodeKindTool, Space: SpaceComputation}
}

// --- centaurianGuard tests ---

func TestCentaurianGuard_BothHumanAndAI(t *testing.T) {
	tokens := []*Token{humanToken(), aiToken()}
	if !centaurianGuard(tokens) {
		t.Fatal("centaurianGuard should return true when both human and AI tokens present")
	}
}

func TestCentaurianGuard_OnlyHuman(t *testing.T) {
	tokens := []*Token{humanToken(), humanToken()}
	if centaurianGuard(tokens) {
		t.Fatal("centaurianGuard should return false with only human tokens")
	}
}

func TestCentaurianGuard_OnlyAI(t *testing.T) {
	tokens := []*Token{aiToken(), aiToken()}
	if centaurianGuard(tokens) {
		t.Fatal("centaurianGuard should return false with only AI tokens")
	}
}

func TestCentaurianGuard_Empty(t *testing.T) {
	if centaurianGuard(nil) {
		t.Fatal("centaurianGuard should return false with nil tokens")
	}
	if centaurianGuard([]*Token{}) {
		t.Fatal("centaurianGuard should return false with empty tokens")
	}
}

func TestCentaurianGuard_MixedOrigins(t *testing.T) {
	tokens := []*Token{
		aiToken(),
		aiToken(),
		humanToken(),
		aiToken(),
	}
	if !centaurianGuard(tokens) {
		t.Fatal("centaurianGuard should return true with at least one of each")
	}
}

func TestCentaurianGuard_HumanByOriginKindOnly(t *testing.T) {
	// Token with OriginKind=NodeKindHITL but Color != ColorHuman.
	tok := &Token{Color: ColorArtifact, OriginKind: NodeKindHITL, Space: SpaceComputation}
	tokens := []*Token{tok, aiToken()}
	// tok.IsHumanOrigin() == true because OriginKind == NodeKindHITL
	if !centaurianGuard(tokens) {
		t.Fatal("centaurianGuard should recognize human origin by OriginKind")
	}
}

// --- isComputationTransition tests ---

func TestIsComputationTransition_HasCompOutput(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	cpn := &CPN{Places: places}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})

	if !cpn.isComputationTransition(tr) {
		t.Fatal("should return true when output place is SpaceComputation")
	}
}

func TestIsComputationTransition_NoCompOutput(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorString, SpaceSurface),
		"P2": NewPlace("P2", ColorEvent, SpaceObservation),
	}
	cpn := &CPN{Places: places}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1", "P2"})

	if cpn.isComputationTransition(tr) {
		t.Fatal("should return false when no output place is SpaceComputation")
	}
}

func TestIsComputationTransition_Mixed(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorString, SpaceSurface),
		"P2": NewPlace("P2", ColorArtifact, SpaceComputation),
	}
	cpn := &CPN{Places: places}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1", "P2"})

	if !cpn.isComputationTransition(tr) {
		t.Fatal("should return true when at least one output place is SpaceComputation")
	}
}

func TestIsComputationTransition_MissingPlace(t *testing.T) {
	places := map[string]*Place{}
	cpn := &CPN{Places: places}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P_MISSING"})

	if cpn.isComputationTransition(tr) {
		t.Fatal("should return false when output place not found")
	}
}

// --- effectiveGuard tests ---

func TestEffectiveGuard_ModeMAS(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	customGuard := func(_ []*Token) bool { return true }
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	tr.Guard = customGuard

	c := &CPN{Mode: ModeMAS, Places: places}

	// In ModeMAS, effectiveGuard should return t.Guard unchanged.
	got := c.effectiveGuard(tr)
	if got == nil {
		t.Fatal("effectiveGuard should return custom guard in ModeMAS")
	}
}

func TestEffectiveGuard_ModeMAS_NilGuard(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})

	c := &CPN{Mode: ModeMAS, Places: places}

	got := c.effectiveGuard(tr)
	if got != nil {
		t.Fatal("effectiveGuard should return nil guard in ModeMAS when t.Guard is nil")
	}
}

func TestEffectiveGuard_Centaurian_CompTransition_NoCustom(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	// Guard is nil.

	c := &CPN{Mode: ModeCentaurian, Places: places}

	got := c.effectiveGuard(tr)
	if got == nil {
		t.Fatal("effectiveGuard should return centaurianGuard for computation transition in Centaurian mode")
	}

	// Verify it behaves like centaurianGuard.
	if got([]*Token{humanToken(), aiToken()}) != true {
		t.Fatal("injected guard should accept human+AI tokens")
	}
	if got([]*Token{aiToken()}) != false {
		t.Fatal("injected guard should reject AI-only tokens")
	}
}

func TestEffectiveGuard_Centaurian_CompTransition_WithCustom(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	customCalled := false
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	tr.Guard = func(tokens []*Token) bool {
		customCalled = true
		return true
	}

	c := &CPN{Mode: ModeCentaurian, Places: places}

	got := c.effectiveGuard(tr)
	if got == nil {
		t.Fatal("effectiveGuard should return composed guard")
	}

	// Both human+AI → centaurian passes, custom passes → true.
	if !got([]*Token{humanToken(), aiToken()}) {
		t.Fatal("composed guard should accept when both guards pass")
	}
	if !customCalled {
		t.Fatal("custom guard should be called in composition")
	}

	// Only AI → centaurian fails → false (custom not even needed).
	customCalled = false
	if got([]*Token{aiToken()}) {
		t.Fatal("composed guard should reject when centaurian guard fails")
	}
}

func TestEffectiveGuard_Centaurian_CompTransition_CustomRejects(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	tr.Guard = func(_ []*Token) bool { return false }

	c := &CPN{Mode: ModeCentaurian, Places: places}

	got := c.effectiveGuard(tr)
	// Both tokens present but custom guard rejects.
	if got([]*Token{humanToken(), aiToken()}) {
		t.Fatal("composed guard should reject when custom guard fails")
	}
}

func TestEffectiveGuard_Centaurian_NonCompTransition(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorString, SpaceSurface),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	tr.Guard = nil

	c := &CPN{Mode: ModeCentaurian, Places: places}

	got := c.effectiveGuard(tr)
	if got != nil {
		t.Fatal("effectiveGuard should return nil for non-computation transition in Centaurian mode")
	}
}

// --- effectiveCanFire tests ---

func TestEffectiveCanFire_ModeMAS_BackwardCompat(t *testing.T) {
	pIn := NewPlace("P1", ColorArtifact, SpaceComputation)
	_ = pIn.Deposit(&Token{Color: ColorArtifact, Space: SpaceComputation})

	places := map[string]*Place{
		"P1": pIn,
		"P2": NewPlace("P2", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"})

	c := &CPN{Mode: ModeMAS, Places: places}

	// In ModeMAS, effectiveCanFire should behave like CanFire.
	got := c.effectiveCanFire(tr)
	want := tr.CanFire(places)
	if got != want {
		t.Fatalf("effectiveCanFire = %v, want %v (same as CanFire)", got, want)
	}
}

func TestEffectiveCanFire_Centaurian_BlocksWithoutHuman(t *testing.T) {
	pIn := NewPlace("P_IN", ColorArtifact, SpaceComputation)
	_ = pIn.Deposit(aiToken())

	places := map[string]*Place{
		"P_IN":  pIn,
		"P_OUT": NewPlace("P_OUT", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, []string{"P_IN"}, []string{"P_OUT"})

	c := &CPN{Mode: ModeCentaurian, Places: places}

	if c.effectiveCanFire(tr) {
		t.Fatal("effectiveCanFire should block computation transition without human token in Centaurian mode")
	}
}

func TestEffectiveCanFire_Centaurian_AllowsWithBoth(t *testing.T) {
	pPlan := NewPlace("P_PLAN", ColorArtifact, SpaceComputation)
	_ = pPlan.Deposit(aiToken())

	pApproved := NewPlace("P_APPROVED", ColorHuman, SpaceComputation)
	_ = pApproved.Deposit(humanToken())

	places := map[string]*Place{
		"P_PLAN":     pPlan,
		"P_APPROVED": pApproved,
		"P_OUT":      NewPlace("P_OUT", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, []string{"P_PLAN", "P_APPROVED"}, []string{"P_OUT"})

	c := &CPN{Mode: ModeCentaurian, Places: places}

	if !c.effectiveCanFire(tr) {
		t.Fatal("effectiveCanFire should allow computation transition with both human and AI tokens")
	}
}

func TestEffectiveCanFire_Centaurian_NonCompAlwaysFires(t *testing.T) {
	pIn := NewPlace("P_IN", ColorArtifact, SpaceSurface)
	_ = pIn.Deposit(&Token{Color: ColorArtifact, Space: SpaceSurface, OriginKind: NodeKindTool})

	places := map[string]*Place{
		"P_IN":  pIn,
		"P_OUT": NewPlace("P_OUT", ColorArtifact, SpaceSurface),
	}
	tr := NewTransition("T1", NodeKindTool, []string{"P_IN"}, []string{"P_OUT"})

	c := &CPN{Mode: ModeCentaurian, Places: places}

	if !c.effectiveCanFire(tr) {
		t.Fatal("effectiveCanFire should allow non-computation transition in Centaurian mode without human token")
	}
}

// --- CanFire backward compatibility ---

func TestCanFire_BackwardCompatibility(t *testing.T) {
	// All existing CanFire tests from transition_test.go should still pass.
	// This test verifies the refactored CanFire delegates correctly.
	tests := []struct {
		name   string
		trans  *Transition
		places map[string]*Place
		want   bool
	}{
		{
			name:  "single_input_with_token",
			trans: NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
			},
			want: true,
		},
		{
			name:  "single_input_empty",
			trans: NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"}),
			places: map[string]*Place{
				"P1": NewPlace("P1", ColorString, SpaceSurface),
			},
			want: false,
		},
		{
			name:   "empty_input_places",
			trans:  NewTransition("T1", NodeKindTool, nil, []string{"P2"}),
			places: map[string]*Place{},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.trans.CanFire(tt.places)
			if got != tt.want {
				t.Errorf("CanFire() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Mode switch tests ---

func TestCheckModeSwitch_MAStoCentaurian(t *testing.T) {
	pComp := NewPlace("P1", ColorHuman, SpaceComputation)
	_ = pComp.Deposit(humanToken())

	c := &CPN{
		Mode:        ModeMAS,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeCentaurian {
		t.Fatalf("mode = %s, want %s after human token in computation", c.getMode(), ModeCentaurian)
	}
}

func TestCheckModeSwitch_CentaurianToMAS(t *testing.T) {
	// Computation place with only AI tokens — no human.
	pComp := NewPlace("P1", ColorArtifact, SpaceComputation)
	_ = pComp.Deposit(aiToken())

	c := &CPN{
		Mode:        ModeCentaurian,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeMAS {
		t.Fatalf("mode = %s, want %s when no human tokens in computation", c.getMode(), ModeMAS)
	}
}

func TestCheckModeSwitch_NoSwitchWhenHITLPending(t *testing.T) {
	// No human tokens in computation, but a HITL transition is firable.
	pIn := NewPlace("P_IN", ColorString, SpaceSurface)
	_ = pIn.Deposit(&Token{Color: ColorString, Space: SpaceSurface})

	hitlTr := NewTransition("T_HITL", NodeKindHITL, []string{"P_IN"}, []string{"P_OUT"})

	c := &CPN{
		Mode:   ModeCentaurian,
		Places: map[string]*Place{"P_IN": pIn},
		Transitions: map[string]*Transition{
			"T_HITL": hitlTr,
		},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeCentaurian {
		t.Fatalf("mode = %s, want %s when HITL is pending", c.getMode(), ModeCentaurian)
	}
}

func TestCheckModeSwitch_SwitchWhenHITLNotFirable(t *testing.T) {
	// No human tokens, HITL exists but not firable (empty input place).
	pIn := NewPlace("P_IN", ColorString, SpaceSurface)
	// pIn is empty — HITL cannot fire.

	hitlTr := NewTransition("T_HITL", NodeKindHITL, []string{"P_IN"}, []string{"P_OUT"})

	c := &CPN{
		Mode:   ModeCentaurian,
		Places: map[string]*Place{"P_IN": pIn},
		Transitions: map[string]*Transition{
			"T_HITL": hitlTr,
		},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeMAS {
		t.Fatalf("mode = %s, want %s when no human tokens and HITL not firable", c.getMode(), ModeMAS)
	}
}

func TestCheckModeSwitch_NoopWhenAlreadyCorrect(t *testing.T) {
	// ModeMAS with no human tokens in computation — should stay MAS.
	pComp := NewPlace("P1", ColorArtifact, SpaceComputation)
	_ = pComp.Deposit(aiToken())

	var events []*Event
	c := &CPN{
		Mode:        ModeMAS,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
		EventSink:   func(e *Event) { events = append(events, e) },
	}

	c.checkModeSwitch()

	if c.getMode() != ModeMAS {
		t.Fatalf("mode should remain MAS")
	}
	if len(events) != 0 {
		t.Fatal("no EventModeSwitch should be emitted when mode doesn't change")
	}
}

// --- SetMode tests ---

func TestSetMode_ExplicitOverride(t *testing.T) {
	c := &CPN{Mode: ModeMAS}

	c.SetMode(ModeCentaurian)
	if c.getMode() != ModeCentaurian {
		t.Fatal("SetMode should force mode to Centaurian")
	}

	c.SetMode(ModeMAS)
	if c.getMode() != ModeMAS {
		t.Fatal("SetMode should force mode to MAS")
	}
}

func TestSetMode_Noop(t *testing.T) {
	var events []*Event
	c := &CPN{
		Mode:      ModeMAS,
		EventSink: func(e *Event) { events = append(events, e) },
	}

	c.SetMode(ModeMAS) // same mode
	if len(events) != 0 {
		t.Fatal("SetMode to same mode should not emit event")
	}
}

// --- EventModeSwitch emission ---

func TestModeSwitch_EmitsEvent(t *testing.T) {
	var events []*Event
	pComp := NewPlace("P1", ColorHuman, SpaceComputation)
	_ = pComp.Deposit(humanToken())

	c := &CPN{
		Mode:        ModeMAS,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
		EventSink:   func(e *Event) { events = append(events, e) },
	}

	c.checkModeSwitch()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != EventModeSwitch {
		t.Fatalf("event type = %s, want %s", e.Type, EventModeSwitch)
	}
	payload, ok := e.Payload.(map[string]Mode)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]Mode", e.Payload)
	}
	if payload["from"] != ModeMAS || payload["to"] != ModeCentaurian {
		t.Fatalf("payload = %v, want from=mas to=centaurian", payload)
	}
}

func TestModeSwitch_EmitsEventOnCentaurianToMAS(t *testing.T) {
	var events []*Event
	pComp := NewPlace("P1", ColorArtifact, SpaceComputation)
	_ = pComp.Deposit(aiToken())

	c := &CPN{
		Mode:        ModeCentaurian,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
		EventSink:   func(e *Event) { events = append(events, e) },
	}

	c.checkModeSwitch()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	payload, ok := events[0].Payload.(map[string]Mode)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]Mode", events[0].Payload)
	}
	if payload["from"] != ModeCentaurian || payload["to"] != ModeMAS {
		t.Fatalf("payload = %v, want from=centaurian to=mas", payload)
	}
}

// --- Reference Net 16.3: Approval Pattern (end-to-end) ---

func TestReferenceNet_16_3_ApprovalPattern(t *testing.T) {
	// Topology:
	// P:PLAN (ARTIFACT, Computation) ──┐
	//                                  ├──> [tool:execute-change] --> P:RESULT
	// P:APPROVED (HUMAN, Computation) ─┘
	//
	// In ModeCentaurian, execute-change requires BOTH P:PLAN (AI) AND P:APPROVED (human).
	// Without P:APPROVED, centaurianGuard blocks the transition.

	pPlan := NewPlace("P:PLAN", ColorArtifact, SpaceComputation)
	pApproved := NewPlace("P:APPROVED", ColorHuman, SpaceComputation)
	pResult := NewPlace("P:RESULT", ColorArtifact, SpaceComputation)

	execute := NewTransition("tool:execute-change", NodeKindTool,
		[]string{"P:PLAN", "P:APPROVED"},
		[]string{"P:RESULT"})
	execute.Executor = func(_ context.Context, in Token) (Token, error) {
		return Token{
			Color:   ColorArtifact,
			Space:   SpaceComputation,
			Payload: "executed:" + in.Payload.(string),
		}, nil
	}

	places := map[string]*Place{
		"P:PLAN":     pPlan,
		"P:APPROVED": pApproved,
		"P:RESULT":   pResult,
	}
	transitions := map[string]*Transition{
		"tool:execute-change": execute,
	}

	c := NewCPN("test-16.3", "test", 0, ModeCentaurian, "sess-1", places, transitions)

	// --- Phase 1: Only AI token in P:PLAN. Should NOT fire. ---
	planToken := &Token{
		Color:      ColorArtifact,
		Space:      SpaceComputation,
		OriginKind: NodeKindLLM,
		Payload:    "plan-data",
	}
	_ = pPlan.Deposit(planToken)

	if c.effectiveCanFire(execute) {
		t.Fatal("Phase 1: execute-change should NOT fire with only AI token")
	}

	// --- Phase 2: Add human approval token. Should fire. ---
	approvalToken := &Token{
		Color:      ColorHuman,
		Space:      SpaceComputation,
		OriginKind: NodeKindHITL,
		Payload:    "approved",
	}
	_ = pApproved.Deposit(approvalToken)

	if !c.effectiveCanFire(execute) {
		t.Fatal("Phase 2: execute-change should fire with both human and AI tokens")
	}

	// --- Phase 3: Run the CPN end-to-end. ---
	var events []*Event
	c.EventSink = func(e *Event) { events = append(events, e) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Phase 3: Run failed: %v", err)
	}

	// Verify result token was deposited.
	if pResult.Len() == 0 {
		t.Fatal("Phase 3: P:RESULT should have a token after execution")
	}

	tok, _ := pResult.Consume()
	payload, ok := tok.Payload.(string)
	if !ok || payload != "executed:plan-data" {
		t.Fatalf("Phase 3: result payload = %v, want 'executed:plan-data'", tok.Payload)
	}

	// Verify CPN completed.
	if c.getState() != StateCompleted {
		t.Fatalf("Phase 3: state = %s, want completed", c.getState())
	}
}

func TestReferenceNet_16_3_BlockedWithoutApproval(t *testing.T) {
	// Same topology but only AI token — should deadlock.
	pPlan := NewPlace("P:PLAN", ColorArtifact, SpaceComputation)
	pApproved := NewPlace("P:APPROVED", ColorHuman, SpaceComputation)
	pResult := NewPlace("P:RESULT", ColorArtifact, SpaceComputation)

	execute := NewTransition("tool:execute-change", NodeKindTool,
		[]string{"P:PLAN", "P:APPROVED"},
		[]string{"P:RESULT"})
	execute.Executor = func(_ context.Context, in Token) (Token, error) {
		return Token{Color: ColorArtifact, Space: SpaceComputation}, nil
	}

	places := map[string]*Place{
		"P:PLAN":     pPlan,
		"P:APPROVED": pApproved,
		"P:RESULT":   pResult,
	}
	transitions := map[string]*Transition{
		"tool:execute-change": execute,
	}

	c := NewCPN("test-blocked", "test", 0, ModeCentaurian, "sess-1", places, transitions)

	// Only deposit AI token — no human approval.
	_ = pPlan.Deposit(&Token{
		Color:      ColorArtifact,
		Space:      SpaceComputation,
		OriginKind: NodeKindLLM,
		Payload:    "plan",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := c.Run(ctx)

	// Should deadlock because the transition can't fire without human token.
	if err != ErrDeadlock {
		t.Fatalf("expected ErrDeadlock, got %v", err)
	}
	if pResult.Len() != 0 {
		t.Fatal("P:RESULT should be empty when blocked by centaurianGuard")
	}
}

// --- canFireWith tests ---

func TestCanFireWith_NilGuard(t *testing.T) {
	p := testPlaceWithTokens("P1", ColorString, SpaceSurface, 1)
	tr := NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"})
	tr.Guard = func(_ []*Token) bool { return false } // custom guard that rejects

	places := map[string]*Place{"P1": p}

	// canFireWith with nil guard should ignore t.Guard and fire.
	if !tr.canFireWith(places, nil) {
		t.Fatal("canFireWith(nil guard) should fire when tokens present")
	}

	// canFireWith with custom guard should respect it.
	if tr.canFireWith(places, func(_ []*Token) bool { return false }) {
		t.Fatal("canFireWith(rejecting guard) should not fire")
	}
}

func TestCanFireWith_ConcurrentSafety(t *testing.T) {
	// Verifies canFireWith doesn't mutate transition state (no race).
	p := testPlaceWithTokens("P1", ColorString, SpaceSurface, 1)
	tr := NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"})
	places := map[string]*Place{"P1": p}

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tr.canFireWith(places, centaurianGuard)
		}()
		go func() {
			defer wg.Done()
			tr.CanFire(places)
		}()
	}
	wg.Wait()
}

// --- Guard injection matrix verification ---

func TestGuardInjectionMatrix(t *testing.T) {
	compPlace := NewPlace("P_COMP", ColorArtifact, SpaceComputation)
	surfPlace := NewPlace("P_SURF", ColorString, SpaceSurface)

	alwaysTrue := func(_ []*Token) bool { return true }

	tests := []struct {
		name        string
		mode        Mode
		outputPlace string
		guard       func([]*Token) bool
		wantNil     bool // if true, effective guard should be nil
		wantCent    bool // if true, effective guard should be centaurianGuard (or composed)
	}{
		{"MAS_any_nil", ModeMAS, "P_COMP", nil, true, false},
		{"MAS_any_custom", ModeMAS, "P_COMP", alwaysTrue, false, false},
		{"Centaurian_noComp_nil", ModeCentaurian, "P_SURF", nil, true, false},
		{"Centaurian_noComp_custom", ModeCentaurian, "P_SURF", alwaysTrue, false, false},
		{"Centaurian_comp_nil", ModeCentaurian, "P_COMP", nil, false, true},
		{"Centaurian_comp_custom", ModeCentaurian, "P_COMP", alwaysTrue, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			places := map[string]*Place{
				"P_COMP": compPlace,
				"P_SURF": surfPlace,
			}
			tr := NewTransition("T1", NodeKindTool, nil, []string{tt.outputPlace})
			tr.Guard = tt.guard

			c := &CPN{Mode: tt.mode, Places: places}
			got := c.effectiveGuard(tr)

			if tt.wantNil && got != nil {
				t.Fatal("expected nil guard")
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected non-nil guard")
			}
			if tt.wantCent && got != nil {
				// Verify centaurian behavior.
				if got([]*Token{aiToken()}) {
					t.Fatal("guard should reject AI-only tokens in centaurian mode")
				}
				if !got([]*Token{humanToken(), aiToken()}) {
					t.Fatal("guard should accept human+AI tokens in centaurian mode")
				}
			}
		})
	}
}

// --- Edge case: Guard always-true + centaurian ---

func TestEffectiveGuard_AlwaysTrueCustomWithCentaurian(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P1"})
	tr.Guard = func(_ []*Token) bool { return true }

	c := &CPN{Mode: ModeCentaurian, Places: places}

	got := c.effectiveGuard(tr)
	// Even with always-true custom guard, centaurian should still enforce co-trigger.
	if got([]*Token{aiToken()}) {
		t.Fatal("centaurian guard should still enforce co-trigger even with always-true custom guard")
	}
	if !got([]*Token{humanToken(), aiToken()}) {
		t.Fatal("should pass when both present")
	}
}

// --- checkModeSwitch with empty places ---

func TestCheckModeSwitch_EmptyPlaces(t *testing.T) {
	c := &CPN{
		Mode:        ModeMAS,
		Places:      map[string]*Place{},
		Transitions: map[string]*Transition{},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeMAS {
		t.Fatal("should stay MAS with no places")
	}
}

func TestCheckModeSwitch_EmptyCompPlaces(t *testing.T) {
	pComp := NewPlace("P1", ColorArtifact, SpaceComputation)
	// Empty — no tokens.

	c := &CPN{
		Mode:        ModeCentaurian,
		Places:      map[string]*Place{"P1": pComp},
		Transitions: map[string]*Transition{},
	}

	c.checkModeSwitch()

	if c.getMode() != ModeMAS {
		t.Fatal("should switch to MAS when computation places are empty")
	}
}
