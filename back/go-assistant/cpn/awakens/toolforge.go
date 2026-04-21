package awakens

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// AwakeningNamespace is the namespace every awakening-minted tool lands in.
const AwakeningNamespace = "awakens"

// DefaultAwakeningToolbox is assigned when the LLM omits `toolbox`
// (spec-architecture-brae-awakening-toolbox-extension.md REQ-005: fall back
// to the awakening namespace since most awakening-minted tools are shell
// wrappers tagged under `awakens`).
const DefaultAwakeningToolbox = AwakeningNamespace

// canonicalToolboxes is the admissible toolbox set from REQ-003 plus the
// implicit `awakens` default. Entries outside this set are mapped to
// `general` (GUD-002): misclassification into `general` is softer than into
// a specific domain.
var canonicalToolboxes = map[string]struct{}{
	"system":    {},
	"developer": {},
	"web":       {},
	"image":     {},
	"pdf":       {},
	"data":      {},
	"general":   {},
	"awakens":   {},
}

// canonicaliseToolbox returns the toolbox to persist for an awakening-minted
// tool. Empty → DefaultAwakeningToolbox; unknown → "general".
func canonicaliseToolbox(raw string) string {
	t := strings.TrimSpace(strings.ToLower(raw))
	if t == "" {
		return DefaultAwakeningToolbox
	}
	if _, ok := canonicalToolboxes[t]; ok {
		return t
	}
	return "general"
}

// normaliseHashtags applies taxonomy REQ-NORM-001 (silent drop of invalid
// tokens) and awakening REQ-011 (cap at MaxHashtagsPerAwakeningTool after
// ascending sort). Returns the cleaned set plus the count of entries dropped
// by the cap for `personality.tooltags.truncated` emission.
func normaliseHashtags(raw []string) (kept []string, droppedByCap int) {
	normalised := tools.NormalizeSet(raw)
	if len(normalised) <= MaxHashtagsPerAwakeningTool {
		return normalised, 0
	}
	sorted := make([]string, len(normalised))
	copy(sorted, normalised)
	sort.Strings(sorted)
	kept = sorted[:MaxHashtagsPerAwakeningTool]
	droppedByCap = len(sorted) - MaxHashtagsPerAwakeningTool
	return kept, droppedByCap
}

// RegisterBatch iterates r.ToolsRegister and calls ToolRegistry.RegisterManifest
// once per entry. Idempotent (CON-005): a registry that rejects with a
// duplicate-name error is treated as success. Bounded by MaxToolsToRegister
// (CON-004).
//
// Toolbox + Hashtags flow into the ToolManifest per REQ-005 of the awakening-
// toolbox-extension spec. Events observable via slog:
//
//   - lexicon.tag.drifted — one per tool registered with empty hashtags
//     (BEH-002). Reason: "awakening: tool registered without hashtags".
//   - personality.tooltags.truncated — one per tool whose emitted hashtag
//     count exceeded MaxHashtagsPerAwakeningTool (REQ-011).
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
		tags, droppedByCap := normaliseHashtags(entry.Hashtags)
		qn := AwakeningNamespace + "/" + entry.Name + "@0.1.0"
		if droppedByCap > 0 {
			logger.Info("personality.tooltags.truncated",
				slog.String("event", "personality.tooltags.truncated"),
				slog.String("qualified_name", qn),
				slog.Int("kept", len(tags)),
				slog.Int("dropped", droppedByCap),
			)
		}
		if len(tags) == 0 {
			logger.Info("lexicon.tag.drifted",
				slog.String("event", "lexicon.tag.drifted"),
				slog.String("source", "awakening"),
				slog.String("qualified_name", qn),
				slog.String("reason", "awakening: tool registered without hashtags"),
			)
		}
		manifest := cpn.ToolManifest{
			Namespace: AwakeningNamespace,
			Name:      entry.Name,
			Version:   "0.1.0",
			Origin:    "awakening",
			HelpText:  fmt.Sprintf("auto-registered during awakening, basis=%s", entry.Basis),
			Toolbox:   canonicaliseToolbox(entry.Toolbox),
			Hashtags:  tags,
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
