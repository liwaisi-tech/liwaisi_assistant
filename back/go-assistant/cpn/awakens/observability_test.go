package awakens

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// captureEmitter returns an Emitter writing JSON slog records into buf.
func captureEmitter(buf *bytes.Buffer) *Emitter {
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return NewEmitter(slog.New(h))
}

func decodeRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decode slog line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestEmitter_NilReceiverIsNoop(t *testing.T) {
	var e *Emitter
	e.Started(context.Background(), "s", "m")
	e.Failed(context.Background(), "x", "y", "z")
	e.CheckSLO(context.Background(), "s", time.Hour)
}

func TestEmitter_NilLoggerDefaults(t *testing.T) {
	e := NewEmitter(nil)
	if e == nil || e.logger == nil {
		t.Fatal("NewEmitter(nil) should fall back to slog.Default()")
	}
}

func TestEmitter_EventsSlogShape(t *testing.T) {
	var buf bytes.Buffer
	e := captureEmitter(&buf)
	ctx := context.Background()

	e.Started(ctx, "sess-1", "google/gemini-2.5-flash")
	e.LLMBootstrapCompleted(ctx, 120*time.Millisecond, 42, 99)
	e.ProbeComposed(ctx, []string{"info-os-name", "info-user"})
	e.ProbeFired(ctx, "info-user", 30*time.Millisecond, 0)
	e.ProbeReduced(ctx, 7, 1)
	e.ReportProjected(ctx, 5, []string{"system", "developer"})
	e.Registered(ctx, 5, 2)
	e.Emitted(ctx, 2)
	e.Failed(ctx, "llm.bootstrap", "timeout", "deadline exceeded")
	e.SLOBreached(ctx, "total", 31*time.Second, AwakeningSLOp99)
	e.CacheHit(ctx, 5*time.Minute)

	recs := decodeRecords(t, &buf)
	want := []string{
		"brae.awakening.started",
		"brae.awakening.llm.bootstrap.completed",
		"brae.awakening.probe.composed",
		"brae.awakening.probe.fired",
		"brae.awakening.probe.reduced",
		"brae.awakening.report.projected",
		"brae.awakening.registered",
		"brae.awakening.emitted",
		"brae.awakening.failed",
		"brae.awakening.slo.breached",
		"brae.awakening.cache.hit",
	}
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d; buf=%s", len(recs), len(want), buf.String())
	}
	for i, w := range want {
		if recs[i]["msg"] != w {
			t.Errorf("record %d: msg=%q want %q", i, recs[i]["msg"], w)
		}
	}

	// Spot-check structured fields.
	if recs[0]["session_id"] != "sess-1" || recs[0]["model"] != "google/gemini-2.5-flash" {
		t.Errorf("started record fields: %+v", recs[0])
	}
	if recs[3]["slug"] != "info-user" || recs[3]["exit_code"].(float64) != 0 {
		t.Errorf("probe.fired fields: %+v", recs[3])
	}
	if recs[9]["slo_ms"].(float64) != float64(AwakeningSLOp99.Milliseconds()) {
		t.Errorf("slo.breached slo_ms mismatch: %+v", recs[9])
	}
	if recs[10]["age_ms"].(float64) != float64((5 * time.Minute).Milliseconds()) {
		t.Errorf("cache.hit age_ms mismatch: %+v", recs[10])
	}
}

func TestEmitter_CheckSLO_TierSelection(t *testing.T) {
	cases := []struct {
		name      string
		latency   time.Duration
		wantEvent bool
		wantSLO   time.Duration
	}{
		{"under-p50", 5 * time.Second, false, 0},
		{"over-p50", 10 * time.Second, true, AwakeningSLOp50},
		{"over-p95", 20 * time.Second, true, AwakeningSLOp95},
		{"over-p99", 45 * time.Second, true, AwakeningSLOp99},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := captureEmitter(&buf)
			e.CheckSLO(context.Background(), "total", tc.latency)
			recs := decodeRecords(t, &buf)
			if !tc.wantEvent {
				if len(recs) != 0 {
					t.Fatalf("expected no breach, got %+v", recs)
				}
				return
			}
			if len(recs) != 1 {
				t.Fatalf("expected 1 breach record, got %d: %+v", len(recs), recs)
			}
			if recs[0]["msg"] != "brae.awakening.slo.breached" {
				t.Errorf("unexpected msg %q", recs[0]["msg"])
			}
			if got := time.Duration(recs[0]["slo_ms"].(float64)) * time.Millisecond; got != tc.wantSLO {
				t.Errorf("slo_ms=%s want %s", got, tc.wantSLO)
			}
		})
	}
}

func TestEmitter_AttachesToActiveSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("awakens-test")

	var buf bytes.Buffer
	e := captureEmitter(&buf)

	ctx, span := tracer.Start(context.Background(), "awakening.test")
	e.ProbeFired(ctx, "info-os-arch", 7*time.Millisecond, 0)
	e.CacheHit(ctx, 42*time.Second)
	span.End()

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(ended))
	}
	events := ended[0].Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 span events, got %d", len(events))
	}
	if events[0].Name != "brae.awakening.probe.fired" {
		t.Errorf("event[0] name=%q", events[0].Name)
	}
	if events[1].Name != "brae.awakening.cache.hit" {
		t.Errorf("event[1] name=%q", events[1].Name)
	}

	// Also confirm attributes propagate onto the span itself.
	attrs := ended[0].Attributes()
	var sawSlug bool
	for _, a := range attrs {
		if string(a.Key) == "brae.awakening.probe.fired.slug" && a.Value.AsString() == "info-os-arch" {
			sawSlug = true
		}
	}
	if !sawSlug {
		t.Errorf("probe.fired.slug attribute missing from span: %+v", attrs)
	}
}

func TestSlogAttrToOTEL_AllKinds(t *testing.T) {
	cases := []slog.Attr{
		slog.String("s", "v"),
		slog.Int64("i64", 7),
		slog.Uint64("u64", 9),
		slog.Float64("f", 1.5),
		slog.Bool("b", true),
		slog.Duration("d", 250*time.Millisecond),
		slog.Time("t", time.Unix(0, 0).UTC()),
		slog.Any("ss", []string{"a", "b"}),
		slog.Any("fallback", struct{ X int }{X: 1}),
	}
	for _, a := range cases {
		kv := slogAttrToOTEL("brae.awakening.test", a)
		if string(kv.Key) == "" {
			t.Errorf("empty key for %v", a)
		}
	}
}

func TestEmitter_NoSpanNoCrash(t *testing.T) {
	var buf bytes.Buffer
	e := captureEmitter(&buf)
	// Context has no active span — must not panic.
	e.Started(context.Background(), "sess", "model")
	if buf.Len() == 0 {
		t.Fatal("expected slog output even without active span")
	}
}
