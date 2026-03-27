package cpn

import (
	"sync"
	"testing"
)

// testPlaceWithTokens creates a Place and deposits n tokens into it.
func testPlaceWithTokens(id string, color ColorSet, space SpaceKind, n int) *Place {
	p := NewPlace(id, color, space)
	for i := range n {
		_ = p.Deposit(&Token{Color: color, Space: space, Payload: i})
	}
	return p
}

func TestNewTransition(t *testing.T) {
	tr := NewTransition("T1", NodeKindTool, []string{"P1", "P2"}, []string{"P3"})

	if tr.ID != "T1" {
		t.Fatalf("ID = %q, want %q", tr.ID, "T1")
	}
	if tr.Kind != NodeKindTool {
		t.Fatalf("Kind = %q, want %q", tr.Kind, NodeKindTool)
	}
	if len(tr.InputPlaces) != 2 || tr.InputPlaces[0] != "P1" || tr.InputPlaces[1] != "P2" {
		t.Fatalf("InputPlaces = %v, want [P1 P2]", tr.InputPlaces)
	}
	if len(tr.OutputPlaces) != 1 || tr.OutputPlaces[0] != "P3" {
		t.Fatalf("OutputPlaces = %v, want [P3]", tr.OutputPlaces)
	}
	if tr.Guard != nil {
		t.Fatal("Guard should be nil on new transition")
	}
	if tr.ErrorPlace != "" {
		t.Fatalf("ErrorPlace = %q, want empty", tr.ErrorPlace)
	}
}

func TestTransition_CanFire(t *testing.T) {
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
			name:  "input_place_not_in_map",
			trans: NewTransition("T1", NodeKindTool, []string{"P_MISSING"}, []string{"P2"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
			},
			want: false,
		},
		{
			name:  "two_inputs_both_have_tokens",
			trans: NewTransition("T1", NodeKindTool, []string{"P1", "P2"}, []string{"P3"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
				"P2": testPlaceWithTokens("P2", ColorJSON, SpaceObservation, 1),
			},
			want: true,
		},
		{
			name:  "two_inputs_second_empty",
			trans: NewTransition("T1", NodeKindTool, []string{"P1", "P2"}, []string{"P3"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
				"P2": NewPlace("P2", ColorJSON, SpaceObservation),
			},
			want: false,
		},
		{
			name:  "three_inputs_last_empty",
			trans: NewTransition("T1", NodeKindTool, []string{"P1", "P2", "P3"}, []string{"P4"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
				"P2": testPlaceWithTokens("P2", ColorJSON, SpaceObservation, 1),
				"P3": NewPlace("P3", ColorArtifact, SpaceComputation),
			},
			want: false,
		},
		{
			name:   "empty_input_places_nil",
			trans:  NewTransition("T1", NodeKindTool, nil, []string{"P2"}),
			places: map[string]*Place{},
			want:   false,
		},
		{
			name:   "empty_input_places_zero_length",
			trans:  NewTransition("T1", NodeKindTool, []string{}, []string{"P2"}),
			places: map[string]*Place{},
			want:   false,
		},
		{
			name:   "nil_places_map",
			trans:  NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"}),
			places: nil,
			want:   false,
		},
		{
			name:  "observer_kind_empty_inputs",
			trans: NewTransition("T1", NodeKindObserver, nil, []string{"P2"}),
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorEvent, SpaceObservation, 1),
			},
			want: false,
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

func TestTransition_CanFire_Guard(t *testing.T) {
	tests := []struct {
		name   string
		guard  func(tokens []*Token) bool
		places map[string]*Place
		want   bool
	}{
		{
			name:  "guard_returns_true",
			guard: func(_ []*Token) bool { return true },
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
			},
			want: true,
		},
		{
			name:  "guard_returns_false",
			guard: func(_ []*Token) bool { return false },
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
			},
			want: false,
		},
		{
			name:  "nil_guard_tokens_present",
			guard: nil,
			places: map[string]*Place{
				"P1": testPlaceWithTokens("P1", ColorString, SpaceSurface, 1),
			},
			want: true,
		},
		{
			name: "guard_inspects_payload",
			guard: func(tokens []*Token) bool {
				for _, tok := range tokens {
					if tok.Payload == "accept" {
						return true
					}
				}
				return false
			},
			places: map[string]*Place{
				"P1": func() *Place {
					p := NewPlace("P1", ColorString, SpaceSurface)
					_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "accept"})
					return p
				}(),
			},
			want: true,
		},
		{
			name: "guard_rejects_payload",
			guard: func(tokens []*Token) bool {
				for _, tok := range tokens {
					if tok.Payload == "accept" {
						return true
					}
				}
				return false
			},
			places: map[string]*Place{
				"P1": func() *Place {
					p := NewPlace("P1", ColorString, SpaceSurface)
					_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "reject"})
					return p
				}(),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"})
			tr.Guard = tt.guard
			got := tr.CanFire(tt.places)
			if got != tt.want {
				t.Errorf("CanFire() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransition_CanFire_GuardSeesAggregatedTokens(t *testing.T) {
	p1 := NewPlace("P1", ColorString, SpaceSurface)
	_ = p1.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "a"})
	_ = p1.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "b"})

	p2 := NewPlace("P2", ColorJSON, SpaceObservation)
	_ = p2.Deposit(&Token{Color: ColorJSON, Space: SpaceObservation, Payload: "c"})
	_ = p2.Deposit(&Token{Color: ColorJSON, Space: SpaceObservation, Payload: "d"})
	_ = p2.Deposit(&Token{Color: ColorJSON, Space: SpaceObservation, Payload: "e"})

	places := map[string]*Place{"P1": p1, "P2": p2}

	var captured []*Token
	tr := NewTransition("T1", NodeKindTool, []string{"P1", "P2"}, []string{"P3"})
	tr.Guard = func(tokens []*Token) bool {
		captured = tokens
		return true
	}

	got := tr.CanFire(places)
	if !got {
		t.Fatal("CanFire() = false, want true")
	}

	// 2 tokens from P1 + 3 tokens from P2 = 5 total
	if len(captured) != 5 {
		t.Fatalf("guard received %d tokens, want 5", len(captured))
	}

	// Verify order: P1 tokens first, then P2 tokens
	expectedPayloads := []any{"a", "b", "c", "d", "e"}
	for i, want := range expectedPayloads {
		if captured[i].Payload != want {
			t.Errorf("token[%d].Payload = %v, want %v", i, captured[i].Payload, want)
		}
	}
}

