package valueobject

import "fmt"

// PlanLifecycle represents the lifecycle state of a persisted plan document.
type PlanLifecycle string

const (
	// PlanLifecycleActive means the plan is current and ready for execution.
	PlanLifecycleActive PlanLifecycle = "active"
	// PlanLifecycleClosed means the plan was explicitly closed by the user.
	PlanLifecycleClosed PlanLifecycle = "closed"
	// PlanLifecycleOverridden means the plan was superseded by a new plan.
	PlanLifecycleOverridden PlanLifecycle = "overridden"
)

var validLifecycles = map[PlanLifecycle]struct{}{
	PlanLifecycleActive:     {},
	PlanLifecycleClosed:     {},
	PlanLifecycleOverridden: {},
}

// IsTerminal reports whether the lifecycle represents a finished state.
func (l PlanLifecycle) IsTerminal() bool {
	return l == PlanLifecycleClosed || l == PlanLifecycleOverridden
}

// IsValid reports whether the lifecycle value is a known valid state.
func (l PlanLifecycle) IsValid() bool {
	_, ok := validLifecycles[l]
	return ok
}

// Validate returns an error if the lifecycle is not valid.
func (l PlanLifecycle) Validate() error {
	if !l.IsValid() {
		return fmt.Errorf("invalid plan lifecycle: %q", string(l))
	}
	return nil
}

// String returns the string representation of the lifecycle.
func (l PlanLifecycle) String() string {
	return string(l)
}
