package cpn

import (
	"errors"
	"testing"
	"time"
)

// ── Paper Section 3: Division of Places ─────────────────────────────────────
// These tests validate the three communication spaces (Surface, Observation,
// Computation) and the axiom that HITL is the only sanctioned space bridge.

// TestSpaceInvariant_CrossSpaceDepositRejected validates that tokens cannot
// cross space boundaries via Place.Deposit.
func TestSpaceInvariant_CrossSpaceDepositRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		placeSpace SpaceKind
		tokenSpace SpaceKind
		wantErr    error
	}{
		// SpaceViolation: Surface token → Computation place (checked first in Deposit).
		{"surface_to_computation", SpaceComputation, SpaceSurface, ErrSpaceViolation},
		// SpaceMismatch: remaining cross-space combinations.
		{"computation_to_surface", SpaceSurface, SpaceComputation, ErrSpaceMismatch},
		{"observation_to_surface", SpaceSurface, SpaceObservation, ErrSpaceMismatch},
		{"surface_to_observation", SpaceObservation, SpaceSurface, ErrSpaceMismatch},
		{"computation_to_observation", SpaceObservation, SpaceComputation, ErrSpaceMismatch},
		{"observation_to_computation", SpaceComputation, SpaceObservation, ErrSpaceMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Use a matching color to isolate space validation only.
			p := NewPlace("P1", ColorString, tt.placeSpace)
			tok := &Token{Color: ColorString, Space: tt.tokenSpace, Payload: "data"}
			err := p.Deposit(tok)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Deposit() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestSpaceInvariant_ValidateBlocksNonHITLSpaceBridge verifies that Validate()
// rejects Surface→Computation wiring for every non-HITL transition kind.
func TestSpaceInvariant_ValidateBlocksNonHITLSpaceBridge(t *testing.T) {
	t.Parallel()
	nonHITLKinds := []NodeKind{
		NodeKindTool,
		NodeKindLLM,
		NodeKindValidate,
		NodeKindSubNet,
		NodeKindObserver,
	}
	for _, kind := range nonHITLKinds {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			places := newPlaces(
				placeSpec{"P:IN", ColorString, SpaceSurface},
				placeSpec{"P:OUT", ColorString, SpaceComputation},
			)
			transitions := newTransitions(transSpec{
				ID: "T:BAD", Kind: kind,
				Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
			})
			err := Validate(places, transitions)
			if !errors.Is(err, ErrSpaceViolation) {
				t.Fatalf("Validate() for kind=%s: error = %v, want ErrSpaceViolation", kind, err)
			}
		})
	}
}

// TestSpaceInvariant_HITLExemptFromSpaceViolation confirms HITL transitions
// pass Validate() when bridging Surface→Computation (Axiom A4).
func TestSpaceInvariant_HITLExemptFromSpaceViolation(t *testing.T) {
	t.Parallel()
	ch := make(chan Token, 1)
	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceSurface},
		placeSpec{"P:OUT", ColorHuman, SpaceComputation},
	)
	transitions := newTransitions(transSpec{
		ID: "T:HITL", Kind: NodeKindHITL,
		Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
		HITL: &HITLConfig{Channel: ch, Prompt: "approve?"},
	})

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("Validate() should pass for HITL bridge, got: %v", err)
	}
}

// TestSpaceInvariant_HITLSpaceBridgingAdjustsTokenSpace runs a CPN with HITL
// and verifies that fireHITL adjusts the token's Space to match each output place.
func TestSpaceInvariant_HITLSpaceBridgingAdjustsTokenSpace(t *testing.T) {
	t.Parallel()
	ch := make(chan Token, 1)
	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceSurface},
		placeSpec{"P:OUT", ColorHuman, SpaceComputation},
	)
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "input"})

	transitions := newTransitions(transSpec{
		ID: "T:HITL", Kind: NodeKindHITL,
		Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
		HITL: &HITLConfig{Channel: ch, Prompt: "approve?"},
	})

	c := NewCPN("test-bridge", "test", 0, ModeMAS, "sess-1", places, transitions)

	// Feed approval token before running (buffered channel).
	ch <- makeApproveToken()

	err := runWithTimeout(t, c, 5*time.Second)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	// Verify the output token's Space was adjusted to SpaceComputation.
	tok, tokErr := places["P:OUT"].Consume()
	if tokErr != nil {
		t.Fatalf("P:OUT should have a token, got: %v", tokErr)
	}
	if tok.Space != SpaceComputation {
		t.Fatalf("output token Space = %s, want %s (HITL should adjust)", tok.Space, SpaceComputation)
	}
}

