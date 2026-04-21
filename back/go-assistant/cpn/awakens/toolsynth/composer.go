package toolsynth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

// ErrNoSchemas is returned by Compose when schemas is empty.
var ErrNoSchemas = errors.New("toolsynth: no schemas to synthesise")

// Deps bundles the collaborators Compose needs.
type Deps struct {
	Store PendingToolStore
	Clock func() time.Time

	OnSynthesisStarted   func(ctx context.Context, binary string)
	OnSynthesisCompleted func(ctx context.Context, binary, provenanceSHA256 string)
	OnSynthesisFailed    func(ctx context.Context, binary, reason string)
}

// Compose builds the per-schema synthesis sub-CPN. One branch per schema:
//
//	t-synth-materialise-<bin> → p-synth-pending-<bin>
//	t-synth-stage-<bin>       → p-synth-staged-<bin>
//
// Per-branch failure is non-fatal (REQ-1205): the materialise transition
// emits an error sentinel token that the stage transition filters, invokes
// OnSynthesisFailed, and does NOT touch the store. Sibling branches run to
// completion.
func Compose(sessionID string, schemas []helpparse.HelpSchema, deps Deps) (*cpn.CPN, error) {
	if len(schemas) == 0 {
		return nil, ErrNoSchemas
	}
	if deps.Store == nil {
		return nil, errors.New("toolsynth: Deps.Store is required")
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	seen := make(map[string]struct{}, len(schemas))
	norm := make([]helpparse.HelpSchema, 0, len(schemas))
	for _, s := range schemas {
		bin := strings.TrimSpace(s.Binary)
		if bin == "" {
			norm = append(norm, s)
			continue
		}
		if _, ok := seen[bin]; ok {
			continue
		}
		seen[bin] = struct{}{}
		norm = append(norm, s)
	}
	sort.SliceStable(norm, func(i, j int) bool { return norm[i].Binary < norm[j].Binary })

	triggerID := "p-synth-trigger"
	places := map[string]*cpn.Place{
		triggerID: cpn.NewPlace(triggerID, cpn.ColorString, cpn.SpaceComputation),
	}
	transitions := make(map[string]*cpn.Transition, len(norm)*2)

	for i, s := range norm {
		branch := branchKey(s.Binary, i)
		pendingPlace := PlacePendingPrefix + branch
		stagedPlace := PlaceStagedPrefix + branch

		places[pendingPlace] = cpn.NewPlace(pendingPlace, cpn.ColorArtifact, cpn.SpaceComputation)
		places[stagedPlace] = cpn.NewPlace(stagedPlace, cpn.ColorArtifact, cpn.SpaceComputation)

		matID := TxMaterialisePrefix + branch
		tMat := cpn.NewTransition(matID, cpn.NodeKindTool,
			[]string{triggerID}, []string{pendingPlace})
		tMat.ToolName = matID
		tMat.Executor = makeMaterialiseExecutor(s, deps, clock)
		transitions[matID] = tMat

		stageID := TxStagePrefix + branch
		tStage := cpn.NewTransition(stageID, cpn.NodeKindTool,
			[]string{pendingPlace}, []string{stagedPlace})
		tStage.ToolName = stageID
		tStage.ToolHandler = makeStageHandler(s.Binary, sessionID, stagedPlace, deps, clock)
		transitions[stageID] = tStage
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-toolsynth", sessionID),
		"brae-awakens-toolsynth",
		1,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.SeedFunc = func(cc *cpn.CPN) { seedTrigger(cc, triggerID, len(norm)) }
	seedTrigger(c, triggerID, len(norm))

	for id, t := range c.Transitions {
		if t.Kind == cpn.NodeKindInstantiate || t.Kind == cpn.NodeKindSubNet {
			return nil, fmt.Errorf("toolsynth: sub-CPN must be flat: transition %q kind=%q", id, t.Kind)
		}
	}
	return c, nil
}

// branchKey produces a stable-per-index branch identifier that survives
// duplicate or empty binary names within the input set (composer already
// deduplicates non-empty names; empty names fall here and get an index
// suffix to keep place IDs unique).
func branchKey(binary string, idx int) string {
	bin := strings.TrimSpace(binary)
	if bin == "" {
		return fmt.Sprintf("unknown-%d", idx)
	}
	return bin
}

func seedTrigger(c *cpn.CPN, triggerID string, branches int) {
	trigger, ok := c.Places[triggerID]
	if !ok {
		return
	}
	for i := 0; i < branches; i++ {
		_ = trigger.Deposit(&cpn.Token{
			Color:   cpn.ColorString,
			Space:   cpn.SpaceComputation,
			Payload: "go",
		})
	}
}

// synthErrToken signals a failed branch to the stage handler without
// aborting the CPN.
type synthErrToken struct {
	Binary string
	Reason string
}

func makeMaterialiseExecutor(schema helpparse.HelpSchema, deps Deps, clock func() time.Time) func(context.Context, cpn.Token) (cpn.Token, error) {
	return func(ctx context.Context, _ cpn.Token) (cpn.Token, error) {
		bin := strings.TrimSpace(schema.Binary)
		if deps.OnSynthesisStarted != nil {
			deps.OnSynthesisStarted(ctx, bin)
		}
		pending, err := SynthesiseManifest(schema)
		if err != nil {
			return cpn.Token{
				Color:   cpn.ColorArtifact,
				Space:   cpn.SpaceComputation,
				Payload: synthErrToken{Binary: bin, Reason: err.Error()},
			}, nil
		}
		pending.CreatedAt = clock().UTC()
		return cpn.Token{
			Color:   cpn.ColorArtifact,
			Space:   cpn.SpaceComputation,
			Payload: pending,
		}, nil
	}
}

func makeStageHandler(binary, sessionID, outPlace string, deps Deps, clock func() time.Time) func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		for _, tok := range consumed {
			switch p := tok.Payload.(type) {
			case PendingTool:
				p.SessionID = sessionID
				if p.CreatedAt.IsZero() {
					p.CreatedAt = clock().UTC()
				}
				if err := deps.Store.Stage(ctx, p); err != nil {
					if deps.OnSynthesisFailed != nil {
						deps.OnSynthesisFailed(ctx, binary, "store: "+err.Error())
					}
					return map[string]cpn.Token{outPlace: {
						Color:   cpn.ColorArtifact,
						Space:   cpn.SpaceComputation,
						Payload: SynthesisResult{Binary: binary, Err: err.Error()},
					}}, nil
				}
				if deps.OnSynthesisCompleted != nil {
					deps.OnSynthesisCompleted(ctx, binary, p.ProvenanceSHA256)
				}
				return map[string]cpn.Token{outPlace: {
					Color:   cpn.ColorArtifact,
					Space:   cpn.SpaceComputation,
					Payload: SynthesisResult{Binary: binary, Pending: p},
				}}, nil
			case synthErrToken:
				if deps.OnSynthesisFailed != nil {
					deps.OnSynthesisFailed(ctx, p.Binary, p.Reason)
				}
				return map[string]cpn.Token{outPlace: {
					Color:   cpn.ColorArtifact,
					Space:   cpn.SpaceComputation,
					Payload: SynthesisResult{Binary: p.Binary, Err: p.Reason},
				}}, nil
			default:
				return nil, fmt.Errorf("toolsynth-stage[%s]: token payload %T", binary, tok.Payload)
			}
		}
		return map[string]cpn.Token{}, nil
	}
}

