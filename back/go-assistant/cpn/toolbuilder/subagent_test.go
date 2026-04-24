package toolbuilder

import (
	"regexp"
	"strings"
	"testing"
)

// TestProfileSpec_NoActionVerbs asserts the foundational invariant of
// PR1: profile text describes WHO a sub-agent is, not WHAT it does. No
// profile field may contain an action verb. When this test fails, the
// profile has been contaminated with action-specific text and the
// (profile × action) decomposition has regressed.
func TestProfileSpec_NoActionVerbs(t *testing.T) {
	verbs := []string{"review", "reviewing", "audit", "auditing", "build", "building", "evaluate", "evaluating"}
	for _, p := range seedProfiles() {
		blob := strings.ToLower(strings.Join([]string{
			p.Persona, p.DomainLens, p.Voice, p.EpistemicLimits,
			strings.Join(p.RedFlags, " "),
		}, " "))
		for _, v := range verbs {
			// Word-boundary match so "architect" doesn't trip on "arch".
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\b`)
			if re.MatchString(blob) {
				t.Errorf("profile %q contains action verb %q — profile must stay action-agnostic", p.ID, v)
			}
		}
	}
}

// TestProfileSpec_NoSchemaFieldNames asserts profile text never names
// a verdict-schema field (approved, blocking_issues, suggestions) —
// those belong in ActionSpec.OutputSchema.
func TestProfileSpec_NoSchemaFieldNames(t *testing.T) {
	fields := []string{"approved", "blocking_issues", "suggestions"}
	for _, p := range seedProfiles() {
		blob := strings.ToLower(strings.Join([]string{
			p.Persona, p.DomainLens, p.Voice, p.EpistemicLimits,
			strings.Join(p.RedFlags, " "),
		}, " "))
		for _, f := range fields {
			if strings.Contains(blob, f) {
				t.Errorf("profile %q references schema field %q — schema belongs to ActionSpec only", p.ID, f)
			}
		}
	}
}

// TestCatalog_SeedsWithoutError is a smoke check on registry seeding.
func TestCatalog_SeedsWithoutError(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatalf("NewSubAgentCatalog: %v", err)
	}
	wantProfiles := []string{ProfilePM, ProfileArch, ProfileQA, ProfileDevOps, ProfileAIEng, ProfileGoEng}
	for _, id := range wantProfiles {
		if _, ok := cat.Profiles.Get(id); !ok {
			t.Errorf("profile %q missing from registry", id)
		}
	}
	if _, ok := cat.Actions.Get(ActionReviewSpec); !ok {
		t.Errorf("action %q missing from registry", ActionReviewSpec)
	}
}

// TestCompose_Deterministic asserts Compose() is stable across calls.
// Snapshot-level guarantee: the same (profile, action) pair always
// renders the same bytes. If this fails, some non-stable ordering
// crept into the renderer.
func TestCompose_Deterministic(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	first, err := cat.Compose(ProfileGoEng, ActionReviewSpec)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		again, err := cat.Compose(ProfileGoEng, ActionReviewSpec)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("Compose not deterministic on iteration %d", i)
		}
	}
}

// TestCompose_ContainsAllBlocks asserts the composed prompt actually
// stacks guardrails + profile + action + output schema in that order.
func TestCompose_ContainsAllBlocks(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.Compose(ProfileQA, ActionReviewSpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"# GUARDRAILS", "# PROFILE:", "# ACTION:", "# OUTPUT SCHEMA"} {
		if !strings.Contains(got, marker) {
			t.Errorf("composed prompt missing marker %q", marker)
		}
	}
	// Order check: guardrails < profile < action < schema.
	gi := strings.Index(got, "# GUARDRAILS")
	pi := strings.Index(got, "# PROFILE:")
	ai := strings.Index(got, "# ACTION:")
	si := strings.Index(got, "# OUTPUT SCHEMA")
	if !(gi < pi && pi < ai && ai < si) {
		t.Errorf("composed prompt blocks out of order: guardrails=%d profile=%d action=%d schema=%d", gi, pi, ai, si)
	}
}

// TestCompose_UnknownIDs asserts Compose fails closed when asked for a
// profile or action that isn't in the registry.
func TestCompose_UnknownIDs(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Compose("no-such-profile", ActionReviewSpec); err == nil {
		t.Error("expected error for unknown profile")
	}
	if _, err := cat.Compose(ProfileQA, "no-such-action"); err == nil {
		t.Error("expected error for unknown action")
	}
}

// TestTransitionIDConvention asserts every reviewer transition ID in
// the topology equals TransitionID(ActionReviewSpec, ProfileID). This
// is the load-bearing invariant that lets the FE derive (profile,
// action) from transition IDs without a sidecar lookup.
func TestTransitionIDConvention(t *testing.T) {
	for _, r := range reviewerRoles {
		want := TransitionID(r.ActionID, r.ProfileID)
		if r.TransitionID != want {
			t.Errorf("reviewer profile=%s action=%s transition_id=%q, want %q",
				r.ProfileID, r.ActionID, r.TransitionID, want)
		}
	}
}

// TestToolAtelier_ReviewerMetaPopulated asserts every reviewer
// transition built by BuildToolAtelierTopology carries the sub-agent
// Meta the visualizer expects.
func TestToolAtelier_ReviewerMetaPopulated(t *testing.T) {
	c := BuildToolAtelierTopology("test-session", AtelierDeps{})
	for _, r := range reviewerRoles {
		tr, ok := c.Transitions[r.TransitionID]
		if !ok {
			t.Fatalf("transition %q missing from topology", r.TransitionID)
		}
		if tr.Meta == nil {
			t.Errorf("transition %q has nil Meta", r.TransitionID)
			continue
		}
		for _, key := range []string{"kind", "profile_id", "action_id", "subagent_label", "icon_key"} {
			if tr.Meta[key] == "" {
				t.Errorf("transition %q Meta[%q] is empty", r.TransitionID, key)
			}
		}
		if tr.Meta["kind"] != "subagent" {
			t.Errorf("transition %q kind=%q, want subagent", r.TransitionID, tr.Meta["kind"])
		}
		if tr.Meta["profile_id"] != r.ProfileID {
			t.Errorf("transition %q profile_id=%q, want %q", r.TransitionID, tr.Meta["profile_id"], r.ProfileID)
		}
		if tr.Meta["action_id"] != r.ActionID {
			t.Errorf("transition %q action_id=%q, want %q", r.TransitionID, tr.Meta["action_id"], r.ActionID)
		}
	}
}

// TestEffectiveTools_IntersectionSemantics asserts the deny-by-default
// intersection rule: empty on either side ⇒ empty result.
func TestEffectiveTools_IntersectionSemantics(t *testing.T) {
	cat, err := NewSubAgentCatalog()
	if err != nil {
		t.Fatal(err)
	}
	// PR1 profiles have nil MaxTools and review-spec has nil
	// RequiredTools — intersection MUST be empty.
	tools, err := cat.EffectiveTools(ProfileQA, ActionReviewSpec)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tool set, got %v", tools)
	}
}

// TestNoOpValidator_Accepts_All keeps the PR2 contract honest: the
// default validator must accept every payload so plumbing doesn't
// silently reject output before PR2 lands real schemas.
func TestNoOpValidator_Accepts_All(t *testing.T) {
	v := NoOpValidator{}
	if err := v.Validate(ActionSpec{}, []byte("not json")); err != nil {
		t.Errorf("NoOpValidator rejected payload: %v", err)
	}
	if err := v.Validate(ActionSpec{}, nil); err != nil {
		t.Errorf("NoOpValidator rejected nil: %v", err)
	}
}
