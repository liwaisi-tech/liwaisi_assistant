package main

// flow_builders.go — registers built-in role→factory mappings used by
// POST /api/v1/flows/{hash}/run. The factory returns a fresh CPN topology
// per session so tokens and HITL channels don't leak across runs.

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/toolbuilder"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// builtinFlowBuilders returns the composition-root registry of built-in
// topology factories, keyed by the flow's `role` (as persisted in the flows
// table). Adding a new built-in CPN means adding one entry here.
func builtinFlowBuilders() map[string]app.TopologyFactory {
	return map[string]app.TopologyFactory{
		toolbuilder.FlowName: func(sessionID string) *cpn.CPN {
			return toolbuilder.BuildToolAtelierTopology(sessionID, toolbuilder.AtelierDeps{})
		},
	}
}

// persistBuiltinFlows upserts a minimal FlowRecord for every library entry
// so GET /api/v1/flows lists them and POST /api/v1/flows/{hash}/run can
// resolve them by hash. TopologyJSON is a small summary document rather than
// a full MarshalCPN dump — built-in topologies contain inline guard/handler
// closures that aren't in the FuncRegistry, which makes a faithful round-trip
// impossible. Execution never re-hydrates from the DB: the run handler looks
// up the role in FlowBuilders and rebuilds the topology from code.
func persistBuiltinFlows(ctx context.Context, repo persist.FlowRepository, lib *cpn.FlowLibrary, logger *slog.Logger) {
	if repo == nil || lib == nil {
		return
	}
	now := time.Now()
	for _, entry := range lib.ListEntries() {
		if entry == nil || entry.CPN == nil || entry.Hash == "" {
			continue
		}
		summary, _ := json.Marshal(map[string]any{
			"role":     entry.CPN.Role,
			"origin":   entry.Origin,
			"template": entry.Signature.Template,
			"hashtags": entry.Signature.Hashtags,
		})
		rec := &persist.FlowRecord{
			Hash:            entry.Hash,
			Role:            entry.CPN.Role,
			TopologyJSON:    summary,
			FunctionMapping: json.RawMessage(`{}`),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := repo.Save(ctx, rec); err != nil {
			logger.Warn("persist builtin flow", slog.String("role", entry.CPN.Role), slog.Any("error", err))
			continue
		}
		logger.Info("persisted builtin flow", slog.String("role", entry.CPN.Role), slog.String("hash", entry.Hash))
	}
}