// TestSpaceInvariant_ToolTransitionPreservesSpace verifies that non-HITL
// transitions do NOT adjust token space — space comes from the executor result.
func TestSpaceInvariant_ToolTransitionPreservesSpace(t *testing.T) {
	t.Parallel()
	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceComputation},
		placeSpec{"P:OUT", ColorString, SpaceComputation},
	)
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "data"})

	transitions := newTransitions(transSpec{
		ID: "T:TOOL", Kind: NodeKindTool,
		Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
		Executor: prefixTool("processed:", ColorString, SpaceComputation),
	})

	c := NewCPN("test-preserve", "test", 0, ModeMAS, "sess-1", places, transitions)
	err := runWithTimeout(t, c, 5*time.Second)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	tok, _ := places["P:OUT"].Consume()
	if tok.Space != SpaceComputation {
		t.Fatalf("output token Space = %s, want %s", tok.Space, SpaceComputation)
	}
}

// TestSpaceInvariant_ObservationTokenStaysInObservation verifies that
// observation tokens are confined to observation places.
func TestSpaceInvariant_ObservationTokenStaysInObservation(t *testing.T) {
	t.Parallel()
	pObs := NewPlace("P:OBS", ColorEvent, SpaceObservation)

	// Same space deposit should succeed.
	err := pObs.Deposit(&Token{Color: ColorEvent, Space: SpaceObservation, Payload: "event"})
	if err != nil {
		t.Fatalf("observation→observation deposit should succeed, got: %v", err)
	}

	// Surface token into observation place should fail.
	err = pObs.Deposit(&Token{Color: ColorEvent, Space: SpaceSurface, Payload: "event"})
	if !errors.Is(err, ErrSpaceMismatch) {
		t.Fatalf("surface→observation deposit error = %v, want ErrSpaceMismatch", err)
	}

	// Computation token into observation place should fail.
	err = pObs.Deposit(&Token{Color: ColorEvent, Space: SpaceComputation, Payload: "event"})
	if !errors.Is(err, ErrSpaceMismatch) {
		t.Fatalf("computation→observation deposit error = %v, want ErrSpaceMismatch", err)
	}
}

// ── Validate: HITL config enforcement ───────────────────────────────────────

// TestSpaceInvariant_ValidateHITLRequiresConfig verifies that HITL transitions
// with nil config or nil channel are rejected by Validate().
func TestSpaceInvariant_ValidateHITLRequiresConfig(t *testing.T) {
	t.Parallel()
	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceSurface},
		placeSpec{"P:OUT", ColorHuman, SpaceComputation},
	)

	t.Run("nil_config", func(t *testing.T) {
		transitions := map[string]*Transition{
			"T:HITL": {
				ID: "T:HITL", Kind: NodeKindHITL,
				InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
				// HITLConfig is nil.
			},
		}
		err := Validate(places, transitions)
		if !errors.Is(err, ErrHITLMisconfigured) {
			t.Fatalf("expected ErrHITLMisconfigured for nil config, got: %v", err)
		}
	})

	t.Run("nil_channel", func(t *testing.T) {
		transitions := map[string]*Transition{
			"T:HITL": {
				ID: "T:HITL", Kind: NodeKindHITL,
				InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
				HITLConfig: &HITLConfig{Channel: nil, Prompt: "?"},
			},
		}
		err := Validate(places, transitions)
		if !errors.Is(err, ErrHITLMisconfigured) {
			t.Fatalf("expected ErrHITLMisconfigured for nil channel, got: %v", err)
		}
	})
}

// ── Cross-space via Validate: inverse direction is allowed ──────────────────

// TestSpaceInvariant_ComputationToSurfaceWiringAllowed verifies that
// Validate() does NOT reject Computation input → Surface output (only
// Surface→Computation is prohibited for non-HITL transitions).
func TestSpaceInvariant_ComputationToSurfaceWiringAllowed(t *testing.T) {
	t.Parallel()
	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceComputation},
		placeSpec{"P:OUT", ColorString, SpaceSurface},
	)
	transitions := newTransitions(transSpec{
		ID: "T:TOOL", Kind: NodeKindTool,
		Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
	})

	// Validate should pass — this direction is allowed by the framework.
	if err := Validate(places, transitions); err != nil {
		t.Fatalf("Validate() should allow Computation→Surface wiring, got: %v", err)
	}
}
