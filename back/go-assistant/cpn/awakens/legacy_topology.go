package awakens

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// SC-16: quarantine escape hatch for two release cycles. BRAE_AWAKENING_MODE
// selects between the default probe-fanout topology and a minimal single-turn
// legacy topology that skips the probe layer entirely.

type AwakeningMode string

const (
	ModeFanout AwakeningMode = "fanout"
	ModeLegacy AwakeningMode = "legacy"
)

const AwakeningModeEnv = "BRAE_AWAKENING_MODE"

// ReadAwakeningMode reads BRAE_AWAKENING_MODE, defaulting to ModeFanout.
// Unknown values log a warning on logger (or slog.Default when nil) and fall
// back to ModeFanout.
func ReadAwakeningMode(logger *slog.Logger) AwakeningMode {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(AwakeningModeEnv)))
	switch AwakeningMode(raw) {
	case "", ModeFanout:
		return ModeFanout
	case ModeLegacy:
		return ModeLegacy
	}
	lg := logger
	if lg == nil {
		lg = slog.Default()
	}
	lg.Warn("awakening: unknown BRAE_AWAKENING_MODE value, defaulting to fanout",
		slog.String("value", raw))
	return ModeFanout
}

const (
	TransitionLegacyLLM         = "t-legacy-llm"
	TransitionLegacyPersist     = "t-legacy-persist"
	TransitionLegacyEmitMessage = "t-legacy-emit-message"
)

