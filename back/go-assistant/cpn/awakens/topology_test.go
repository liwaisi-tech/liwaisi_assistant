package awakens

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// recordRepo is a test double used across the awakens package tests.
type recordRepo struct {
	saved []persist.HostCapabilitySnapshot
}

func (r *recordRepo) Save(_ context.Context, s persist.HostCapabilitySnapshot) error {
	r.saved = append(r.saved, s)
	return nil
}
func (r *recordRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	return persist.HostCapabilitySnapshot{}, persist.ErrHostSnapshotNotFound
}
func (r *recordRepo) AppendProbeResult(_ context.Context, _ string, _ persist.BinaryProbe) error {
	return nil
}

var _ persist.HostCapabilityRepository = (*recordRepo)(nil)

func TestTopologyFactory_Shape(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-1", Deps{HostID: "host-1"})
	if c == nil {
		t.Fatal("nil CPN")
	}
	expectedPlaces := []string{
		PlaceAwakenTrigger, PlaceAwakenSystemPrompt,
		PlaceAwakenPlan, PlaceAwakenSubnetSpec, PlaceAwakeningReport,
		cpn.WellKnownHostCapabilitiesPlace,
	}
	for _, id := range expectedPlaces {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place %q", id)
		}
	}
	expectedTransitions := []string{
		TransitionAwakenLLMBootstrap,
		TransitionAwakenProbeCompose, TransitionAwakenProbeInstantiate,
		TransitionAwakenReport,
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

// TestBootstrapTransition_InputsEnabledAtStart is the core of the bootstrap
// fix: the first LLM transition MUST be firable with only the seed tokens
// (trigger + system-prompt). Any dependency on p-awaken-shell-result would
// re-introduce the deadlock.
func TestBootstrapTransition_InputsEnabledAtStart(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-boot", Deps{})
	tt := c.Transitions[TransitionAwakenLLMBootstrap]
	if tt == nil {
		t.Fatal("bootstrap transition missing")
	}
	wantInputs := map[string]bool{
		PlaceAwakenTrigger:      true,
		PlaceAwakenSystemPrompt: true,
	}
	if len(tt.InputPlaces) != len(wantInputs) {
		t.Fatalf("bootstrap inputs: got %v want %v", tt.InputPlaces, wantInputs)
	}
	for _, in := range tt.InputPlaces {
		if !wantInputs[in] {
			t.Errorf("unexpected bootstrap input %q (must not depend on shell-result)", in)
		}
	}
	// canFire should be true right after construction.
	if !tt.CanFire(c.Places) {
		t.Fatal("bootstrap transition must be firable from initial marking")
	}
}

// TestTopology_PlanToFanout_WireUp asserts the plan → compose → subnet-spec
// → instantiate → report arcs are wired per spec §4.4 and that the legacy
// in-turn shell places/transitions no longer exist.
func TestTopology_PlanToFanout_WireUp(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-wire", Deps{})

	// Legacy in-turn shell places/transitions MUST be gone — the probe
	// fanout sub-CPN replaces them.
	legacyNames := []string{
		"t-awaken-shell", "t-awaken-llm-followup",
		"p-awaken-shell-call", "p-awaken-shell-result",
	}
	for _, n := range legacyNames {
		if _, ok := c.Transitions[n]; ok {
			t.Errorf("legacy transition %q must not be present", n)
		}
		if _, ok := c.Places[n]; ok {
			t.Errorf("legacy place %q must not be present", n)
		}
	}

	// Bootstrap LLM must emit to p-awaken-plan.
	boot := c.Transitions[TransitionAwakenLLMBootstrap]
	if boot == nil {
		t.Fatal("bootstrap transition missing")
	}
	if len(boot.OutputPlaces) != 1 || boot.OutputPlaces[0] != PlaceAwakenPlan {
		t.Errorf("bootstrap outputs = %v; want [%s]", boot.OutputPlaces, PlaceAwakenPlan)
	}

	// compose: p-awaken-plan -> p-awaken-subnet-spec
	compose := c.Transitions[TransitionAwakenProbeCompose]
	if compose == nil {
		t.Fatal("probe-compose transition missing")
	}
	if len(compose.InputPlaces) != 1 || compose.InputPlaces[0] != PlaceAwakenPlan {
		t.Errorf("compose inputs = %v; want [%s]", compose.InputPlaces, PlaceAwakenPlan)
	}
	if len(compose.OutputPlaces) != 1 || compose.OutputPlaces[0] != PlaceAwakenSubnetSpec {
		t.Errorf("compose outputs = %v; want [%s]", compose.OutputPlaces, PlaceAwakenSubnetSpec)
	}
	if compose.Kind != cpn.NodeKindTool {
		t.Errorf("compose kind = %v; want NodeKindTool", compose.Kind)
	}

	// instantiate: p-awaken-subnet-spec -> p-awakening-report-raw
	inst := c.Transitions[TransitionAwakenProbeInstantiate]
	if inst == nil {
		t.Fatal("probe-instantiate transition missing")
	}
	if len(inst.InputPlaces) != 1 || inst.InputPlaces[0] != PlaceAwakenSubnetSpec {
		t.Errorf("instantiate inputs = %v; want [%s]", inst.InputPlaces, PlaceAwakenSubnetSpec)
	}
	if len(inst.OutputPlaces) != 1 || inst.OutputPlaces[0] != PlaceAwakeningReportRaw {
		t.Errorf("instantiate outputs = %v; want [%s]", inst.OutputPlaces, PlaceAwakeningReportRaw)
	}

	// curate: p-awakening-report-raw -> p-awakening-report
	curate := c.Transitions[TransitionAwakenCurate]
	if curate == nil {
		t.Fatal("curate transition missing")
	}
	if len(curate.InputPlaces) != 1 || curate.InputPlaces[0] != PlaceAwakeningReportRaw {
		t.Errorf("curate inputs = %v; want [%s]", curate.InputPlaces, PlaceAwakeningReportRaw)
	}
	if len(curate.OutputPlaces) != 1 || curate.OutputPlaces[0] != PlaceAwakeningReport {
		t.Errorf("curate outputs = %v; want [%s]", curate.OutputPlaces, PlaceAwakeningReport)
	}

	// synth: p-awakening-report -> p-awakening-report-synth
	synth := c.Transitions[TransitionAwakenSynth]
	if synth == nil {
		t.Fatal("synth transition missing")
	}
	if len(synth.InputPlaces) != 1 || synth.InputPlaces[0] != PlaceAwakeningReport {
		t.Errorf("synth inputs = %v; want [%s]", synth.InputPlaces, PlaceAwakeningReport)
	}
	if len(synth.OutputPlaces) != 1 || synth.OutputPlaces[0] != PlaceAwakeningReportSynth {
		t.Errorf("synth outputs = %v; want [%s]", synth.OutputPlaces, PlaceAwakeningReportSynth)
	}

	// Report consumes p-awakening-report-synth.
	report := c.Transitions[TransitionAwakenReport]
	if report == nil {
		t.Fatal("report transition missing")
	}
	if len(report.InputPlaces) != 1 || report.InputPlaces[0] != PlaceAwakeningReportSynth {
		t.Errorf("report inputs = %v; want [%s]", report.InputPlaces, PlaceAwakeningReportSynth)
	}
}

// TestNoLegacyLLMTransition guards against reintroduction of the single
// `t-awaken-llm` transition that caused the original deadlock.
func TestNoLegacyLLMTransition(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-legacy", Deps{})
	if _, ok := c.Transitions["t-awaken-llm"]; ok {
		t.Fatal("legacy t-awaken-llm must not be present; use the plan/fanout pipeline")
	}
}

// TestProbeComposeTransition_NoComposerConfigured surfaces a clear
// diagnostic when Deps.Composer is nil rather than silently stalling.
func TestProbeComposeTransition_NoComposerConfigured(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-noComposer", Deps{})
	tt := c.Transitions[TransitionAwakenProbeCompose]
	_, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: []byte(`{"probes":[]}`)},
	})
	if err == nil {
		t.Fatal("expected error when composer is nil")
	}
}

