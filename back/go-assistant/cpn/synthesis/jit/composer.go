package jit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
)

// ── Errors ─────────────────────────────────────────────────────────────────

var (
	// ErrNoMatches is returned when ToolMatchSet.Matches is empty (BEH-001).
	ErrNoMatches = errors.New("jit: empty match set")
	// ErrUnknownPrimitive is returned when a matched tool cannot be
	// re-resolved at compose time (REQ-004, SEC-002).
	ErrUnknownPrimitive = errors.New("jit: unknown primitive / tool")
	// ErrLintFailed wraps synthesis.Lint rejections.
	ErrLintFailed = errors.New("jit: lint failed")
	// ErrAdversarialFail wraps REQ-LINT-ADV rejections.
	ErrAdversarialFail = errors.New("jit: adversarial lint rejected topology")
)

// JITCompositionCap is the REQ-012 size cap applied before and during lint.
var JITCompositionCap = cpn.SizeCap{MaxPlaces: 32, MaxTransitions: 24}

// ── Public types ───────────────────────────────────────────────────────────

// ResolveFunc is the narrow interface the composer uses to re-resolve each
// matched tool at compose time (SEC-002). Return a non-nil error to signal
// "unknown"; the composer wraps it in ErrUnknownPrimitive. Callers typically
// adapt *cpn/tools.Registry.Get into this shape.
type ResolveFunc func(ctx context.Context, qualifiedName string) error

// ComposeOptions bundles the per-call inputs the composer needs.
type ComposeOptions struct {
	SessionID         string
	TraceID           string
	PersonalityDigest string
	Template          TemplateID
	Cap               cpn.SizeCap
	Resolver          ResolveFunc
	SafeRegistry      cpn.SafeRegistryPort
	Cache             *Cache
	Logger            *slog.Logger
	Now               func() time.Time
}

// ── Compose ────────────────────────────────────────────────────────────────

// Compose runs the full Compose → Emit → Lint → Adversarial → Cache
// pipeline (PAT-001). Returns the draft, the emitted canonical JSON, or
// a sentinel error wrapping the first failure.
func Compose(
	ctx context.Context,
	matchSet cpn.ToolMatchSet,
	intent cpn.Intent,
	opts ComposeOptions,
) (cpn.TopologyDraft, []byte, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	if len(matchSet.Matches) == 0 {
		return cpn.TopologyDraft{}, nil, ErrNoMatches
	}

	start := now()
	template := selectTemplate(opts.Template, intent)
	intentDigest := intent.Digest()

	logger.Info("jit.compose.started",
		"event", "jit.compose.started",
		"session_id", opts.SessionID,
		"trace_id", opts.TraceID,
		"template", string(template),
		"match_count", len(matchSet.Matches),
	)

	cacheKey := composeCacheKey(intentDigest, matchSet.Digest, opts.PersonalityDigest, template)

	// Cache hit path.
	if opts.Cache != nil {
		if blob, draft, ok := opts.Cache.Get(cacheKey); ok {
			digest := sha256Hex(blob)
			logger.Info("jit.cache.hit",
				"event", "jit.cache.hit",
				"digest", digest,
				"session_id", opts.SessionID,
			)
			logger.Info("jit.compose.completed",
				"event", "jit.compose.completed",
				"digest", digest,
				"places", len(draft.Places),
				"transitions", len(draft.Transitions),
				"cache_hit", true,
				"lint_ok", true,
				"latency_ms", now().Sub(start).Milliseconds(),
			)
			return draft, blob, nil
		}
	}

	// Re-resolve every tool (SEC-002 / REQ-004).
	if opts.Resolver != nil {
		for _, m := range matchSet.Matches {
			if err := opts.Resolver(ctx, m.QualifiedName); err != nil {
				return cpn.TopologyDraft{}, nil, fmt.Errorf("%w: %s: %v", ErrUnknownPrimitive, m.QualifiedName, err)
			}
		}
	}

	// Build draft.
	draftID := "jit-" + shortHash(intentDigest+":"+matchSet.Digest+":"+string(template))
	var draft cpn.TopologyDraft
	switch template {
	case TemplateSequentialPipeline:
		draft = buildSequentialPipeline(draftID, matchSet.Matches)
	default:
		draft = buildParallelFanout(draftID, matchSet.Matches)
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]string)
	}
	draft.Metadata["jit"] = "true"
	draft.Metadata["intent_digest"] = intentDigest
	draft.Metadata["match_count"] = strconv.Itoa(len(matchSet.Matches))
	draft.Metadata["template"] = string(template)

	// Cap pre-check (REQ-012 / AC-005). We still run the full Lint below so
	// the spec §10 proof path exercises the existing lint surface.
	capEff := opts.Cap
	if capEff.MaxPlaces == 0 && capEff.MaxTransitions == 0 && capEff.MaxArcs == 0 {
		capEff = JITCompositionCap
	}

	blob, err := emit(draft)
	if err != nil {
		return cpn.TopologyDraft{}, nil, fmt.Errorf("jit: emit: %w", err)
	}

	// Gate through synthesis.Lint — the same hook authored flows see.
	lintResult := lintBlob(blob, opts.SafeRegistry, capEff)
	if !lintResult.Passed() {
		return cpn.TopologyDraft{}, nil, fmt.Errorf("%w: %v", ErrLintFailed, lintResult.Err())
	}

	// Adversarial rules (REQ-LINT-ADV).
	adv := adversarialLint(draft)
	if !adv.Passed {
		return cpn.TopologyDraft{}, nil, fmt.Errorf("%w: %s: %s", ErrAdversarialFail, adv.Rule, adv.Reason)
	}

	// Cache store.
	if opts.Cache != nil {
		opts.Cache.Put(cacheKey, blob, draft, opts.PersonalityDigest)
	}

	digest := sha256Hex(blob)
	logger.Info("jit.compose.completed",
		"event", "jit.compose.completed",
		"digest", digest,
		"places", len(draft.Places),
		"transitions", len(draft.Transitions),
		"cache_hit", false,
		"lint_ok", true,
		"latency_ms", now().Sub(start).Milliseconds(),
	)

	return draft, blob, nil
}