// LegacyTopologyFactory builds the minimal single-turn awakening CPN:
//
//	t-legacy-llm (NodeKindLLM)           : trigger + prompt -> report-json
//	t-legacy-persist (NodeKindTool)      : report -> persisted snapshot + tool registration
//	t-legacy-emit-message (NodeKindTool) : snapshot -> A2UI envelope
//
// No probe fanout, no composer, no sandbox.
func LegacyTopologyFactory(sessionID string, deps Deps) *cpn.CPN {
	places := map[string]*cpn.Place{
		PlaceAwakenTrigger:                 cpn.NewPlace(PlaceAwakenTrigger, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakenSystemPrompt:            cpn.NewPlace(PlaceAwakenSystemPrompt, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakeningReport:               cpn.NewPlace(PlaceAwakeningReport, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceHostCapabilitiesWK:            cpn.NewPlace(PlaceHostCapabilitiesWK, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningSnapshot:             cpn.NewPlace(PlaceAwakeningSnapshot, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningMessage + "-emitted": cpn.NewPlace(PlaceAwakeningMessage+"-emitted", cpn.ColorEvent, cpn.SpaceComputation),
	}

	prompt := SystemPrompt
	if deps.Lexicon != nil {
		prompt = BuildSystemPromptPlan(deps.Lexicon)
	}

	transitions := map[string]*cpn.Transition{
		TransitionLegacyLLM:         newLegacyLLMTransition(prompt),
		TransitionLegacyPersist:     newLegacyPersistTransition(deps),
		TransitionLegacyEmitMessage: newLegacyEmitMessageTransition(deps.Emitter),
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-awakens-legacy", sessionID),
		FlowName,
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 8

	seed := makeAwakenSeeder(prompt)
	c.SeedFunc = seed
	seed(c)
	return c
}

func newLegacyLLMTransition(systemPrompt string) *cpn.Transition {
	if systemPrompt == "" {
		systemPrompt = SystemPrompt
	}
	t := cpn.NewTransition(
		TransitionLegacyLLM,
		cpn.NodeKindLLM,
		[]string{PlaceAwakenTrigger, PlaceAwakenSystemPrompt},
		[]string{PlaceAwakeningReport},
	)
	t.SystemPrompt = systemPrompt
	t.LLMConfig = &cpn.LLMConfig{
		Role:        "awakening-legacy",
		MaxTokens:   2048,
		Temperature: 0.2,
		RequireJSON: true,
	}
	return t
}

func newLegacyPersistTransition(deps Deps) *cpn.Transition {
	t := cpn.NewTransition(
		TransitionLegacyPersist,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningReport},
		[]string{PlaceHostCapabilitiesWK, PlaceAwakeningSnapshot},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionLegacyPersist)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		hostID := deps.HostID
		if hostID == "" {
			hostID = "awakening-" + uuid.NewString()
		}
		source := deps.Source
		if source == "" {
			source = SourceAwakening
		}
		clock := deps.Clock
		if clock == nil {
			clock = SystemClock
		}
		snap := report.Project(hostID, source, clock())
		if snap.ID == "" {
			snap.ID = uuid.NewString()
		}
		if deps.Repository != nil {
			if err := deps.Repository.Save(ctx, snap); err != nil {
				return nil, fmt.Errorf("%s: save snapshot: %w", TransitionLegacyPersist, err)
			}
		}
		if registry := toolRegistryFromContext(ctx); registry != nil {
			registered, regErr := RegisterBatch(ctx, registry, report, nil)
			if regErr != nil {
				return nil, regErr
			}
			if deps.Emitter != nil {
				duplicates := len(report.ToolsRegister) - len(registered)
				if duplicates < 0 {
					duplicates = 0
				}
				deps.Emitter.Registered(ctx, len(registered), duplicates)
			}
		} else if deps.Emitter != nil {
			deps.Emitter.Registered(ctx, 0, 0)
		}
		return map[string]cpn.Token{
			PlaceHostCapabilitiesWK: {
				Color:   cpn.ColorHostFact,
				Space:   cpn.SpaceComputation,
				Payload: snap,
			},
			PlaceAwakeningSnapshot: {
				Color:   cpn.ColorHostFact,
				Space:   cpn.SpaceComputation,
				Payload: snap,
			},
		}, nil
	}
	return t
}

func newLegacyEmitMessageTransition(emitter *Emitter) *cpn.Transition {
	t := cpn.NewTransition(
		TransitionLegacyEmitMessage,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningSnapshot},
		[]string{PlaceAwakeningMessage + "-emitted"},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionLegacyEmitMessage)
		}
		snap, ok := consumed[0].Payload.(persist.HostCapabilitySnapshot)
		if !ok {
			return nil, fmt.Errorf("%s: expected HostCapabilitySnapshot payload, got %T", TransitionLegacyEmitMessage, consumed[0].Payload)
		}
		report := reportFromSnapshot(snap)
		envelope := BuildFirstTurnMessage(report)
		if emitter != nil {
			emitter.Emitted(ctx, len(envelope.Components))
		}
		return map[string]cpn.Token{
			PlaceAwakeningMessage + "-emitted": {
				Color:   cpn.ColorEvent,
				Space:   cpn.SpaceComputation,
				Payload: envelope,
			},
		}, nil
	}
	return t
}

// reportFromSnapshot reconstructs a minimal AwakeningReport from a persisted
// snapshot — used by the legacy emit-message transition, which consumes the
// snapshot deposited by t-legacy-persist and re-renders the first-turn card.
func reportFromSnapshot(snap persist.HostCapabilitySnapshot) AwakeningReport {
	report := AwakeningReport{
		OS: AwakeningOS{
			Name:    snap.Kernel.OS,
			Version: snap.Kernel.OSRelease["VERSION_ID"],
			Kernel:  snap.Kernel.Kernel,
			Arch:    snap.Kernel.Arch,
		},
		Shell: AwakeningShell{Path: snap.Identity.Shell},
		Identity: AwakeningIdentity{
			User: snap.Identity.User,
			UID:  snap.Identity.UID,
			GID:  snap.Identity.GID,
			Home: snap.Identity.Home,
		},
	}
	for _, b := range snap.Binaries {
		if b.Present {
			report.PresentTools = append(report.PresentTools, AwakeningTool{
				Name: b.Name, Version: b.Version,
			})
		} else {
			report.AbsentTools = append(report.AbsentTools, b.Name)
		}
	}
	return report
}
