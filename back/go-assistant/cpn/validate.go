package cpn

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationErrors collects all structural problems found during validation.
// Implements the error interface with Go 1.20+ multi-unwrap semantics.
//
// Usage:
//
//	err := Validate(places, transitions)
//	if err != nil {
//	    if errors.Is(err, ErrInvalidArc) { /* at least one bad arc */ }
//	    if errors.Is(err, ErrSpaceViolation) { /* at least one space bypass */ }
//	    var ve *ValidationErrors
//	    if errors.As(err, &ve) {
//	        for _, e := range ve.Errors { fmt.Println(e) }
//	    }
//	}
type ValidationErrors struct {
	Errors []error
}

// Error returns a formatted multi-line error string.
func (ve *ValidationErrors) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "validation failed (%d errors):", len(ve.Errors))
	for _, e := range ve.Errors {
		fmt.Fprintf(&b, "\n  - %s", e.Error())
	}
	return b.String()
}

// Unwrap returns all contained errors for Go 1.20+ multi-unwrap.
// Enables errors.Is(validationErr, ErrInvalidArc) to search all contained errors.
func (ve *ValidationErrors) Unwrap() []error {
	return ve.Errors
}

// Validate checks the structural integrity of a CPN topology.
//
// Checks performed:
//   - Arc reference integrity: all place IDs in transitions must exist in the places map
//   - Space violation: no transition may wire Surface inputs directly to Computation outputs
//
// Returns nil if the topology is valid. Returns *ValidationErrors with all
// problems found if invalid. Use errors.Is() to check for specific sentinel errors.
//
// Iteration order is deterministic (transitions sorted by ID).
func Validate(places map[string]*Place, transitions map[string]*Transition) error {
	if len(transitions) == 0 {
		return nil
	}

	ids := make([]string, 0, len(transitions))
	for id := range transitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var errs []error

	for _, id := range ids {
		tr := transitions[id]

		arcErrs := checkArcReferences(tr, places)
		errs = append(errs, arcErrs...)

		// Skip space violation check if this transition already has invalid arcs (GUD-002).
		// GUD-001: NodeKindHITL is exempt — HITL is the sanctioned space bridge.
		if len(arcErrs) == 0 && tr.Kind != NodeKindHITL {
			errs = append(errs, checkSpaceViolations(tr, places)...)
		}

		// REQ-018: Validate HITL transitions have proper configuration.
		errs = append(errs, checkHITLConfig(tr)...)
	}

	if len(errs) == 0 {
		return nil
	}
	return &ValidationErrors{Errors: errs}
}

// checkArcReferences verifies all place references in a transition exist.
func checkArcReferences(t *Transition, places map[string]*Place) []error {
	var errs []error

	for _, pid := range t.InputPlaces {
		if _, ok := places[pid]; !ok {
			errs = append(errs, fmt.Errorf("%w: transition %s references non-existent input place %s",
				ErrInvalidArc, t.ID, pid))
		}
	}

	for _, pid := range t.OutputPlaces {
		if _, ok := places[pid]; !ok {
			errs = append(errs, fmt.Errorf("%w: transition %s references non-existent output place %s",
				ErrInvalidArc, t.ID, pid))
		}
	}

	if t.ErrorPlace != "" {
		if _, ok := places[t.ErrorPlace]; !ok {
			errs = append(errs, fmt.Errorf("%w: transition %s references non-existent error place %s",
				ErrInvalidArc, t.ID, t.ErrorPlace))
		}
	}

	return errs
}

// checkHITLConfig validates that NodeKindHITL transitions have proper configuration.
// REQ-018: HITLConfig must be non-nil and Channel must be non-nil.
func checkHITLConfig(t *Transition) []error {
	if t.Kind != NodeKindHITL {
		return nil
	}
	var errs []error
	if t.HITLConfig == nil {
		errs = append(errs, fmt.Errorf("%w: transition %s has nil HITLConfig",
			ErrHITLMisconfigured, t.ID))
		return errs // Channel check is meaningless without config.
	}
	if t.HITLConfig.Channel == nil {
		errs = append(errs, fmt.Errorf("%w: transition %s has nil HITLConfig.Channel",
			ErrHITLMisconfigured, t.ID))
	}
	return errs
}

// checkSpaceViolations detects Surface→Computation direct wiring.
func checkSpaceViolations(t *Transition, places map[string]*Place) []error {
	var errs []error

	hasSurfaceInput := false
	for _, pid := range t.InputPlaces {
		if p, ok := places[pid]; ok && p.Space == SpaceSurface {
			hasSurfaceInput = true
			break
		}
	}

	if !hasSurfaceInput {
		return nil
	}

	for _, pid := range t.OutputPlaces {
		if p, ok := places[pid]; ok && p.Space == SpaceComputation {
			errs = append(errs, fmt.Errorf(
				"%w: transition %s wires Surface input to Computation output %s",
				ErrSpaceViolation, t.ID, pid))
		}
	}

	return errs
}
