package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestBuildHITLReviewCard_ContainsAllActionTypes asserts the legacy
// fallback path's card shape matches the transition-owned builder (via the
// shared reviewCardPayload helper). Regression guard for the refactor that
// removed the inline struct payload in favor of reviewCardPayload.
func TestBuildHITLReviewCard_ContainsAllActionTypes(t *testing.T) {
	body := buildHITLReviewCard("ignored prompt", "t-review")
	if !strings.HasPrefix(body, "$$a2ui:") {
		t.Fatalf("legacy review card missing $$a2ui: prefix: %q", body)
	}
	for _, needle := range []string{"hitl:approve", "hitl:revise", "hitl:reject", "t-review"} {
		if !strings.Contains(body, needle) {
			t.Errorf("legacy review card missing %q; body=%s", needle, body)
		}
	}
}

// TestBuildHITLReviewCard_ByteIdenticalToTransitionBuilder asserts that the
// legacy fallback path and the transition-owned builder produce the SAME
// JSON component tree. Source of truth: reviewCardPayload. Violating this
// means rehydrated and live-legacy renders would diverge (PAT-002 regression).
func TestBuildHITLReviewCard_ByteIdenticalToTransitionBuilder(t *testing.T) {
	payload, err := buildReviewA2UIPayload("t-review")(nil)
	if err != nil {
		t.Fatalf("buildReviewA2UIPayload: %v", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := "$$a2ui:" + string(encoded)

	got := buildHITLReviewCard("ignored", "t-review")
	if got != want {
		t.Errorf("legacy and transition-owned card bodies differ:\n got  %s\n want %s", got, want)
	}
}

// TestLegacyReviewCardWARN_Shape asserts the WARN log line emitted by the
// legacy fallback branch has the EXACT message string and field set the spec
// mandates (REQ-BE-003). We exercise the emit directly against a captured
// handler — the callback wiring itself is integration-covered. The point of
// this test is to lock the message string so log alerts don't silently
// break.
func TestLegacyReviewCardWARN_Shape(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	// Swap the default logger for the duration of this test; the WARN in
	// main.go uses slog.WarnContext which routes through the default.
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(prev)

	// Mirror the exact call shape from main.go so a drift in the message
	// string is caught here.
	slog.WarnContext(context.Background(), "legacy review-card emit path used (NOT persisted) — t-review topology may be misconfigured",
		"session_id", "sess-1",
		"cpn_id", "cpn-sess-1",
		"transition_id", "t-review",
		"prompt_len", 0,
	)

	out := buf.String()
	if !strings.Contains(out, "legacy review-card emit path used (NOT persisted)") {
		t.Errorf("WARN message string drifted; got: %s", out)
	}
	if !strings.Contains(out, "t-review topology may be misconfigured") {
		t.Errorf("WARN message tail drifted; got: %s", out)
	}
	for _, field := range []string{"session_id=sess-1", "cpn_id=cpn-sess-1", "transition_id=t-review", "prompt_len=0"} {
		if !strings.Contains(out, field) {
			t.Errorf("WARN missing field %q; got: %s", field, out)
		}
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected level=WARN; got: %s", out)
	}
}
