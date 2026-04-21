package fanout

import (
	"fmt"
	"sort"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Compose builds the parallel probe-fanout sub-CPN described in spec §4.3.
//
// The returned *cpn.CPN has:
//   - Two seed places (PlaceTriggerID, PlacePlanID) pre-populated with the
//     trigger sentinel and the validated plan.
//   - One NodeKindTool transition per probe, each reading the shared
//     trigger and writing its own PlaceProbeResultPrefix place.
//   - A single TransitionReduceID transition performing the AND-join and
//     depositing the assembled AwakeningReport on PlaceReportID +
//     PlaceEgressID.
//
// Determinism (NFR-002): probe IDs are slugified then sorted before the
// transition/place list is constructed, so map-iteration order cannot leak
// into IDs or arc ordering.
//
// Self-check (CON-004): before returning, Compose walks its own Transitions
// map and errors out if any transition uses NodeKindInstantiate or
// NodeKindSubNet — the fanout MUST be flat.
func Compose(sessionID string, plan AwakeningProbePlan, deps Deps) (*cpn.CPN, error) {
	if err := ValidatePlan(plan); err != nil {
		return nil, err
	}
	plan = withMandatoryInfoProbes(plan)
	// Re-validate the augmented plan so cap / field invariants still hold.
	if err := ValidatePlan(plan); err != nil {
		return nil, err
	}
	timeout := clampTimeout(plan.TimeoutPerProbeMs)

	// Normalise probe IDs up-front. Duplicate IDs are disambiguated by
	// suffixing their 1-based index; this is deterministic and keeps
	// transition IDs unique without reordering probe inputs.
	normalised := make([]AwakeningProbeEntry, len(plan.Probes))
	used := make(map[string]int, len(plan.Probes))
	for i, p := range plan.Probes {
		id := p.ID
		if id == "" {
			id = slugify(p.Command)
		} else {
			id = slugify(id)
		}
		if _, seen := used[id]; seen {
			// Suffix with the 1-based input index so the IDs stay
			// deterministic across calls with identical plans.
			id = fmt.Sprintf("%s-%d", id, i+1)
		}
		used[id]++
		entry := p
		entry.ID = id
		normalised[i] = entry
	}

	// Sort by ID for deterministic transition / arc ordering.
	sorted := make([]AwakeningProbeEntry, len(normalised))
	copy(sorted, normalised)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	places := map[string]*cpn.Place{
		PlaceTriggerID: cpn.NewPlace(PlaceTriggerID, cpn.ColorString, cpn.SpaceComputation),
		PlacePlanID:    cpn.NewPlace(PlacePlanID, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceReportID:  cpn.NewPlace(PlaceReportID, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceEgressID:  cpn.NewPlace(PlaceEgressID, cpn.ColorArtifact, cpn.SpaceComputation),
	}

	transitions := make(map[string]*cpn.Transition, len(sorted)+1)

	// Per-probe output places + NodeKindTool transitions, consumed in
	// sorted order.
	reducerInputs := make([]string, 0, len(sorted))
	for _, entry := range sorted {
		resultPlaceID := PlaceProbeResultPrefix + entry.ID
		places[resultPlaceID] = cpn.NewPlace(resultPlaceID, cpn.ColorArtifact, cpn.SpaceComputation)

		transitionID := TransitionProbePrefix + entry.ID
		t := cpn.NewTransition(
			transitionID,
			cpn.NodeKindTool,
			[]string{PlaceTriggerID},
			[]string{resultPlaceID},
		)
		t.ToolName = transitionID
		t.Executor = makeProbeExecutor(entry, deps, timeout)
		transitions[transitionID] = t
		reducerInputs = append(reducerInputs, resultPlaceID)
	}

	// Reducer: N input places (AND-join), 2 output places (report +
	// egress mirror per PAT-002).
	reducer := cpn.NewTransition(
		TransitionReduceID,
		cpn.NodeKindTool,
		reducerInputs,
		[]string{PlaceReportID, PlaceEgressID},
	)
	reducer.ToolHandler = makeReducer(deps)
	transitions[TransitionReduceID] = reducer

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-probe-fanout", sessionID),
		"brae-awakens-probe-fanout",
		1,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)

	// Seed the trigger + plan places so the fanout branches are enabled
	// the moment Run() starts. CON-005: each Compose returns a fresh
	// instance; seeding cannot leak across sessions.
	c.SeedFunc = func(cc *cpn.CPN) { seedFanout(cc, plan) }
	seedFanout(c, plan)

	// CON-004 self-check: assert flat topology before returning.
	if err := assertFlatTopology(c); err != nil {
		return nil, err
	}
	return c, nil
}

// seedFanout populates the trigger and plan places. Broken out so both the
// initial compose-time seeding and SeedFunc (used by CPN.Reset) can reuse
// the same body.
//
// The trigger place is seeded with ONE token per probe. Every probe
// transition consumes from the shared trigger; with only one token the
// second probe onwards could never enable and the sub-CPN would deadlock
// with "no enabled transitions and no terminal marking".
func seedFanout(c *cpn.CPN, plan AwakeningProbePlan) {
	if trigger, ok := c.Places[PlaceTriggerID]; ok {
		for range plan.Probes {
			_ = trigger.Deposit(&cpn.Token{
				Color:   cpn.ColorString,
				Space:   cpn.SpaceComputation,
				Payload: "go",
			})
		}
	}
	if planPlace, ok := c.Places[PlacePlanID]; ok {
		_ = planPlace.Deposit(&cpn.Token{
			Color:   cpn.ColorArtifact,
			Space:   cpn.SpaceComputation,
			Payload: plan,
		})
	}
}

// mandatoryInfoProbes is the fixed set of metadata probes the composer
// always adds when the LLM plan omits them. They populate AwakeningReport.OS,
// .Shell, .Identity so downstream consumers (A2UI card, environment-awareness
// block injected into later turns) never have to fall back to defaults.
//
// All commands are introspection-class (auto-approved by the Host-gate);
// each one uses a POSIX idiom that is safe on busybox as well as coreutils.
func mandatoryInfoProbes() []AwakeningProbeEntry {
	return []AwakeningProbeEntry{
		{ID: "info-os-name", Kind: ProbeKindInfo, Target: InfoTargetOSName, Command: "cat /etc/os-release"},
		{ID: "info-os-kernel", Kind: ProbeKindInfo, Target: InfoTargetOSKernel, Command: "uname -r"},
		{ID: "info-os-arch", Kind: ProbeKindInfo, Target: InfoTargetOSArch, Command: "uname -m"},
		{ID: "info-shell-path", Kind: ProbeKindInfo, Target: InfoTargetShellPath, Command: "printenv SHELL"},
		{ID: "info-user", Kind: ProbeKindInfo, Target: InfoTargetUser, Command: "whoami"},
		{ID: "info-id", Kind: ProbeKindInfo, Target: InfoTargetIdentity, Command: "id"},
		{ID: "info-home", Kind: ProbeKindInfo, Target: InfoTargetHome, Command: "printenv HOME"},
		{ID: "info-hostname", Kind: ProbeKindInfo, Target: InfoTargetHostname, Command: "hostname"},
	}
}

// withMandatoryInfoProbes appends every entry from mandatoryInfoProbes that is
// not already covered (by ID or by Target) in plan.Probes. Fresh info probes
// are deterministically ordered (the fixed list), which preserves the
// NFR-002 deterministic-output guarantee.
//
// The plan cap (MaxProbes) is respected: any mandatory probe that would push
// the total over MaxProbes is dropped, since structural validity must hold
// for the call to succeed.
func withMandatoryInfoProbes(plan AwakeningProbePlan) AwakeningProbePlan {
	seenID := make(map[string]struct{}, len(plan.Probes))
	seenTarget := make(map[string]struct{}, len(plan.Probes))
	for _, p := range plan.Probes {
		if p.ID != "" {
			seenID[p.ID] = struct{}{}
		}
		if p.Target != "" {
			seenTarget[p.Target] = struct{}{}
		}
	}
	out := plan
	out.Probes = make([]AwakeningProbeEntry, 0, len(plan.Probes)+len(mandatoryInfoProbes()))
	out.Probes = append(out.Probes, plan.Probes...)
	for _, p := range mandatoryInfoProbes() {
		if _, ok := seenID[p.ID]; ok {
			continue
		}
		if _, ok := seenTarget[p.Target]; ok {
			continue
		}
		if len(out.Probes) >= MaxProbes {
			break
		}
		out.Probes = append(out.Probes, p)
	}
	return out
}

// clampTimeout applies REQ-004: min(2s, plan.TimeoutPerProbeMs/1000), with a
// 100ms floor so a zero or negative request falls back to a safe default.
func clampTimeout(ms int) time.Duration {
	if ms <= 0 {
		return DefaultPerProbeTimeout
	}
	requested := time.Duration(ms) * time.Millisecond
	if requested > DefaultPerProbeTimeout {
		return DefaultPerProbeTimeout
	}
	if requested < MinPerProbeTimeout {
		return MinPerProbeTimeout
	}
	return requested
}

// assertFlatTopology walks the composed CPN and errors if it finds any
// NodeKindInstantiate or NodeKindSubNet transition (CON-004). Exposed as a
// package-private helper so tests can drive it via a test-only hook without
// copy-pasting the logic.
func assertFlatTopology(c *cpn.CPN) error {
	for id, t := range c.Transitions {
		if t.Kind == cpn.NodeKindInstantiate || t.Kind == cpn.NodeKindSubNet {
			return fmt.Errorf("fanout: CON-004 violated: transition %q has kind %q",
				id, t.Kind)
		}
	}
	return nil
}
