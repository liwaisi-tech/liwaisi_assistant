package awakens

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func TestTopologyFactory_Shape(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-1", Deps{HostID: "host-1"})
	if c == nil {
		t.Fatal("nil CPN")
	}
	expectedPlaces := []string{
		PlaceAwakenTrigger, PlaceAwakenSystemPrompt, PlaceAwakenShellCall,
		PlaceAwakenShellResult, PlaceAwakeningReport,
		cpn.WellKnownHostCapabilitiesPlace,
	}
	for _, id := range expectedPlaces {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place %q", id)
		}
	}
	expectedTransitions := []string{
		TransitionAwakenLLM, TransitionAwakenShell, TransitionAwakenReport,
		TransitionAwakenPersist, TransitionAwakenRegisterTools, TransitionAwakenEmitMessage,
	}
	for _, id := range expectedTransitions {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition %q", id)
		}
	}
	// Trigger place must be seeded with one token; system-prompt place too.
	tokens, _ := c.Places[PlaceAwakenTrigger].Peek()
	if len(tokens) != 1 {
		t.Errorf("trigger seed count: got %d want 1", len(tokens))
	}
	prompt, _ := c.Places[PlaceAwakenSystemPrompt].Peek()
	if len(prompt) != 1 {
		t.Errorf("prompt seed count: got %d want 1", len(prompt))
	}
}

func TestReportTransition_ValidatesPayload(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-1", Deps{})
	tt := c.Transitions[TransitionAwakenReport]
	// Happy path.
	r := validReport()
	raw, _ := json.Marshal(r)
	out, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: json.RawMessage(raw)},
	})
	if err != nil {
		t.Fatalf("report transition: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 out tokens, got %d", len(out))
	}
	// Invalid payload rejected.
	_, err = tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: "not json"},
	})
	if err == nil {
		t.Fatalf("invalid payload must return err")
	}
}

func TestPersistTransition_WritesSnapshot(t *testing.T) {
	t.Parallel()
	repo := &recordRepo{}
	c := TopologyFactory("sess-1", Deps{
		Repository: repo,
		HostID:     "host-1",
		Source:     SourceAwakening,
		Clock:      func() time.Time { return time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) },
	})
	tt := c.Transitions[TransitionAwakenPersist]
	r := validReport()
	_, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: r},
	})
	if err != nil {
		t.Fatalf("persist transition: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("expected 1 save, got %d", len(repo.saved))
	}
	if repo.saved[0].Source != SourceAwakening {
		t.Errorf("source: %q", repo.saved[0].Source)
	}
	if repo.saved[0].HostID != "host-1" {
		t.Errorf("host_id: %q", repo.saved[0].HostID)
	}
}

func TestEmitMessageTransition_A2UI(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-1", Deps{})
	tt := c.Transitions[TransitionAwakenEmitMessage]
	r := validReport()
	out, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorEvent, Space: cpn.SpaceComputation, Payload: r},
	})
	if err != nil {
		t.Fatalf("emit transition: %v", err)
	}
	tok, ok := out[PlaceAwakeningMessage+"-emitted"]
	if !ok {
		t.Fatal("missing emitted message token")
	}
	env, ok := tok.Payload.(A2UIMessage)
	if !ok {
		t.Fatalf("payload not A2UIMessage: %T", tok.Payload)
	}
	if len(env.Components) == 0 {
		t.Fatal("empty components")
	}
	// Serialises cleanly.
	if _, err := env.Marshal(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestRegisterToolsTransition_WithContext(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-1", Deps{})
	tt := c.Transitions[TransitionAwakenRegisterTools]

	reg := &fakeRegistry{}
	ctx := WithToolRegistry(context.Background(), reg)
	r := AwakeningReport{
		OS:            AwakeningOS{Name: "Alpine", Arch: "amd64"},
		Shell:         AwakeningShell{Path: "/bin/sh"},
		ToolsRegister: []AwakeningToolRegister{{Name: "shell-exec", Basis: "sh"}},
	}
	_, err := tt.ToolHandler(ctx, []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: r},
	})
	if err != nil {
		t.Fatalf("register-tools: %v", err)
	}
	if len(reg.calls) != 1 {
		t.Fatalf("registry calls: %d", len(reg.calls))
	}
}

// TestGoldenTranscript feeds a fixture transcript through the deterministic
// transitions (report → persist → emit → register) and asserts the expected
// snapshot row + A2UI envelope + registrations.
func TestGoldenTranscript_Alpine(t *testing.T) {
	t.Parallel()
	raw := goldenTranscriptAlpine
	report, err := ParseReport(raw)
	if err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	// Project → snapshot
	snap := report.Project("host-alpine", SourceAwakening, time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC))
	if snap.Source != SourceAwakening {
		t.Errorf("source: %q", snap.Source)
	}
	// Expected present/absent tools per AC-002.
	expectedPresent := map[string]bool{"sh": true, "awk": true, "sed": true, "grep": true, "tar": true, "wget": true}
	expectedAbsent := map[string]bool{"git": true, "python3": true, "node": true, "go": true, "gcc": true}
	for _, b := range snap.Binaries {
		if b.Present {
			delete(expectedPresent, b.Name)
		} else {
			delete(expectedAbsent, b.Name)
		}
	}
	if len(expectedPresent) > 0 {
		t.Errorf("missing present binaries: %v", expectedPresent)
	}
	if len(expectedAbsent) > 0 {
		t.Errorf("missing absent binaries: %v", expectedAbsent)
	}

	// Tool registrations
	reg := &fakeRegistry{}
	got, err := RegisterBatch(context.Background(), reg, report, nil)
	if err != nil {
		t.Fatalf("register batch: %v", err)
	}
	if len(got) == 0 {
		t.Errorf("golden fixture expects at least one registration")
	}

	// A2UI envelope
	env := BuildFirstTurnMessage(report)
	if len(env.Components) < 2 {
		t.Errorf("A2UI envelope must have card + trailing text")
	}
}

// recordRepo defined in fallback_test.go is reused.
var _ persist.HostCapabilityRepository = (*recordRepo)(nil)