// TestProbeComposeTransition_InvokesComposer exercises the compose path
// with a fake ComposerFunc — the returned *cpn.CPN MUST be deposited on
// p-awaken-subnet-spec.
func TestProbeComposeTransition_InvokesComposer(t *testing.T) {
	t.Parallel()
	fake := cpn.NewCPN("child-fake", "fanout", 0, cpn.ModeMAS, "s",
		map[string]*cpn.Place{
			"p-terminal": cpn.NewPlace("p-terminal", cpn.ColorArtifact, cpn.SpaceComputation),
		}, map[string]*cpn.Transition{})
	var gotSession string
	composer := func(_ context.Context, sid string, _ any, _ ComposerDeps) (*cpn.CPN, error) {
		gotSession = sid
		return fake, nil
	}
	c := TopologyFactory("sess-compose", Deps{Composer: composer})
	tt := c.Transitions[TransitionAwakenProbeCompose]
	out, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: []byte(`{"probes":[]}`)},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if gotSession != "sess-compose" {
		t.Errorf("composer session = %q; want sess-compose", gotSession)
	}
	tok, ok := out[PlaceAwakenSubnetSpec]
	if !ok {
		t.Fatal("missing output token for p-awaken-subnet-spec")
	}
	if child, ok := tok.Payload.(*cpn.CPN); !ok || child != fake {
		t.Fatalf("payload = %T; want *cpn.CPN == fake", tok.Payload)
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
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: json.RawMessage(raw)},
	})
	if err != nil {
		t.Fatalf("report transition: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 out tokens, got %d", len(out))
	}
	// Invalid payload rejected.
	_, err = tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: "not json"},
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
	out, err := tt.ToolHandler(ctx, []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: r},
	})
	if err != nil {
		t.Fatalf("register-tools: %v", err)
	}
	if len(reg.calls) != 1 {
		t.Fatalf("registry calls: %d", len(reg.calls))
	}
	donePlace := PlaceAwakeningToolBatch + "-done"
	doneTok, ok := out[donePlace]
	if !ok {
		t.Fatalf("missing token for output place %s (fireToolHandler would error)", donePlace)
	}
	if doneTok.Color != cpn.ColorEvent {
		t.Fatalf("done token color: got %v want %v", doneTok.Color, cpn.ColorEvent)
	}
	if _, ok := doneTok.Payload.([]string); !ok {
		t.Fatalf("done token payload: got %T want []string", doneTok.Payload)
	}
}

// TestRegisterToolsTransition_NilRegistry guards REQ: even without a wired
// ToolRegistry the transition MUST still deposit a token into the -done
// output place, or fireToolHandler returns "ToolHandler produced no token
// for output place p-awakening-toolbatch-done" and the awakening flow
// errors out (production regression observed 2026-04-21).
func TestRegisterToolsTransition_NilRegistry(t *testing.T) {
	t.Parallel()
	c := TopologyFactory("sess-nilreg", Deps{})
	tt := c.Transitions[TransitionAwakenRegisterTools]
	r := AwakeningReport{
		OS:    AwakeningOS{Name: "Alpine", Arch: "amd64"},
		Shell: AwakeningShell{Path: "/bin/sh"},
	}
	out, err := tt.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: r},
	})
	if err != nil {
		t.Fatalf("register-tools (nil registry): %v", err)
	}
	donePlace := PlaceAwakeningToolBatch + "-done"
	if _, ok := out[donePlace]; !ok {
		t.Fatalf("nil-registry path must still emit %s token", donePlace)
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
	// Expected present/absent tools.
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
