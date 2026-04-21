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
