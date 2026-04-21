package awakens

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SC-08 Observability Spine.
//
// Implements REQ-801..REQ-804 from spec-architecture-brae-awakening.md §4.6:
// a dual-emission (slog + OTEL span attributes) surface covering the
// awakening event taxonomy. The emitter intentionally attaches span
// attributes to the active span pulled from context rather than starting a
// new span per event — the topology's spans are owned by the CPN engine and
// higher-level session plumbing; observability here must not distort them.
//
// Event name convention (REQ-802): slog messages and span-attribute keys
// share the same `brae.awakening.<event>` prefix so log-to-trace correlation
// is a literal string match.

// SLO thresholds per REQ-803. Exported so tests and external code can assert
// the contract without redeclaring the numbers.
const (
	AwakeningSLOp50 = 8 * time.Second
	AwakeningSLOp95 = 15 * time.Second
	AwakeningSLOp99 = 30 * time.Second
)

// spanPrefix is the OTEL span-attribute namespace for awakening events.
const spanPrefix = "brae.awakening."

// Emitter is the dual-emission sink for awakening events. A nil *Emitter is
// safe to call — every method no-ops — so callers can freely embed the
// emitter in Deps without nil-checks at every site.
type Emitter struct {
	logger *slog.Logger
}

// NewEmitter wires an emitter around logger. A nil logger falls back to
// slog.Default() so call sites never have to branch on configuration.
func NewEmitter(logger *slog.Logger) *Emitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Emitter{logger: logger}
}

// emit dual-writes one event: slog.Info at `brae.awakening.<event>` and an
// OTEL attribute namespaced the same way on the active span (if any). attrs
// are converted once and fed to both sinks so the payload shape is identical
// across the two tracks.
func (e *Emitter) emit(ctx context.Context, event string, attrs []slog.Attr) {
	if e == nil {
		return
	}
	name := spanPrefix + event
	logger := e.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, name, attrs...)

	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}
	kvs := make([]attribute.KeyValue, 0, len(attrs)+1)
	kvs = append(kvs, attribute.String("event", name))
	for _, a := range attrs {
		kvs = append(kvs, slogAttrToOTEL(name, a))
	}
	span.AddEvent(name, trace.WithAttributes(kvs...))
	span.SetAttributes(kvs...)
}

// slogAttrToOTEL converts one slog.Attr to an OTEL attribute.KeyValue under
// the span's event namespace. Unknown kinds are serialised via their String()
// representation to keep the conversion total.
func slogAttrToOTEL(prefix string, a slog.Attr) attribute.KeyValue {
	key := prefix + "." + a.Key
	v := a.Value
	switch v.Kind() {
	case slog.KindString:
		return attribute.String(key, v.String())
	case slog.KindInt64:
		return attribute.Int64(key, v.Int64())
	case slog.KindUint64:
		return attribute.Int64(key, int64(v.Uint64()))
	case slog.KindFloat64:
		return attribute.Float64(key, v.Float64())
	case slog.KindBool:
		return attribute.Bool(key, v.Bool())
	case slog.KindDuration:
		return attribute.Int64(key, v.Duration().Milliseconds())
	case slog.KindTime:
		return attribute.String(key, v.Time().Format(time.RFC3339Nano))
	default:
		if ss, ok := v.Any().([]string); ok {
			return attribute.StringSlice(key, ss)
		}
		return attribute.String(key, v.String())
	}
}

// ── Event taxonomy (spec §4.6) ─────────────────────────────────────────────

// Started → awakening.started {session_id, model}.
func (e *Emitter) Started(ctx context.Context, sessionID, model string) {
	e.emit(ctx, "started", []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("model", model),
	})
}

// LLMBootstrapCompleted → awakening.llm.bootstrap.completed.
func (e *Emitter) LLMBootstrapCompleted(ctx context.Context, duration time.Duration, tokensIn, tokensOut int) {
	e.emit(ctx, "llm.bootstrap.completed", []slog.Attr{
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("tokens_in", tokensIn),
		slog.Int("tokens_out", tokensOut),
	})
}

