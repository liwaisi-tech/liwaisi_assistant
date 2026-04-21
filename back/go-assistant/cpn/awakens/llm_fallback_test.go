package awakens

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestFallbackChain_PrimarySucceeds_NoFallback(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := captureEmitter(buf)

	chain := DefaultFallbackChain()
	var seen []string
	err := chain.Run(context.Background(), emitter, func(_ context.Context, model string) error {
		seen = append(seen, model)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(seen) != 1 || seen[0] != AwakeningPrimaryModel {
		t.Fatalf("expected single primary attempt, got %v", seen)
	}
	if strings.Contains(buf.String(), "llm.fallback_used") {
		t.Fatalf("unexpected fallback_used event: %s", buf.String())
	}
}

func TestFallbackChain_Primary503_FallbackToHaiku(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := captureEmitter(buf)

	chain := DefaultFallbackChain()
	var seen []string
	err := chain.Run(context.Background(), emitter, func(_ context.Context, model string) error {
		seen = append(seen, model)
		if model == AwakeningPrimaryModel {
			return cpn.ErrProviderUnavailable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success on fallback, got %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 attempts, got %v", seen)
	}
	if seen[1] != "anthropic/claude-haiku-4-5" {
		t.Fatalf("expected first fallback haiku, got %q", seen[1])
	}
	events := countEvent(buf, "llm.fallback_used")
	if events != 1 {
		t.Fatalf("expected 1 fallback_used event, got %d (log=%s)", events, buf.String())
	}
}

func TestFallbackChain_PrimaryTimeout_HaikuTimeout_GPTSucceeds(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := captureEmitter(buf)

	chain := DefaultFallbackChain()
	var seen []string
	err := chain.Run(context.Background(), emitter, func(_ context.Context, model string) error {
		seen = append(seen, model)
		switch model {
		case AwakeningPrimaryModel, "anthropic/claude-haiku-4-5":
			return context.DeadlineExceeded
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success on third, got %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 attempts, got %v", seen)
	}
	if seen[2] != "openai/gpt-4o-mini" {
		t.Fatalf("expected final attempt gpt-4o-mini, got %q", seen[2])
	}
	events := countEvent(buf, "llm.fallback_used")
	if events != 2 {
		t.Fatalf("expected 2 fallback_used events, got %d", events)
	}
}

func TestFallbackChain_Primary401_FailsFast(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := captureEmitter(buf)

	chain := DefaultFallbackChain()
	var seen []string
	err := chain.Run(context.Background(), emitter, func(_ context.Context, model string) error {
		seen = append(seen, model)
		return cpn.ErrUnauthorized
	})
	if !errors.Is(err, cpn.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized surfaced, got %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("expected no fallback attempts, got %v", seen)
	}
	if strings.Contains(buf.String(), "llm.fallback_used") {
		t.Fatalf("auth error must not emit fallback_used: %s", buf.String())
	}
}

func TestFallbackChain_AllFail_SurfacesFinal(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := captureEmitter(buf)

	chain := DefaultFallbackChain()
	finalErr := cpn.ErrProviderOverloaded
	err := chain.Run(context.Background(), emitter, func(_ context.Context, _ string) error {
		return finalErr
	})
	if !errors.Is(err, finalErr) {
		t.Fatalf("expected final error surfaced, got %v", err)
	}
	events := countEvent(buf, "llm.fallback_used")
	if events != 2 {
		t.Fatalf("expected 2 fallback_used events (two retries), got %d", events)
	}
}

func TestIsFallbackEligible(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"auth_401", cpn.ErrUnauthorized, false},
		{"auth_403", cpn.ErrForbidden, false},
		{"bad_request", cpn.ErrBadRequest, false},
		{"not_found", cpn.ErrNotFound, false},
		{"canceled", context.Canceled, false},
		{"rate_limited", cpn.ErrRateLimited, true},
		{"provider_overloaded", cpn.ErrProviderOverloaded, true},
		{"provider_unavailable", cpn.ErrProviderUnavailable, true},
		{"edge_timeout", cpn.ErrEdgeTimeout, true},
		{"request_timeout", cpn.ErrRequestTimeout, true},
		{"deadline", context.DeadlineExceeded, true},
		{"wrapped_5xx", errors.New("openrouter status 502"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFallbackEligible(tc.err); got != tc.want {
				t.Fatalf("isFallbackEligible(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func countEvent(buf *bytes.Buffer, name string) int {
	return strings.Count(buf.String(), "brae.awakening."+name)
}
