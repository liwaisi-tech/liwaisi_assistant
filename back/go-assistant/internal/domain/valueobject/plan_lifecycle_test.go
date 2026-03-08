package valueobject

import "testing"

func TestPlanLifecycle_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		lc       PlanLifecycle
		terminal bool
	}{
		{"active is not terminal", PlanLifecycleActive, false},
		{"closed is terminal", PlanLifecycleClosed, true},
		{"overridden is terminal", PlanLifecycleOverridden, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lc.IsTerminal(); got != tt.terminal {
				t.Errorf("PlanLifecycle(%q).IsTerminal() = %v, want %v", tt.lc, got, tt.terminal)
			}
		})
	}
}

func TestPlanLifecycle_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		lc    PlanLifecycle
		valid bool
	}{
		{"active", PlanLifecycleActive, true},
		{"closed", PlanLifecycleClosed, true},
		{"overridden", PlanLifecycleOverridden, true},
		{"empty", PlanLifecycle(""), false},
		{"unknown", PlanLifecycle("deleted"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lc.IsValid(); got != tt.valid {
				t.Errorf("PlanLifecycle(%q).IsValid() = %v, want %v", tt.lc, got, tt.valid)
			}
		})
	}
}

func TestPlanLifecycle_Validate(t *testing.T) {
	if err := PlanLifecycleActive.Validate(); err != nil {
		t.Errorf("Validate() for active: unexpected error: %v", err)
	}
	if err := PlanLifecycle("bogus").Validate(); err == nil {
		t.Error("Validate() for bogus: expected error, got nil")
	}
}

func TestPlanLifecycle_String(t *testing.T) {
	if got := PlanLifecycleActive.String(); got != "active" {
		t.Errorf("String() = %q, want %q", got, "active")
	}
}