// ProbeComposed → awakening.probe.composed.
func (e *Emitter) ProbeComposed(ctx context.Context, slugs []string) {
	e.emit(ctx, "probe.composed", []slog.Attr{
		slog.Int("probe_count", len(slugs)),
		slog.Any("slugs", slugs),
	})
}

// ProbeFired → awakening.probe.fired.
func (e *Emitter) ProbeFired(ctx context.Context, slug string, duration time.Duration, exitCode int) {
	e.emit(ctx, "probe.fired", []slog.Attr{
		slog.String("slug", slug),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("exit_code", exitCode),
	})
}

// ProbeReduced → awakening.probe.reduced.
func (e *Emitter) ProbeReduced(ctx context.Context, success, fail int) {
	e.emit(ctx, "probe.reduced", []slog.Attr{
		slog.Int("success_count", success),
		slog.Int("fail_count", fail),
	})
}

// ReportProjected → awakening.report.projected.
func (e *Emitter) ReportProjected(ctx context.Context, toolCount int, buckets []string) {
	e.emit(ctx, "report.projected", []slog.Attr{
		slog.Int("tool_count", toolCount),
		slog.Any("toolbox_buckets", buckets),
	})
}

// Registered → awakening.registered.
func (e *Emitter) Registered(ctx context.Context, toolCount, duplicateCount int) {
	e.emit(ctx, "registered", []slog.Attr{
		slog.Int("tool_count", toolCount),
		slog.Int("duplicate_count", duplicateCount),
	})
}

// Emitted → awakening.emitted (A2UI message emission, NOT observability
// emission — naming matches the spec taxonomy).
func (e *Emitter) Emitted(ctx context.Context, componentCount int) {
	e.emit(ctx, "emitted", []slog.Attr{
		slog.Int("component_count", componentCount),
	})
}

// Failed → awakening.failed.
func (e *Emitter) Failed(ctx context.Context, stage, errorClass, errorMsg string) {
	e.emit(ctx, "failed", []slog.Attr{
		slog.String("stage", stage),
		slog.String("error_class", errorClass),
		slog.String("error_msg", errorMsg),
	})
}

// SLOBreached → awakening.slo.breached (REQ-804). latency and slo are dual-
// emitted as milliseconds so downstream alerting doesn't need to convert.
func (e *Emitter) SLOBreached(ctx context.Context, stage string, latency, slo time.Duration) {
	e.emit(ctx, "slo.breached", []slog.Attr{
		slog.String("stage", stage),
		slog.Int64("latency_ms", latency.Milliseconds()),
		slog.Int64("slo_ms", slo.Milliseconds()),
	})
}

// Mode → awakening.mode.{fanout,legacy} (SC-16). Emitted once per run so
// operators can see which path ran when BRAE_AWAKENING_MODE is set.
func (e *Emitter) Mode(ctx context.Context, mode string) {
	e.emit(ctx, "mode."+mode, []slog.Attr{
		slog.String("mode", mode),
	})
}

// HelpInvoked → awakening.helpparse.invoked (SC-11 REQ-1101 observability).
// Emitted once per variant (--help / -h) invocation per binary.
func (e *Emitter) HelpInvoked(ctx context.Context, binary, variant string, duration time.Duration, exitCode int) {
	e.emit(ctx, "helpparse.invoked", []slog.Attr{
		slog.String("binary", binary),
		slog.String("variant", variant),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("exit_code", exitCode),
	})
}

// HelpParsed → awakening.helpparse.parsed (SC-11 REQ-1102/1103). Emitted once
// per binary whose HelpSchema passed validation.
func (e *Emitter) HelpParsed(ctx context.Context, binary string, flagCount, subCount int) {
	e.emit(ctx, "helpparse.parsed", []slog.Attr{
		slog.String("binary", binary),
		slog.Int("flag_count", flagCount),
		slog.Int("sub_count", subCount),
	})
}

// HelpFailed → awakening.helpparse.failed (SC-11 REQ-1104). Emitted once per
// binary that the sub-CPN could not produce a HelpSchema for. Stage is one
// of "invoke" | "llm" | "validate".
func (e *Emitter) HelpFailed(ctx context.Context, binary, stage, reason string) {
	e.emit(ctx, "helpparse.failed", []slog.Attr{
		slog.String("binary", binary),
		slog.String("stage", stage),
		slog.String("reason", reason),
	})
}

