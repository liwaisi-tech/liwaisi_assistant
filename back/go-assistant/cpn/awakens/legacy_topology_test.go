package awakens

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestLegacyTopologyFactory_ThreeTransitionsNoProbeFanout(t *testing.T) {
	t.Parallel()
	c := LegacyTopologyFactory("sess-legacy", Deps{HostID: "host-legacy"})
	if c == nil {
		t.Fatal("nil CPN")
	}
	wantIDs := map[string]cpn.NodeKind{
		TransitionLegacyLLM:         cpn.NodeKindLLM,
		TransitionLegacyPersist:     cpn.NodeKindTool,
		TransitionLegacyEmitMessage: cpn.NodeKindTool,
	}
	if got := len(c.Transitions); got != len(wantIDs) {
		t.Errorf("transition count = %d; want %d", got, len(wantIDs))
	}
	for id, kind := range wantIDs {
		tr, ok := c.Transitions[id]
		if !ok {
			t.Errorf("missing transition %q", id)
			continue
		}
		if tr.Kind != kind {
			t.Errorf("%s kind = %v; want %v", id, tr.Kind, kind)
		}
	}
	// No probe-compose or probe-instantiate transitions must be wired.
	banned := []string{TransitionAwakenProbeCompose, TransitionAwakenProbeInstantiate, TransitionAwakenLLMBootstrap}
	for _, id := range banned {
		if _, ok := c.Transitions[id]; ok {
			t.Errorf("legacy topology must not wire fanout transition %q", id)
		}
	}
	// Trigger + system-prompt places must be seeded.
	if tokens, _ := c.Places[PlaceAwakenTrigger].Peek(); len(tokens) != 1 {
		t.Errorf("trigger seed count: got %d want 1", len(tokens))
	}
	if tokens, _ := c.Places[PlaceAwakenSystemPrompt].Peek(); len(tokens) != 1 {
		t.Errorf("prompt seed count: got %d want 1", len(tokens))
	}
}

func TestReadAwakeningMode_DefaultAndValid(t *testing.T) {
	t.Setenv(AwakeningModeEnv, "")
	if got := ReadAwakeningMode(nil); got != ModeFanout {
		t.Errorf("unset env: got %q; want %q", got, ModeFanout)
	}
	t.Setenv(AwakeningModeEnv, "fanout")
	if got := ReadAwakeningMode(nil); got != ModeFanout {
		t.Errorf("fanout: got %q; want %q", got, ModeFanout)
	}
	t.Setenv(AwakeningModeEnv, "legacy")
	if got := ReadAwakeningMode(nil); got != ModeLegacy {
		t.Errorf("legacy: got %q; want %q", got, ModeLegacy)
	}
	t.Setenv(AwakeningModeEnv, "LEGACY")
	if got := ReadAwakeningMode(nil); got != ModeLegacy {
		t.Errorf("LEGACY (case): got %q; want %q", got, ModeLegacy)
	}
}

func TestReadAwakeningMode_UnknownValue(t *testing.T) {
	t.Setenv(AwakeningModeEnv, "bogus")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	got := ReadAwakeningMode(logger)
	if got != ModeFanout {
		t.Errorf("unknown value: got %q; want %q (default)", got, ModeFanout)
	}
	if !strings.Contains(buf.String(), "unknown BRAE_AWAKENING_MODE") {
		t.Errorf("expected warning log about unknown value; got %q", buf.String())
	}
}
