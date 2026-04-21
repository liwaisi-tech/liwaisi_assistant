package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
)

// newAwakeningComposerAdapter bridges the awakens.ComposerFunc signature
// (payload is untyped to keep the awakens package independent of fanout's
// types) and fanout.Compose which takes a concrete fanout.AwakeningProbePlan.
//
// The LLM emits strict-JSON matching fanout.AwakeningProbePlan, so the
// adapter's tolerance for payload shape only needs to cover (a) the raw
// bytes/string/json.RawMessage landing from fire_llm and (b) the already-
// typed struct path exercised by tests.
func newAwakeningComposerAdapter() awakens.ComposerFunc {
	return func(_ context.Context, sessionID string, payload any, deps awakens.ComposerDeps) (*cpn.CPN, error) {
		plan, err := planFromPayload(payload)
		if err != nil {
			return nil, fmt.Errorf("awakening composer: %w", err)
		}
		return fanout.Compose(sessionID, plan, fanout.Deps{
			HostAdapter: deps.HostAdapter,
			HostGate:    deps.HostGate,
			Clock:       deps.Clock,
		})
	}
}

func planFromPayload(payload any) (fanout.AwakeningProbePlan, error) {
	switch v := payload.(type) {
	case fanout.AwakeningProbePlan:
		return v, nil
	case *fanout.AwakeningProbePlan:
		if v == nil {
			return fanout.AwakeningProbePlan{}, fmt.Errorf("nil *AwakeningProbePlan")
		}
		return *v, nil
	case json.RawMessage:
		return unmarshalPlan(v)
	case []byte:
		return unmarshalPlan(v)
	case string:
		return unmarshalPlan([]byte(v))
	default:
		raw, err := json.Marshal(payload)
		if err != nil {
			return fanout.AwakeningProbePlan{}, fmt.Errorf("unsupported payload %T: %w", payload, err)
		}
		return unmarshalPlan(raw)
	}
}

func unmarshalPlan(raw []byte) (fanout.AwakeningProbePlan, error) {
	var p fanout.AwakeningProbePlan
	if err := json.Unmarshal(raw, &p); err != nil {
		return fanout.AwakeningProbePlan{}, fmt.Errorf("unmarshal plan: %w", err)
	}
	return p, nil
}
