package cpn

import (
	"errors"
	"strings"
	"testing"
)

func TestAsSystemPrompt(t *testing.T) {
	p := DefaultPersonality()
	got := p.AsSystemPrompt()

	// Must start and end with fences.
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("expected prompt to start with ---\\n, got: %q", got[:20])
	}
	if !strings.HasSuffix(got, "---\n") {
		t.Fatalf("expected prompt to end with ---\\n")
	}

	// Must contain all section headers.
	for _, header := range []string{
		"# Agent Identity",
		"## Principle 1",
		"## Principle 2",
		"## Principle 3",
		"## Hierarchy",
		"## Tension Resolution",
	} {
		if !strings.Contains(got, header) {
			t.Errorf("missing header %q in prompt:\n%s", header, got)
		}
	}

	// Hierarchy line must reflect default order.
	if !strings.Contains(got, "etica > conducta > nucleo") {
		t.Errorf("hierarchy line not found in prompt:\n%s", got)
	}

	// Principles should be rendered in hierarchy order (etica first).
	eticaIdx := strings.Index(got, "Privacidad radical")
	nucleoIdx := strings.Index(got, "Comprension") //nolint:misspell // Spanish word
	if eticaIdx < 0 || nucleoIdx < 0 {
		t.Fatalf("principle titles not found in prompt")
	}
	if eticaIdx > nucleoIdx {
		t.Errorf("etica should appear before nucleo (hierarchy order)")
	}

	// Rules should appear as bullet points.
	if !strings.Contains(got, "- Toda llamada de red se anuncia ANTES de ejecutarse") {
		t.Errorf("etica rule not rendered")
	}
}

func TestValidateHierarchy_Valid(t *testing.T) {
	// All permutations where etica is NOT last.
	valid := [][3]PrincipleKind{
		{PrincipleEtica, PrincipleNucleo, PrincipleConducta},
		{PrincipleEtica, PrincipleConducta, PrincipleNucleo},
		{PrincipleNucleo, PrincipleEtica, PrincipleConducta},
		{PrincipleConducta, PrincipleEtica, PrincipleNucleo},
	}

	for _, h := range valid {
		p := DefaultPersonality()
		p.Hierarchy = h
		if err := p.ValidateHierarchy(); err != nil {
			t.Errorf("ValidateHierarchy(%v) = %v; want nil", h, err)
		}
	}
}

func TestValidateHierarchy_EticaLast(t *testing.T) {
	cases := [][3]PrincipleKind{
		{PrincipleNucleo, PrincipleConducta, PrincipleEtica},
		{PrincipleConducta, PrincipleNucleo, PrincipleEtica},
	}

	for _, h := range cases {
		p := DefaultPersonality()
		p.Hierarchy = h
		err := p.ValidateHierarchy()
		if err == nil {
			t.Errorf("ValidateHierarchy(%v) = nil; want ErrEticaCannotBeLast", h)
			continue
		}
		if !errors.Is(err, ErrEticaCannotBeLast) {
			t.Errorf("ValidateHierarchy(%v) = %v; want ErrEticaCannotBeLast", h, err)
		}
	}
}

func TestValidateHierarchy_MissingKind(t *testing.T) {
	p := DefaultPersonality()
	p.Hierarchy = [3]PrincipleKind{PrincipleEtica, PrincipleNucleo, PrincipleNucleo}

	err := p.ValidateHierarchy()
	if err == nil {
		t.Fatal("expected error for missing kind, got nil")
	}
	if !errors.Is(err, ErrInvalidHierarchy) {
		t.Errorf("expected ErrInvalidHierarchy, got: %v", err)
	}
}

func TestValidateHierarchy_DuplicateKind(t *testing.T) {
	p := DefaultPersonality()
	p.Hierarchy = [3]PrincipleKind{PrincipleEtica, PrincipleEtica, PrincipleNucleo}

	err := p.ValidateHierarchy()
	if err == nil {
		t.Fatal("expected error for duplicate kind, got nil")
	}
	if !errors.Is(err, ErrInvalidHierarchy) {
		t.Errorf("expected ErrInvalidHierarchy, got: %v", err)
	}
}

