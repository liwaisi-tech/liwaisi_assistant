package awakens

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// AwakeningNamespace is the namespace every awakening-minted tool lands in.
const AwakeningNamespace = "awakens"

// RegisterBatch iterates r.ToolsRegister and calls ToolRegistry.RegisterManifest
// once per entry. Idempotent (CON-005): a registry that rejects with a
// duplicate-name error is treated as success. Bounded by MaxToolsToRegister
// (CON-004).
//
// Returns the list of successfully-registered qualified names in registration
// order plus the first non-duplicate error encountered (if any). A non-nil
// error does NOT unwind prior registrations — the registry is append-only.
func RegisterBatch(ctx context.Context, reg cpn.ToolRegistry, r AwakeningReport, logger *slog.Logger) ([]string, error) {
	if reg == nil {
		return nil, errors.New("awakens: nil ToolRegistry")
	}
	if logger == nil {
		logger = slog.Default()
	}

	entries := r.ToolsRegister
	if len(entries) > MaxToolsToRegister {
		entries = entries[:MaxToolsToRegister]
	}

	registered := make([]string, 0, len(entries))
	for _, entry := range entries {
		manifest := cpn.ToolManifest{
			Namespace: AwakeningNamespace,
			Name:      entry.Name,
			Version:   "0.1.0",
			Origin:    "awakening",
			HelpText:  fmt.Sprintf("auto-registered during awakening, basis=%s", entry.Basis),
			Provenance: cpn.ProvenanceSnapshot{
				PromptDigest: PromptDigestMarker,
			},
		}
		result, err := reg.RegisterManifest(ctx, manifest)
		if err != nil {
			if isDuplicateErr(err) {
				logger.Debug("awakens: duplicate tool registration (idempotent no-op)",
					slog.String("tool", entry.Name),
					slog.Any("error", err),
				)
				continue
			}
			return registered, fmt.Errorf("awakens: register %s: %w", entry.Name, err)
		}
		registered = append(registered, result.QualifiedName)
	}
	return registered, nil
}

// isDuplicateErr matches the sentinel shapes that the tool registry returns
// when a tool name is already present. We stay conservative: any error whose
// message includes "exists" or "duplicate" (case-insensitive) is treated as
// a no-op per CON-005.
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "exists") ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "already")
}
