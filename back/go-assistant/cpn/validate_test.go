package cpn

import (
	"errors"
	"strings"
	"testing"
)

// ── Test helpers ────────────────────────────────────────────────────────────

func makePlace(id string, space SpaceKind) *Place {
	return NewPlace(id, ColorString, space)
}

func makeTrans(id string, inputs, outputs []string) *Transition {
	return NewTransition(id, NodeKindTool, inputs, outputs)
}

// ── Nil / Empty inputs ─────────────────────────────────────────────────────

func TestValidate_NilInputs(t *testing.T) {
	if err := Validate(nil, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_EmptyMaps(t *testing.T) {
	if err := Validate(map[string]*Place{}, map[string]*Transition{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// ── Valid topologies ────────────────────────────────────────────────────────

func TestValidate_ValidTopology(t *testing.T) {
	places := map[string]*Place{
		"P:A": makePlace("P:A", SpaceSurface),
		"P:B": makePlace("P:B", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:A"}, []string{"P:B"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_ValidMultiTransition(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceSurface),
		"P:MID": makePlace("P:MID", SpaceObservation),
		"P:OUT": makePlace("P:OUT", SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:parse":   makeTrans("T:parse", []string{"P:IN"}, []string{"P:MID"}),
		"T:compute": makeTrans("T:compute", []string{"P:MID"}, []string{"P:OUT"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// ── ErrInvalidArc ───────────────────────────────────────────────────────────

func TestValidate_InvalidInputArc(t *testing.T) {
	places := map[string]*Place{
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:MISSING"}, []string{"P:OUT"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArc) {
		t.Fatalf("expected ErrInvalidArc, got %v", err)
	}
}

func TestValidate_InvalidOutputArc(t *testing.T) {
	places := map[string]*Place{
		"P:IN": makePlace("P:IN", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:IN"}, []string{"P:MISSING"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArc) {
		t.Fatalf("expected ErrInvalidArc, got %v", err)
	}
}

func TestValidate_InvalidErrorPlace(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceSurface),
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	tr := makeTrans("T:1", []string{"P:IN"}, []string{"P:OUT"})
	tr.ErrorPlace = "P:MISSING"
	transitions := map[string]*Transition{"T:1": tr}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArc) {
		t.Fatalf("expected ErrInvalidArc, got %v", err)
	}
}

func TestValidate_EmptyErrorPlace_NoError(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceSurface),
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	tr := makeTrans("T:1", []string{"P:IN"}, []string{"P:OUT"})
	tr.ErrorPlace = ""
	transitions := map[string]*Transition{"T:1": tr}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_MultipleInvalidArcs(t *testing.T) {
	places := map[string]*Place{
		"P:OK": makePlace("P:OK", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:A": makeTrans("T:A", []string{"P:X"}, []string{"P:OK"}),
		"T:B": makeTrans("T:B", []string{"P:OK"}, []string{"P:Y", "P:Z"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}

	var ve *ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(ve.Errors) != 3 {
		t.Fatalf("expected 3 errors, got %d: %v", len(ve.Errors), ve.Errors)
	}
}

// ── ErrSpaceViolation ───────────────────────────────────────────────────────

func TestValidate_SpaceViolation_SurfaceToComp(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceSurface),
		"P:OUT": makePlace("P:OUT", SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:BAD": makeTrans("T:BAD", []string{"P:IN"}, []string{"P:OUT"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !errors.Is(err, ErrSpaceViolation) {
		t.Fatalf("expected ErrSpaceViolation, got %v", err)
	}
}

func TestValidate_NoViolation_SurfaceToObs(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceSurface),
		"P:OUT": makePlace("P:OUT", SpaceObservation),
	}
	transitions := map[string]*Transition{
		"T:OK": makeTrans("T:OK", []string{"P:IN"}, []string{"P:OUT"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_NoViolation_ObsToComp(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  makePlace("P:IN", SpaceObservation),
		"P:OUT": makePlace("P:OUT", SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:OK": makeTrans("T:OK", []string{"P:IN"}, []string{"P:OUT"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_NoViolation_SameSpace(t *testing.T) {
	spaces := []SpaceKind{SpaceSurface, SpaceObservation, SpaceComputation}
	for _, sp := range spaces {
		places := map[string]*Place{
			"P:A": makePlace("P:A", sp),
			"P:B": makePlace("P:B", sp),
		}
		transitions := map[string]*Transition{
			"T:1": makeTrans("T:1", []string{"P:A"}, []string{"P:B"}),
		}
		if err := Validate(places, transitions); err != nil {
			t.Fatalf("space %s: expected nil, got %v", sp, err)
		}
	}
}

// ── Mixed errors ────────────────────────────────────────────────────────────

func TestValidate_MixedErrors(t *testing.T) {
	places := map[string]*Place{
		"P:SURF": makePlace("P:SURF", SpaceSurface),
		"P:COMP": makePlace("P:COMP", SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:BAD-ARC":   makeTrans("T:BAD-ARC", []string{"P:MISSING"}, []string{"P:SURF"}),
		"T:BAD-SPACE": makeTrans("T:BAD-SPACE", []string{"P:SURF"}, []string{"P:COMP"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArc) {
		t.Fatal("expected ErrInvalidArc in mixed errors")
	}
	if !errors.Is(err, ErrSpaceViolation) {
		t.Fatal("expected ErrSpaceViolation in mixed errors")
	}
}

// ── Deterministic ordering ──────────────────────────────────────────────────

func TestValidate_DeterministicOrder(t *testing.T) {
	places := map[string]*Place{
		"P:OK": makePlace("P:OK", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:C": makeTrans("T:C", []string{"P:X"}, []string{"P:OK"}),
		"T:A": makeTrans("T:A", []string{"P:Y"}, []string{"P:OK"}),
		"T:B": makeTrans("T:B", []string{"P:Z"}, []string{"P:OK"}),
	}

	err1 := Validate(places, transitions)
	err2 := Validate(places, transitions)

	if err1 == nil || err2 == nil {
		t.Fatal("expected errors")
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("non-deterministic:\n  run1: %s\n  run2: %s", err1, err2)
	}

	// Verify alphabetical transition order: T:A, T:B, T:C
	msg := err1.Error()
	idxA := strings.Index(msg, "T:A")
	idxB := strings.Index(msg, "T:B")
	idxC := strings.Index(msg, "T:C")
	if idxA > idxB || idxB > idxC {
		t.Fatalf("expected T:A before T:B before T:C in: %s", msg)
	}
}

// ── ValidationErrors type ───────────────────────────────────────────────────

func TestValidationErrors_Error(t *testing.T) {
	ve := &ValidationErrors{
		Errors: []error{
			errors.New("error one"),
			errors.New("error two"),
		},
	}

	msg := ve.Error()
	if !strings.Contains(msg, "validation failed (2 errors):") {
		t.Fatalf("unexpected format: %s", msg)
	}
	if !strings.Contains(msg, "  - error one") {
		t.Fatalf("missing error one: %s", msg)
	}
	if !strings.Contains(msg, "  - error two") {
		t.Fatalf("missing error two: %s", msg)
	}
}

func TestValidationErrors_Unwrap(t *testing.T) {
	inner := []error{ErrInvalidArc, ErrSpaceViolation}
	ve := &ValidationErrors{Errors: inner}

	unwrapped := ve.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("expected 2, got %d", len(unwrapped))
	}
	if !errors.Is(ve, ErrInvalidArc) {
		t.Fatal("errors.Is should find ErrInvalidArc")
	}
	if !errors.Is(ve, ErrSpaceViolation) {
		t.Fatal("errors.Is should find ErrSpaceViolation")
	}
}

func TestValidationErrors_ErrorsAs(t *testing.T) {
	places := map[string]*Place{
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:MISSING"}, []string{"P:OUT"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}

	var ve *ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected errors.As to succeed, got %T", err)
	}
	if len(ve.Errors) == 0 {
		t.Fatal("expected at least one contained error")
	}
}

// ── Edge cases ──────────────────────────────────────────────────────────────

func TestValidate_TransitionNoInputPlaces(t *testing.T) {
	places := map[string]*Place{
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", nil, []string{"P:OUT"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil for transition with no inputs, got %v", err)
	}
}

func TestValidate_TransitionNoOutputPlaces(t *testing.T) {
	places := map[string]*Place{
		"P:IN": makePlace("P:IN", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:IN"}, nil),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil for transition with no outputs, got %v", err)
	}
}

func TestValidate_ErrorMessageContainsIDs(t *testing.T) {
	places := map[string]*Place{
		"P:OUT": makePlace("P:OUT", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:CHECK": makeTrans("T:CHECK", []string{"P:GHOST"}, []string{"P:OUT"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "T:CHECK") {
		t.Fatalf("error should contain transition ID 'T:CHECK': %s", msg)
	}
	if !strings.Contains(msg, "P:GHOST") {
		t.Fatalf("error should contain place ID 'P:GHOST': %s", msg)
	}
}

func TestValidate_DuplicateInputPlace(t *testing.T) {
	places := map[string]*Place{
		"P:A": makePlace("P:A", SpaceSurface),
		"P:B": makePlace("P:B", SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:A", "P:A"}, []string{"P:B"}),
	}

	if err := Validate(places, transitions); err != nil {
		t.Fatalf("expected nil for duplicate input place, got %v", err)
	}
}

func TestValidate_AllPlacesMissing(t *testing.T) {
	places := map[string]*Place{}
	transitions := map[string]*Transition{
		"T:1": makeTrans("T:1", []string{"P:A", "P:B"}, []string{"P:C"}),
	}

	err := Validate(places, transitions)
	if err == nil {
		t.Fatal("expected error")
	}
	var ve *ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(ve.Errors) != 3 {
		t.Fatalf("expected 3 errors (one per missing ref), got %d", len(ve.Errors))
	}
}
