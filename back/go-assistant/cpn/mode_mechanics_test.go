package cpn

import (
	"testing"
)

// ── Paper Section 4.1: MAS vs Centaurian Mode Mechanics ─────────────────────

// TestModeMechanics_FullModeSwitchCycle validates the complete MAS → Centaurian → MAS
// cycle by manipulating tokens in computation places and calling checkModeSwitch.
func TestModeMechanics_FullModeSwitchCycle(t *testing.T) {
	t.Parallel()

	ec := &threadSafeEventCollector{}
	pComp := NewPlace("P:COMP", ColorHuman, SpaceComputation)

	c := &CPN{
		Mode:        ModeMAS,
		Places:      map[string]*Place{"P:COMP": pComp},
		Transitions: map[string]*Transition{},
		EventSink:   ec.sink,
	}

	// Phase 1: MAS, no human tokens → should stay MAS.
	c.checkModeSwitch()
	if c.getMode() != ModeMAS {
		t.Fatalf("Phase 1: mode = %s, want MAS", c.getMode())
	}

	// Phase 2: Deposit human token in computation → should switch to Centaurian.
	_ = pComp.Deposit(humanToken())
	c.checkModeSwitch()
	if c.getMode() != ModeCentaurian {
		t.Fatalf("Phase 2: mode = %s, want Centaurian", c.getMode())
	}

	// Phase 3: Consume the human token → should switch back to MAS.
	_, _ = pComp.Consume()
	c.checkModeSwitch()
	if c.getMode() != ModeMAS {
		t.Fatalf("Phase 3: mode = %s, want MAS", c.getMode())
	}

	// Verify 2 mode switch events were emitted.
	events := ec.getEvents()
	modeEvents := collectModeEvents(events)
	if len(modeEvents) != 2 {
		t.Fatalf("expected 2 mode switch events, got %d", len(modeEvents))
	}
	if modeEvents[0]["from"] != ModeMAS || modeEvents[0]["to"] != ModeCentaurian {
		t.Fatalf("event 0: %v, want MAS→Centaurian", modeEvents[0])
	}
	if modeEvents[1]["from"] != ModeCentaurian || modeEvents[1]["to"] != ModeMAS {
		t.Fatalf("event 1: %v, want Centaurian→MAS", modeEvents[1])
	}
}

// TestModeMechanics_CentaurianGuardEdgeCases tests edge cases for
// IsHumanOrigin and centaurianGuard with non-obvious token combinations.
func TestModeMechanics_CentaurianGuardEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tokens []*Token
		want   bool
	}{
		{
			name: "ColorHuman_with_OriginKindTool_counts_as_human",
			tokens: []*Token{
				{Color: ColorHuman, OriginKind: NodeKindTool, Space: SpaceComputation},
				aiToken(),
			},
			want: true, // ColorHuman → IsHumanOrigin() = true
		},
		{
			name: "ColorArtifact_with_OriginKindHITL_counts_as_human",
			tokens: []*Token{
				{Color: ColorArtifact, OriginKind: NodeKindHITL, Space: SpaceComputation},
				aiToken(),
			},
			want: true, // OriginKind == NodeKindHITL → IsHumanOrigin() = true
		},
		{
			name: "single_token_both_human_traits_no_AI",
			tokens: []*Token{
				{Color: ColorHuman, OriginKind: NodeKindHITL, Space: SpaceComputation},
			},
			want: false, // Only human, no AI partner
		},
		{
			name: "single_AI_token",
			tokens: []*Token{
				aiToken(),
			},
			want: false, // Only AI, no human partner
		},
		{
			name:   "nil_token_in_slice",
			tokens: []*Token{nil, aiToken()},
			want:   false, // nil.IsHumanOrigin() = false, only AI
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := centaurianGuard(tt.tokens)
			if got != tt.want {
				t.Fatalf("centaurianGuard() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestModeMechanics_EffectiveGuardAlwaysFalseWithCentaurian verifies that
// when a custom guard returns false, the composed Centaurian guard also rejects
// even when both human and AI tokens are present (AND composition).
func TestModeMechanics_EffectiveGuardAlwaysFalseWithCentaurian(t *testing.T) {
	t.Parallel()
	places := map[string]*Place{
		"P:OUT": NewPlace("P:OUT", ColorArtifact, SpaceComputation),
	}
	tr := NewTransition("T1", NodeKindTool, nil, []string{"P:OUT"})
	tr.Guard = func(_ []*Token) bool { return false }

	c := &CPN{Mode: ModeCentaurian, Places: places}

	guard := c.effectiveGuard(tr)
	if guard == nil {
		t.Fatal("expected non-nil composed guard")
	}

	// Both human and AI tokens present, but custom guard rejects.
	tokens := []*Token{humanToken(), aiToken()}
	if guard(tokens) {
		t.Fatal("composed guard should reject when custom guard returns false")
	}
}

// TestModeMechanics_MultipleComputationPlaces verifies mode switch considers
// ALL computation places, not just the first one.
func TestModeMechanics_MultipleComputationPlaces(t *testing.T) {
	t.Parallel()

	pComp1 := NewPlace("P1", ColorArtifact, SpaceComputation)
	_ = pComp1.Deposit(aiToken())

	pComp2 := NewPlace("P2", ColorHuman, SpaceComputation)
	// P2 is empty at first.

	pSurf := NewPlace("P3", ColorString, SpaceSurface)
	_ = pSurf.Deposit(&Token{Color: ColorString, Space: SpaceSurface})

	c := &CPN{
		Mode: ModeMAS,
		Places: map[string]*Place{
			"P1": pComp1,
			"P2": pComp2,
			"P3": pSurf,
		},
		Transitions: map[string]*Transition{},
	}

	// No human tokens in any computation place → stay MAS.
	c.checkModeSwitch()
	if c.getMode() != ModeMAS {
		t.Fatal("should stay MAS with no human tokens in computation")
	}

	// Add human token to P2 → should switch to Centaurian.
	_ = pComp2.Deposit(humanToken())
	c.checkModeSwitch()
	if c.getMode() != ModeCentaurian {
		t.Fatal("should switch to Centaurian when human token in any computation place")
	}
}
