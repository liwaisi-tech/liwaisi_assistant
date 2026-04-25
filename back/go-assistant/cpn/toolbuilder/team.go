package toolbuilder

// reviewerRole couples a (profile, action) sub-agent with the CPN
// topology places it reads from and writes to. The SystemPrompt for
// the transition is composed at build time from the ProfileSpec and
// ActionSpec registries (see registry.go Compose). No prompt text
// lives in this file — that is the point of the (profile × action)
// decomposition.
type reviewerRole struct {
	TransitionID string
	InputPlace   string
	OutputPlace  string
	ProfileID    string // identity in ProfileRegistry
	ActionID     string // task in ActionRegistry
}

// reviewerRoles enumerates the tool-creator's parallel expert
// sub-agents. Order is stable; fanout + aggregator iterate in this
// order. The transition IDs MUST equal TransitionID(ActionID,
// ProfileID) — asserted by TestTransitionIDConvention.
//
// The team composition deliberately mirrors the three roles tool-creator
// ships with (arch, go-eng, devops): every spec passes through a
// hexagonal-architecture reviewer, an idiomatic-Go reviewer, and a
// pipeline/packaging reviewer.
var reviewerRoles = []reviewerRole{
	{TrReviewGo, PlaceSpecForGo, PlaceReviewGo, ProfileGoEng, ActionReviewSpec},
	{TrReviewDevOps, PlaceSpecForDevOps, PlaceReviewDevOps, ProfileDevOps, ActionReviewSpec},
	{TrReviewArch, PlaceSpecForArch, PlaceReviewArch, ProfileArch, ActionReviewSpec},
}

// ReviewerRoles returns the stable ordering of profile IDs used by the
// aggregator and by t-fanout-reviewers. Exposed for tests; copies the
// slice so callers cannot mutate internals. The strings returned are
// ProfileIDs (e.g. "go-eng", "devops") which now double as the
// Review.Role field value emitted by each reviewer sub-agent.
func ReviewerRoles() []string {
	out := make([]string, len(reviewerRoles))
	for i, r := range reviewerRoles {
		out[i] = r.ProfileID
	}
	return out
}