func TestTransition_CanFire_ConcurrentWithDeposit(t *testing.T) {
	const n = 1000
	p := NewPlace("P1", ColorString, SpaceSurface)
	// Seed with one token so CanFire can return true
	_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "seed"})

	tr := NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"})
	places := map[string]*Place{"P1": p}

	var wg sync.WaitGroup
	wg.Add(2 * n)

	// Half goroutines call CanFire, half deposit tokens
	for i := range n {
		go func() {
			defer wg.Done()
			tr.CanFire(places)
		}()
		go func() {
			defer wg.Done()
			_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: i})
		}()
	}
	wg.Wait()

	// After all deposits, place should have 1 (seed) + n tokens
	if p.Len() != n+1 {
		t.Fatalf("Len = %d, want %d", p.Len(), n+1)
	}
}

func TestTransition_CanFire_DuplicateInputPlace(t *testing.T) {
	p := NewPlace("P1", ColorString, SpaceSurface)
	_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	// Same place referenced twice — tokens aggregated twice
	tr := NewTransition("T1", NodeKindTool, []string{"P1", "P1"}, []string{"P2"})

	var captured []*Token
	tr.Guard = func(tokens []*Token) bool {
		captured = tokens
		return true
	}

	places := map[string]*Place{"P1": p}
	got := tr.CanFire(places)
	if !got {
		t.Fatal("CanFire() = false, want true")
	}
	// Token appears twice (once per input place reference)
	if len(captured) != 2 {
		t.Fatalf("guard received %d tokens, want 2", len(captured))
	}
}
