package cpn

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestNewPlace(t *testing.T) {
	p := NewPlace("p1", ColorString, SpaceSurface)

	if p.ID != "p1" {
		t.Fatalf("ID = %q, want %q", p.ID, "p1")
	}
	if p.Color != ColorString {
		t.Fatalf("Color = %q, want %q", p.Color, ColorString)
	}
	if p.Space != SpaceSurface {
		t.Fatalf("Space = %q, want %q", p.Space, SpaceSurface)
	}
	if len(p.Tokens) != 0 {
		t.Fatalf("Tokens length = %d, want 0", len(p.Tokens))
	}
}

func TestPlace_Deposit(t *testing.T) {
	tests := []struct {
		name       string
		placeColor ColorSet
		placeSpace SpaceKind
		tokenColor ColorSet
		tokenSpace SpaceKind
		wantErr    error
	}{
		{"success_surface_to_surface", ColorString, SpaceSurface, ColorString, SpaceSurface, nil},
		{"success_obs_to_obs", ColorJSON, SpaceObservation, ColorJSON, SpaceObservation, nil},
		{"success_comp_to_comp", ColorArtifact, SpaceComputation, ColorArtifact, SpaceComputation, nil},
		{"color_mismatch", ColorString, SpaceSurface, ColorJSON, SpaceSurface, ErrColorMismatch},
		{"space_mismatch_obs_to_surf", ColorString, SpaceSurface, ColorString, SpaceObservation, ErrSpaceMismatch},
		{"space_mismatch_obs_to_comp", ColorJSON, SpaceComputation, ColorJSON, SpaceObservation, ErrSpaceMismatch},
		{"space_mismatch_comp_to_surf", ColorString, SpaceSurface, ColorString, SpaceComputation, ErrSpaceMismatch},
		{"space_violation_surf_to_comp", ColorString, SpaceComputation, ColorString, SpaceSurface, ErrSpaceViolation},
		{"violation_priority_color_also_wrong", ColorJSON, SpaceComputation, ColorString, SpaceSurface, ErrSpaceViolation},
		{"color_before_space_mismatch", ColorJSON, SpaceObservation, ColorString, SpaceSurface, ErrColorMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPlace("test-place", tt.placeColor, tt.placeSpace)
			tok := &Token{Color: tt.tokenColor, Space: tt.tokenSpace}
			err := p.Deposit(tok)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if p.Len() != 1 {
					t.Fatalf("expected 1 token, got %d", p.Len())
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestPlace_Deposit_ErrorContext(t *testing.T) {
	tests := []struct {
		name       string
		placeColor ColorSet
		placeSpace SpaceKind
		tokenColor ColorSet
		tokenSpace SpaceKind
		wantErr    error
		wantSubstr string
	}{
		{
			"space_violation_contains_place_id",
			ColorString, SpaceComputation,
			ColorString, SpaceSurface,
			ErrSpaceViolation, "P:ERR",
		},
		{
			"color_mismatch_contains_place_id",
			ColorString, SpaceSurface,
			ColorJSON, SpaceSurface,
			ErrColorMismatch, "P:ERR",
		},
		{
			"space_mismatch_contains_place_id",
			ColorString, SpaceSurface,
			ColorString, SpaceObservation,
			ErrSpaceMismatch, "P:ERR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPlace("P:ERR", tt.placeColor, tt.placeSpace)
			tok := &Token{Color: tt.tokenColor, Space: tt.tokenSpace}
			err := p.Deposit(tok)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestPlace_Consume_FIFO(t *testing.T) {
	p := NewPlace("fifo", ColorString, SpaceSurface)
	tokens := []*Token{
		{Color: ColorString, Space: SpaceSurface, Payload: "A"},
		{Color: ColorString, Space: SpaceSurface, Payload: "B"},
		{Color: ColorString, Space: SpaceSurface, Payload: "C"},
	}
	for _, tok := range tokens {
		if err := p.Deposit(tok); err != nil {
			t.Fatalf("deposit: %v", err)
		}
	}

	for i, want := range []string{"A", "B", "C"} {
		got, err := p.Consume()
		if err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
		if got.Payload != want {
			t.Fatalf("consume %d: payload = %v, want %v", i, got.Payload, want)
		}
	}
}

func TestPlace_Consume_EmptyPlace(t *testing.T) {
	p := NewPlace("empty", ColorString, SpaceSurface)
	_, err := p.Consume()
	if !errors.Is(err, ErrEmptyPlace) {
		t.Fatalf("expected ErrEmptyPlace, got %v", err)
	}
}

func TestPlace_Consume_AllThenEmpty(t *testing.T) {
	p := NewPlace("drain", ColorJSON, SpaceObservation)
	for i := 0; i < 5; i++ {
		if err := p.Deposit(&Token{Color: ColorJSON, Space: SpaceObservation, Payload: i}); err != nil {
			t.Fatalf("deposit %d: %v", i, err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := p.Consume(); err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
	}
	_, err := p.Consume()
	if !errors.Is(err, ErrEmptyPlace) {
		t.Fatalf("expected ErrEmptyPlace after draining, got %v", err)
	}
}

func TestPlace_Peek_ReturnsCopy(t *testing.T) {
	p := NewPlace("peek-copy", ColorString, SpaceSurface)
	if err := p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "original"}); err != nil {
		t.Fatal(err)
	}

	got, ok := p.Peek()
	if !ok {
		t.Fatal("Peek returned false, want true")
	}
	if len(got) != 1 {
		t.Fatalf("Peek len = %d, want 1", len(got))
	}

	// Mutate returned slice
	got[0] = Token{Color: ColorJSON, Space: SpaceObservation, Payload: "mutated"}

	// Re-peek should be unchanged
	got2, ok := p.Peek()
	if !ok {
		t.Fatal("second Peek returned false")
	}
	if got2[0].Payload != "original" {
		t.Fatalf("Peek returned mutated data: %v", got2[0].Payload)
	}
}

func TestPlace_Peek_EmptyPlace(t *testing.T) {
	p := NewPlace("empty-peek", ColorString, SpaceSurface)
	got, ok := p.Peek()
	if ok {
		t.Fatal("Peek on empty place returned true")
	}
	if got != nil {
		t.Fatalf("Peek on empty place returned %v, want nil", got)
	}
}

func TestPlace_Peek_NonDestructive(t *testing.T) {
	p := NewPlace("non-destructive", ColorString, SpaceSurface)
	tok := &Token{Color: ColorString, Space: SpaceSurface, Payload: "keep"}
	if err := p.Deposit(tok); err != nil {
		t.Fatal(err)
	}

	peeked, ok := p.Peek()
	if !ok || peeked[0].Payload != "keep" {
		t.Fatal("Peek did not return expected token")
	}

	consumed, err := p.Consume()
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Payload != "keep" {
		t.Fatalf("Consume after Peek: payload = %v, want %v", consumed.Payload, "keep")
	}
}

func TestPlace_Len(t *testing.T) {
	p := NewPlace("len-test", ColorString, SpaceSurface)
	if p.Len() != 0 {
		t.Fatalf("Len on new place = %d, want 0", p.Len())
	}

	for i := 1; i <= 3; i++ {
		if err := p.Deposit(&Token{Color: ColorString, Space: SpaceSurface}); err != nil {
			t.Fatal(err)
		}
		if p.Len() != i {
			t.Fatalf("after %d deposits, Len = %d", i, p.Len())
		}
	}

	if _, err := p.Consume(); err != nil {
		t.Fatal(err)
	}
	if p.Len() != 2 {
		t.Fatalf("after consume, Len = %d, want 2", p.Len())
	}
}

func TestPlace_Concurrent_DepositConsume(t *testing.T) {
	p := NewPlace("concurrent-dc", ColorString, SpaceSurface)
	const n = 1000
	var wg sync.WaitGroup

	// Deposit n tokens
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: i})
		}(i)
	}
	wg.Wait()

	if p.Len() != n {
		t.Fatalf("after %d concurrent deposits, Len = %d", n, p.Len())
	}

	// Consume n tokens concurrently
	consumed := make(chan Token, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tok, err := p.Consume()
			if err != nil {
				t.Errorf("consume error: %v", err)
				return
			}
			consumed <- tok
		}()
	}
	wg.Wait()
	close(consumed)

	if len(consumed) != n {
		t.Fatalf("consumed %d tokens, want %d", len(consumed), n)
	}
	if p.Len() != 0 {
		t.Fatalf("after consuming all, Len = %d, want 0", p.Len())
	}
}

func TestPlace_Concurrent_DepositPeek(t *testing.T) {
	p := NewPlace("concurrent-dp", ColorString, SpaceSurface)
	const n = 1000
	var wg sync.WaitGroup

	// Half deposit, half peek concurrently
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: i})
		}(i)
		go func() {
			defer wg.Done()
			p.Peek()
		}()
	}
	wg.Wait()

	if p.Len() != n {
		t.Fatalf("after concurrent deposit+peek, Len = %d, want %d", p.Len(), n)
	}
}

func TestPlace_Concurrent_MultipleConsumers(t *testing.T) {
	p := NewPlace("multi-consumer", ColorString, SpaceSurface)
	const total = 1000
	const consumers = 10

	// Deposit all tokens first
	for i := 0; i < total; i++ {
		if err := p.Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: i}); err != nil {
			t.Fatal(err)
		}
	}

	// Multiple consumers compete
	results := make(chan Token, total)
	errs := make(chan error, total)
	var wg sync.WaitGroup

	wg.Add(consumers)
	for c := 0; c < consumers; c++ {
		go func() {
			defer wg.Done()
			for {
				tok, err := p.Consume()
				if err != nil {
					if errors.Is(err, ErrEmptyPlace) {
						return
					}
					errs <- err
					return
				}
				results <- tok
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for range results {
		count++
	}
	if count != total {
		t.Fatalf("consumed %d tokens, want %d", count, total)
	}
}
