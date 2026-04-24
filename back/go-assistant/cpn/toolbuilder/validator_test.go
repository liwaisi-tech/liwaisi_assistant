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

func TestStrictJSONValidator_SecurityEval_Accepts(t *testing.T) {
	v := StrictJSONValidator{}
	action := ActionSpec{ID: ActionSecurityEval}
	ok := []byte(`{"role":"security","approved":false,"threats":[{"id":"T1","stride":"I","severity":"high","mitigation":"move to env"}],"required_controls":["no-secrets-in-source"]}`)
	if err := v.Validate(action, ok); err != nil {
		t.Fatalf("expected accept, got %v", err)
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

func TestAllowlistMatrix_SecurityProfileGatesSecurityEval(t *testing.T) {
	// security-eval is restricted to the security profile.
	if err := IsAllowed(ProfileSecurity, ActionSecurityEval); err != nil {
		t.Errorf("security×security-eval should be allowed: %v", err)
	}
	if err := IsAllowed(ProfileGoEng, ActionSecurityEval); err == nil {
		t.Error("go-eng×security-eval should be denied")
	}
}

func TestAllowlistMatrix_ReviewSpecOpenToAll(t *testing.T) {
	for _, p := range []string{ProfilePM, ProfileArch, ProfileQA, ProfileDevOps, ProfileAIEng, ProfileGoEng, ProfileSecurity} {
		if err := IsAllowed(p, ActionReviewSpec); err != nil {
			t.Errorf("%s×review-spec should be allowed: %v", p, err)
		}
	}
}

func TestAllowlistMatrix_RefineSpecRestrictedToArch(t *testing.T) {
	if err := IsAllowed(ProfileArch, ActionRefineSpec); err != nil {
		t.Errorf("arch×refine-spec should be allowed: %v", err)
	}
	for _, p := range []string{ProfilePM, ProfileQA, ProfileDevOps, ProfileAIEng, ProfileGoEng, ProfileSecurity} {
		if err := IsAllowed(p, ActionRefineSpec); err == nil {
			t.Errorf("%s×refine-spec should be denied", p)
		}
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
	// go-eng×security-eval is denied — Compose must fail.
	if _, err := cat.Compose(ProfileGoEng, ActionSecurityEval); err == nil {
		t.Error("Compose should refuse denied (profile, action) pair")
	}
	// security×security-eval is allowed.
	if _, err := cat.Compose(ProfileSecurity, ActionSecurityEval); err != nil {
		t.Errorf("Compose should accept allowed pair: %v", err)
	}
}

func TestSecurityProfile_Registered(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cat.Profiles.Get(ProfileSecurity)
	if !ok {
		t.Fatal("security profile not registered")
	}
	// The security profile text must claim read-only authority — this is
	// the load-bearing invariant that keeps the profile from producing
	// executable artifacts.
	blob := strings.ToLower(p.Persona + " " + p.EpistemicLimits)
	if !strings.Contains(blob, "read-only") {
		t.Errorf("security profile must declare read-only authority, got persona=%q limits=%q", p.Persona, p.EpistemicLimits)
	}
}

func TestActionCapabilities_SecurityEvalIsAdvisory(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := cat.Actions.Get(ActionSecurityEval)
	if !a.Capabilities.AdvisoryOnly {
		t.Error("security-eval must be AdvisoryOnly — it cannot emit executable artifacts")
	}
	if a.Capabilities.EmitsCode || a.Capabilities.EmitsTests {
		t.Error("security-eval must NOT declare EmitsCode/EmitsTests")
	}
}