func TestValidatePrincipleUpdate_Conducta(t *testing.T) {
	p := DefaultPersonality()
	updated := Principle{
		Kind:        PrincipleConducta,
		Title:       "New Title",
		Description: "Totally changed",
		Rules:       []string{"only one rule"},
	}
	if err := p.ValidatePrincipleUpdate(PrincipleConducta, updated); err != nil {
		t.Errorf("expected nil for conducta update, got: %v", err)
	}
}

func TestValidatePrincipleUpdate_Nucleo(t *testing.T) {
	p := DefaultPersonality()
	updated := Principle{
		Kind:        PrincipleNucleo,
		Title:       "Changed",
		Description: "Different",
		Rules:       nil,
	}
	if err := p.ValidatePrincipleUpdate(PrincipleNucleo, updated); err != nil {
		t.Errorf("expected nil for nucleo update, got: %v", err)
	}
}

func TestValidatePrincipleUpdate_Etica_ExtendOK(t *testing.T) {
	p := DefaultPersonality()
	defaultEtica := defaultPersonality.findPrinciple(PrincipleEtica)

	// Keep all core rules and add one extra.
	extended := Principle{
		Kind:        PrincipleEtica,
		Title:       defaultEtica.Title,
		Description: defaultEtica.Description,
		Rules:       append(append([]string{}, defaultEtica.Rules...), "Extra rule: log all access attempts"),
	}
	if err := p.ValidatePrincipleUpdate(PrincipleEtica, extended); err != nil {
		t.Errorf("expected nil for extending etica rules, got: %v", err)
	}
}

func TestValidatePrincipleUpdate_Etica_RemoveRuleFails(t *testing.T) {
	p := DefaultPersonality()
	defaultEtica := defaultPersonality.findPrinciple(PrincipleEtica)

	// Remove the first core rule.
	trimmed := Principle{
		Kind:        PrincipleEtica,
		Title:       defaultEtica.Title,
		Description: defaultEtica.Description,
		Rules:       defaultEtica.Rules[1:], // drop first rule
	}
	err := p.ValidatePrincipleUpdate(PrincipleEtica, trimmed)
	if err == nil {
		t.Fatal("expected error when removing core etica rule, got nil")
	}
	if !errors.Is(err, ErrEticaViolation) {
		t.Errorf("expected ErrEticaViolation, got: %v", err)
	}
}

func TestDefaultPersonality(t *testing.T) {
	p := DefaultPersonality()
	if p == nil {
		t.Fatal("DefaultPersonality() returned nil")
	}

	// All principle kinds present.
	kinds := make(map[PrincipleKind]bool)
	for _, pr := range p.Principles {
		if pr.Kind == "" {
			t.Error("principle has empty Kind")
		}
		if pr.Title == "" {
			t.Error("principle has empty Title")
		}
		if pr.Description == "" {
			t.Error("principle has empty Description")
		}
		if len(pr.Rules) == 0 {
			t.Errorf("principle %s has no rules", pr.Kind)
		}
		kinds[pr.Kind] = true
	}
	if len(kinds) != 3 {
		t.Errorf("expected 3 distinct principle kinds, got %d", len(kinds))
	}

	// Hierarchy populated.
	for i, h := range p.Hierarchy {
		if h == "" {
			t.Errorf("hierarchy[%d] is empty", i)
		}
	}

	// Tensions populated.
	for i, tension := range p.Tensions {
		if tension.Friction == "" {
			t.Errorf("tension[%d] has empty Friction", i)
		}
		if tension.Resolution == "" {
			t.Errorf("tension[%d] has empty Resolution", i)
		}
	}

	// Validate hierarchy passes.
	if err := p.ValidateHierarchy(); err != nil {
		t.Errorf("default personality hierarchy invalid: %v", err)
	}

	// Returned value is a copy, not the package-level pointer.
	p.UserID = "mutated"
	p2 := DefaultPersonality()
	if p2.UserID == "mutated" {
		t.Error("DefaultPersonality() returned the same pointer, not a copy")
	}
}
