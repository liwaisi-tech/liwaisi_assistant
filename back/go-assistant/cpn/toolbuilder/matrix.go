package toolbuilder

import "fmt"

// allowlistMatrix is the deny-by-default sparse matrix of permitted
// (profile, action) pairs. Adding a new action requires adding it to
// this matrix — silent permission creep is the main risk the matrix
// exists to block.
//
// Rows: action IDs. Values: the profiles allowed to run that action.
// The empty-map case (action not present) denies ALL profiles.
//
// tool-creator collapses the catalog to 3 profiles (arch, go-eng,
// devops). review-spec is open to all three; refine-spec /
// decompose-totals / plan-subtasks are arch-only; review-code is
// restricted to engineering reviewers.
var allowlistMatrix = map[string]map[string]struct{}{
	ActionReviewSpec: setOf(
		ProfileArch, ProfileGoEng, ProfileDevOps,
	),
	ActionReviewCode: setOf(
		ProfileGoEng, ProfileArch,
	),
	ActionRefineSpec: setOf(
		ProfileArch,
	),
	ActionDecomposeTotals: setOf(
		ProfileArch,
	),
	ActionPlanSubtasks: setOf(
		ProfileArch,
	),
}

func setOf(ids ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// IsAllowed returns nil when (profileID, actionID) is in the
// allowlist, or an error describing why the pair is denied.
// Deny-by-default: an unknown action OR a known action with no entry
// for the given profile both return an error.
func IsAllowed(profileID, actionID string) error {
	profiles, ok := allowlistMatrix[actionID]
	if !ok {
		return fmt.Errorf("allowlist: action %q has no matrix entry (deny-by-default)", actionID)
	}
	if _, ok := profiles[profileID]; !ok {
		return fmt.Errorf("allowlist: profile %q is not permitted to run action %q", profileID, actionID)
	}
	return nil
}

// AllowedProfiles returns the profiles permitted to run the given
// action. Returns nil for unknown actions.
func AllowedProfiles(actionID string) []string {
	m, ok := allowlistMatrix[actionID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
