package toolbuilder

import (
	"strings"
	"testing"
)

func TestStrictJSONValidator_ReviewSpec_Accepts(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewSpec}
	ok := []byte(`{"role":"go-eng","approved":true,"blocking_issues":[],"suggestions":[]}`)
	if err := v.Validate(action, ok); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestStrictJSONValidator_ReviewSpec_RejectsUnknownField(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewSpec}
	bad := []byte(`{"role":"go-eng","approved":true,"blocking_issues":[],"suggestions":[],"exfiltrated":"secret"}`)
	err := v.Validate(action, bad)
	if err == nil {
		t.Fatal("expected rejection of unknown field")
	}
	if !strings.Contains(err.Error(), "unknown fields") {
		t.Errorf("error should mention unknown fields, got: %v", err)
	}
}

func TestStrictJSONValidator_ReviewSpec_RejectsMissingRequired(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewSpec}
	bad := []byte(`{"role":"go-eng","approved":true}`)
	err := v.Validate(action, bad)
	if err == nil {
		t.Fatal("expected rejection for missing fields")
	}
	if !strings.Contains(err.Error(), "missing required fields") {
		t.Errorf("error should mention missing required, got: %v", err)
	}
}

func TestStrictJSONValidator_RejectsMalformedJSON(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewSpec}
	if err := v.Validate(action, []byte("not json")); err == nil {
		t.Fatal("expected rejection of malformed JSON")
	}
}

func TestStrictJSONValidator_ReviewCode_Accepts(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewCode}
	ok := []byte(`{"role":"go-eng","approved":true,"findings":[],"style_notes":[],"suggested_patches":[]}`)
	if err := v.Validate(action, ok); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestStrictJSONValidator_MaxBytes(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionReviewSpec}
	big := `{"role":"go-eng","approved":true,"blocking_issues":[],"suggestions":["` + strings.Repeat("x", 17*1024) + `"]}`
	err := v.Validate(action, []byte(big))
	if err == nil {
		t.Fatal("expected rejection for oversized payload")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Errorf("error should mention max, got: %v", err)
	}
}

func TestStrictJSONValidator_UnknownAction_AcceptsValidJSON(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: "no-such-action"}
	if err := v.Validate(action, []byte(`{"anything":1}`)); err != nil {
		t.Fatalf("unknown action with valid JSON should pass, got: %v", err)
	}
	if err := v.Validate(action, []byte("not json")); err == nil {
		t.Fatal("unknown action with bad JSON should fail")
	}
}

// tool-creator uses exactly three profiles (arch, go-eng, devops).
// review-spec must be open to all three; refine-spec / decompose-totals /
// plan-subtasks are arch-only.
func TestAllowlistMatrix_ReviewSpecOpenToThreeProfiles(t *testing.T) {
	for _, p := range []string{ProfileArch, ProfileGoEng, ProfileDevOps} {
		if err := IsAllowed(p, ActionReviewSpec); err != nil {
			t.Errorf("%s×review-spec should be allowed: %v", p, err)
		}
	}
}

func TestAllowlistMatrix_RefineSpecRestrictedToArch(t *testing.T) {
	if err := IsAllowed(ProfileArch, ActionRefineSpec); err != nil {
		t.Errorf("arch×refine-spec should be allowed: %v", err)
	}
	for _, p := range []string{ProfileGoEng, ProfileDevOps} {
		if err := IsAllowed(p, ActionRefineSpec); err == nil {
			t.Errorf("%s×refine-spec should be denied", p)
		}
	}
}

func TestAllowlistMatrix_ReviewCodeRestrictedToEngineers(t *testing.T) {
	for _, p := range []string{ProfileArch, ProfileGoEng} {
		if err := IsAllowed(p, ActionReviewCode); err != nil {
			t.Errorf("%s×review-code should be allowed: %v", p, err)
		}
	}
	if err := IsAllowed(ProfileDevOps, ActionReviewCode); err == nil {
		t.Error("devops×review-code should be denied")
	}
}

func TestAllowlistMatrix_UnknownActionDeniedByDefault(t *testing.T) {
	if err := IsAllowed(ProfileGoEng, "no-such-action"); err == nil {
		t.Error("unknown action should be deny-by-default")
	}
}

func TestCompose_RespectsMatrix(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	// devops×review-code is denied — Compose must fail.
	if _, err := cat.Compose(ProfileDevOps, ActionReviewCode); err == nil {
		t.Error("Compose should refuse denied (profile, action) pair")
	}
	// go-eng×review-code is allowed.
	if _, err := cat.Compose(ProfileGoEng, ActionReviewCode); err != nil {
		t.Errorf("Compose should accept allowed pair: %v", err)
	}
}