// ── Emit ───────────────────────────────────────────────────────────────────

// emit marshals a TopologyDraft into the persist.CPNTopology JSON shape.
// Kept local so cpn/synthesis/jit does not import cpn/persist (Axiom A13);
// the struct tags mirror persist.CPNTopology field-by-field.
type emitTopology struct {
	ID          string                    `json:"id"`
	Role        string                    `json:"role"`
	Depth       int                       `json:"depth"`
	Mode        string                    `json:"mode,omitempty"`
	Metadata    map[string]string         `json:"metadata,omitempty"`
	Places      map[string]emitPlace      `json:"places"`
	Transitions map[string]emitTransition `json:"transitions"`
}

type emitPlace struct {
	ID    string `json:"id"`
	Color string `json:"color"`
	Space string `json:"space"`
}

type emitTransition struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	InputPlaces  []string `json:"inputPlaces"`
	OutputPlaces []string `json:"outputPlaces"`
	ToolName     string   `json:"toolName,omitempty"`
	ExecutorFunc string   `json:"executorFunc,omitempty"`
}

func emit(d cpn.TopologyDraft) ([]byte, error) {
	t := emitTopology{
		ID:          d.ID,
		Role:        d.Role,
		Metadata:    d.Metadata,
		Places:      make(map[string]emitPlace, len(d.Places)),
		Transitions: make(map[string]emitTransition, len(d.Transitions)),
	}
	for _, p := range d.Places {
		t.Places[p.ID] = emitPlace{ID: p.ID, Color: p.Color, Space: p.Space}
	}
	for _, tr := range d.Transitions {
		t.Transitions[tr.ID] = emitTransition{
			ID:           tr.ID,
			Kind:         tr.Kind,
			InputPlaces:  tr.Inputs,
			OutputPlaces: tr.Outputs,
			ToolName:     tr.ToolName,
			ExecutorFunc: tr.ExecutorFunc,
		}
	}

	// Deterministic: marshal via encoder with sorted keys using json.Marshal
	// — Go's json.Marshal already sorts map keys by UTF-8 order, so our
	// map[string]X fields render deterministically. We additionally strip
	// the default trailing newline for byte-equality across platforms.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return out, nil
}

// ── Lint plumbing ──────────────────────────────────────────────────────────

// lintBlob runs synthesis.LintRaw (which decodes into *persist.CPNTopology
// inside the synthesis package) so this file avoids importing persist
// directly (Axiom A13 / CON-001).
func lintBlob(blob []byte, safe cpn.SafeRegistryPort, sizeCap cpn.SizeCap) cpn.LintResultPort {
	return synthesis.LintRaw(json.RawMessage(blob), safe, sizeCap)
}

// ── Helpers ────────────────────────────────────────────────────────────────

func composeCacheKey(intentDigest, matchDigest, persDigest string, template TemplateID) string {
	h := sha256.New()
	h.Write([]byte(intentDigest))
	h.Write([]byte{':'})
	h.Write([]byte(matchDigest))
	h.Write([]byte{':'})
	h.Write([]byte(persDigest))
	h.Write([]byte{':'})
	h.Write([]byte(template))
	return hex.EncodeToString(h.Sum(nil))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