// SynthesisStarted → awakening.synthesis.started (SC-12 REQ-1205
// observability). Emitted once per schema as the synthesis branch begins.
func (e *Emitter) SynthesisStarted(ctx context.Context, binary string) {
	e.emit(ctx, "synthesis.started", []slog.Attr{
		slog.String("binary", binary),
	})
}

// SynthesisCompleted → awakening.synthesis.completed (SC-12). Emitted once
// per binary whose ToolManifest was materialised and staged.
func (e *Emitter) SynthesisCompleted(ctx context.Context, binary, provenanceSHA256 string) {
	e.emit(ctx, "synthesis.completed", []slog.Attr{
		slog.String("binary", binary),
		slog.String("provenance_sha256", provenanceSHA256),
	})
}

// SynthesisFailed → awakening.synthesis.failed (SC-12 REQ-1205). Synthesis
// failures are non-fatal; this event records the drop without aborting
// sibling branches.
func (e *Emitter) SynthesisFailed(ctx context.Context, binary, reason string) {
	e.emit(ctx, "synthesis.failed", []slog.Attr{
		slog.String("binary", binary),
		slog.String("reason", reason),
	})
}

// SynthesizedToolHITLRequested → awakening.synth.hitl.requested (SC-13
// REQ-1301). Emitted when a synthesized tool invocation blocks pending
// first-run approval.
func (e *Emitter) SynthesizedToolHITLRequested(ctx context.Context, sessionID, toolName, provenanceSHA256 string) {
	e.emit(ctx, "synth.hitl.requested", []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("tool_name", toolName),
		slog.String("provenance_sha256", provenanceSHA256),
	})
}

// SynthesizedToolApproved → awakening.synth.approved (SC-13 REQ-1302).
// Emitted after an approval row is persisted.
func (e *Emitter) SynthesizedToolApproved(ctx context.Context, sessionID, toolName, provenanceSHA256 string) {
	e.emit(ctx, "synth.approved", []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("tool_name", toolName),
		slog.String("provenance_sha256", provenanceSHA256),
	})
}

// SynthesizedToolDenied → awakening.synth.denied (SC-13). Emitted when the
// operator denies the HITL prompt (or the gate fails closed).
func (e *Emitter) SynthesizedToolDenied(ctx context.Context, sessionID, toolName, provenanceSHA256, reason string) {
	e.emit(ctx, "synth.denied", []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("tool_name", toolName),
		slog.String("provenance_sha256", provenanceSHA256),
		slog.String("reason", reason),
	})
}

// SynthesizedToolAutoApproved → awakening.synth.autoapproved (SC-13
// REQ-1303 fast-path). Emitted when an identical (session_id, tool_name,
// provenance_sha256) hit returns approved without a prompt.
func (e *Emitter) SynthesizedToolAutoApproved(ctx context.Context, sessionID, toolName, provenanceSHA256 string) {
	e.emit(ctx, "synth.autoapproved", []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("tool_name", toolName),
		slog.String("provenance_sha256", provenanceSHA256),
	})
}

// CacheHit → awakening.cache.hit (needed by SC-03).
func (e *Emitter) CacheHit(ctx context.Context, age time.Duration) {
	e.emit(ctx, "cache.hit", []slog.Attr{
		slog.Int64("age_ms", age.Milliseconds()),
	})
}

// CheckSLO inspects `latency` against the REQ-803 thresholds and emits a
// breach event for each threshold crossed. The p50 threshold is the lowest,
// so "crossed p99" implies a prior crossing of p95 and p50 — the emitter
// only fires for the highest crossed tier to keep alert volume bounded while
// still surfacing the worst-case class.
func (e *Emitter) CheckSLO(ctx context.Context, stage string, latency time.Duration) {
	switch {
	case latency > AwakeningSLOp99:
		e.SLOBreached(ctx, stage, latency, AwakeningSLOp99)
	case latency > AwakeningSLOp95:
		e.SLOBreached(ctx, stage, latency, AwakeningSLOp95)
	case latency > AwakeningSLOp50:
		e.SLOBreached(ctx, stage, latency, AwakeningSLOp50)
	}
}