// ReduceSyntheses filters failed branches and returns the staged PendingTool
// set. Input order is preserved for successes; failures are dropped.
func ReduceSyntheses(results []SynthesisResult) []PendingTool {
	out := make([]PendingTool, 0, len(results))
	for _, r := range results {
		if r.Err != "" {
			continue
		}
		out = append(out, r.Pending)
	}
	return out
}

// StagedPlaceIDs returns the staged place IDs in branch order for the given
// schemas. Mirrors helpparse.ResultPlaceIDs so the wiring chunk can harvest
// synthesised results without knowing internal naming.
func StagedPlaceIDs(schemas []helpparse.HelpSchema) []string {
	seen := make(map[string]struct{}, len(schemas))
	norm := make([]helpparse.HelpSchema, 0, len(schemas))
	for _, s := range schemas {
		bin := strings.TrimSpace(s.Binary)
		if bin == "" {
			norm = append(norm, s)
			continue
		}
		if _, ok := seen[bin]; ok {
			continue
		}
		seen[bin] = struct{}{}
		norm = append(norm, s)
	}
	sort.SliceStable(norm, func(i, j int) bool { return norm[i].Binary < norm[j].Binary })
	out := make([]string, len(norm))
	for i, s := range norm {
		out[i] = PlaceStagedPrefix + branchKey(s.Binary, i)
	}
	return out
}
