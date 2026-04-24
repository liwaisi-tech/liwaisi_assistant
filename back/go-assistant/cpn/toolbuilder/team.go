package toolbuilder

// reviewerRole couples a (profile, action) sub-agent with the CPN
// topology places it reads from and writes to. The SystemPrompt for
// the transition is composed at build time from the ProfileSpec and
// ActionSpec registries (see registry.go Compose). No prompt text
// lives in this file — that is the point of PR1.
type reviewerRole struct {
	TransitionID string
	InputPlace   string
	OutputPlace  string
	ProfileID    string // identity in ProfileRegistry
	ActionID     string // task in ActionRegistry
}

// reviewerRoles enumerates the 6 parallel expert sub-agents. Order is
// stable; fanout + aggregator iterate in this order. The transition
// IDs MUST equal TransitionID(ActionID, ProfileID) — asserted by
// TestTransitionIDConvention.
var reviewerRoles = []reviewerRole{
	{TrReviewGo, PlaceSpecForGo, PlaceReviewGo, ProfileGoEng, ActionReviewSpec},
	{TrReviewAI, PlaceSpecForAI, PlaceReviewAI, ProfileAIEng, ActionReviewSpec},
	{TrReviewDevOps, PlaceSpecForDevOps, PlaceReviewDevOps, ProfileDevOps, ActionReviewSpec},
	{TrReviewQA, PlaceSpecForQA, PlaceReviewQA, ProfileQA, ActionReviewSpec},
	{TrReviewArch, PlaceSpecForArch, PlaceReviewArch, ProfileArch, ActionReviewSpec},
	{TrReviewPM, PlaceSpecForPM, PlaceReviewPM, ProfilePM, ActionReviewSpec},
}

// ReviewerRoles returns the stable ordering of profile IDs used by the
// aggregator and by t-fanout-reviewers. Exposed for tests; copies the
// slice so callers cannot mutate internals. The strings returned are
// ProfileIDs (e.g. "go-eng", "pm") which now double as the Review.Role
// field value emitted by each reviewer sub-agent.
func ReviewerRoles() []string {
	out := make([]string, len(reviewerRoles))
	for i, r := range reviewerRoles {
		out[i] = r.ProfileID
	}
	return out
}
