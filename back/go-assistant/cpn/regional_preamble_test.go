package cpn

import (
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/prompts"
)

// TestRenderSystemPromptInjects verifies the per-session preamble is
// prepended to the transition's static SystemPrompt and never mutates it.
func TestRenderSystemPromptInjects(t *testing.T) {
	t.Parallel()

	const original = "STATIC PROMPT BODY"
	tr := &Transition{
		SystemPrompt: original,
		LLMConfig:    &LLMConfig{},
	}

	c := &CPN{RegionalVariant: "es-CO"}
	got := renderSystemPrompt(tr, c)

	if !strings.HasPrefix(got, "USER CONTEXT — REGIONAL REGISTER") {
		t.Errorf("expected preamble header at start, got: %q", got[:40])
	}
	if !strings.HasSuffix(got, original) {
		t.Errorf("expected static body at end, got: %q", got)
	}
	if tr.SystemPrompt != original {
		t.Errorf("CON-003 violated: Transition.SystemPrompt mutated to %q", tr.SystemPrompt)
	}
}

func TestRenderSystemPromptSkip(t *testing.T) {
	t.Parallel()

	const original = "STATIC"
	tr := &Transition{
		SystemPrompt: original,
		LLMConfig:    &LLMConfig{SkipRegionalPreamble: true},
	}
	c := &CPN{RegionalVariant: "es-CO"}

	if got := renderSystemPrompt(tr, c); got != original {
		t.Errorf("SkipRegionalPreamble=true should return original prompt; got %q", got)
	}
}

func TestRenderSystemPromptDefaultsForEmptyVariant(t *testing.T) {
	t.Parallel()

	tr := &Transition{SystemPrompt: "X", LLMConfig: &LLMConfig{}}
	c := &CPN{RegionalVariant: ""}

	got := renderSystemPrompt(tr, c)
	want := prompts.PreambleFor("es-CO") + "\n\nX"
	if got != want {
		t.Errorf("empty variant should fall back to es-CO preamble")
	}
}

// TestRenderSystemPromptConcurrent runs renderSystemPrompt concurrently from
// many goroutines using a SHARED Transition pointer but two different CPN
// sessions, asserting each call returns the preamble for its own variant
// and the shared Transition is never mutated. Run with -race.
func TestRenderSystemPromptConcurrent(t *testing.T) {
	t.Parallel()

	const original = "shared static prompt"
	shared := &Transition{
		SystemPrompt: original,
		LLMConfig:    &LLMConfig{},
	}

	cCO := &CPN{SessionID: "s1", RegionalVariant: "es-CO"}
	cMX := &CPN{SessionID: "s2", RegionalVariant: "es-MX"}

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range iters {
			got := renderSystemPrompt(shared, cCO)
			if !strings.Contains(got, "Español (Colombia)") {
				t.Errorf("session 1 missing es-CO preamble")
				return
			}
			if !strings.HasSuffix(got, original) {
				t.Errorf("session 1 missing static body")
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for range iters {
			got := renderSystemPrompt(shared, cMX)
			if !strings.Contains(got, "Español (México)") {
				t.Errorf("session 2 missing es-MX preamble")
				return
			}
			if !strings.HasSuffix(got, original) {
				t.Errorf("session 2 missing static body")
				return
			}
		}
	}()

	wg.Wait()

	if shared.SystemPrompt != original {
		t.Fatalf("CON-003 violated: shared Transition.SystemPrompt mutated to %q", shared.SystemPrompt)
	}
}
