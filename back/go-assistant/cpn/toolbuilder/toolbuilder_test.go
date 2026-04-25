package toolbuilder

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestBuildToolCreatorTopology_AllPlacesPresent(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	expected := []string{
		PlaceRequest, PlaceTriaged, PlaceReqInvestigate, PlaceReqScaffold, PlaceReqDraft,
		PlaceExistingTools, PlaceWorkspaceReady, PlaceSpecV0, PlaceSpecDraft,
		PlaceSpecForGo, PlaceSpecForDevOps, PlaceSpecForArch,
		PlaceReviewGo, PlaceReviewDevOps, PlaceReviewArch,
		PlaceSpecApproved, PlaceRefineRequest,
		PlaceTotals, PlaceDoIt, PlaceTotalsEcho, PlaceTotalReady, PlaceSubtasks, PlaceTested,
		PlacePackaged, PlaceInstallResult, PlaceRegistered,
		PlaceErrors, PlaceFailed,
	}
	for _, id := range expected {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place: %s", id)
		}
	}
	if got, want := len(c.Places), len(expected); got != want {
		t.Errorf("place count: got %d, want %d", got, want)
	}
}

func TestBuildToolCreatorTopology_AllTransitionsPresent(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	expected := []string{
		TrTriage, TrDispatch, TrInvestigate, TrScaffoldWorkspace, TrDraftSpecV0,
		TrEnrichSpec, TrFanoutReviewers,
		TrReviewGo, TrReviewDevOps, TrReviewArch,
		TrAggregateApprove, TrAggregateRefine, TrRefineSpec,
		TrDecomposeTotals, TrAuthorizeTotals, TrGateTotal, TrPlanSubtasks,
		TrTDDLoop, TrPackage, TrInstall, TrFinalizeInstall, TrHandleError,
	}
	for _, id := range expected {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition: %s", id)
		}
	}
}

func TestBuildToolCreatorTopology_LLMTransitionsHaveConfig(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	llmIDs := []string{
		TrTriage, TrDraftSpecV0,
		TrReviewGo, TrReviewDevOps, TrReviewArch,
		TrRefineSpec, TrDecomposeTotals, TrPlanSubtasks,
	}
	for _, id := range llmIDs {
		tr := c.Transitions[id]
		if tr.Kind != cpn.NodeKindLLM {
			t.Errorf("%s: Kind=%s, want LLM", id, tr.Kind)
		}
		if tr.SystemPrompt == "" {
			t.Errorf("%s: empty SystemPrompt", id)
		}
		if tr.LLMConfig == nil {
			t.Errorf("%s: nil LLMConfig", id)
		}
	}
}

func TestBuildToolCreatorTopology_ToolTransitionsHaveHandler(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	toolIDs := []string{
		TrDispatch, TrInvestigate, TrEnrichSpec, TrFanoutReviewers,
		TrAggregateApprove, TrAggregateRefine,
		TrAuthorizeTotals, TrGateTotal, TrTDDLoop,
		TrFinalizeInstall, TrHandleError,
	}
	for _, id := range toolIDs {
		tr := c.Transitions[id]
		if tr.Kind != cpn.NodeKindTool {
			t.Errorf("%s: Kind=%s, want Tool", id, tr.Kind)
		}
		if tr.ToolHandler == nil {
			t.Errorf("%s: nil ToolHandler", id)
		}
	}
}

func TestBuildToolCreatorTopology_ScaffoldPackageInstallWired(t *testing.T) {
	// t-scaffold-workspace and t-package are Tool stubs today: their
	// output places carry non-shell colors (ARTIFACT, TOOL_MANIFEST), so
	// NodeKindBash would fail validation.
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	for _, id := range []string{TrScaffoldWorkspace, TrPackage} {
		tr := c.Transitions[id]
		if tr.Kind != cpn.NodeKindTool {
			t.Errorf("%s: Kind=%s, want Tool", id, tr.Kind)
		}
		if tr.ToolHandler == nil {
			t.Errorf("%s: nil ToolHandler", id)
		}
	}
	if c.Transitions[TrInstall].Kind != cpn.NodeKindBash {
		t.Errorf("%s: Kind=%s, want Bash", TrInstall, c.Transitions[TrInstall].Kind)
	}
	if c.Transitions[TrInstall].BashConfig == nil {
		t.Errorf("%s: nil BashConfig", TrInstall)
	}
}

func TestBuildToolCreatorTopology_ErrorRouting(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	for id, tr := range c.Transitions {
		// t-handle-error is itself the PlaceErrors consumer; it has no
		// ErrorPlace of its own.
		if id == TrHandleError {
			if tr.ErrorPlace != "" {
				t.Errorf("%s: ErrorPlace should be empty, got %q", id, tr.ErrorPlace)
			}
			continue
		}
		if tr.ErrorPlace == "" {
			t.Errorf("%s: missing ErrorPlace", id)
			continue
		}
		if tr.ErrorPlace != PlaceErrors {
			t.Errorf("%s: ErrorPlace=%q, want %q", id, tr.ErrorPlace, PlaceErrors)
		}
	}
}

// TestHandleErrorTransition_WiredAndConsumesErrors asserts the
// halt-bug fix: a failed sub-agent deposits a token into PlaceErrors
// (via depositSubAgentError) and t-handle-error drains it into
// PlaceFailed so the CPN reaches an explicit terminal state instead of
// deadlocking.
func TestHandleErrorTransition_WiredAndConsumesErrors(t *testing.T) {
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	tr, ok := c.Transitions[TrHandleError]
	if !ok {
		t.Fatal("t-handle-error is not registered")
	}
	if len(tr.InputPlaces) != 1 || tr.InputPlaces[0] != PlaceErrors {
		t.Errorf("t-handle-error inputs=%v, want [%s]", tr.InputPlaces, PlaceErrors)
	}
	if len(tr.OutputPlaces) != 1 || tr.OutputPlaces[0] != PlaceFailed {
		t.Errorf("t-handle-error outputs=%v, want [%s]", tr.OutputPlaces, PlaceFailed)
	}

	// Simulate a validator failure deposit and assert the handler
	// transforms it into a PlaceFailed token.
	failurePayload := `{"transition_id":"t-review-spec-go-eng","profile_id":"go-eng","action_id":"review-spec","stage":"validate","error":"missing required fields: role"}`
	out, err := tr.ToolHandler(context.Background(), []cpn.Token{
		{Color: cpn.ColorError, Space: cpn.SpaceComputation, Payload: failurePayload},
	})
	if err != nil {
		t.Fatalf("handle-error handler returned error: %v", err)
	}
	failedTok, ok := out[PlaceFailed]
	if !ok {
		t.Fatal("handler did not emit a PlaceFailed token")
	}
	if failedTok.Color != cpn.ColorError {
		t.Errorf("failed token color=%s, want %s", failedTok.Color, cpn.ColorError)
	}
}

func TestBuildToolCreatorTopology_AggregatorAlwaysApproves(t *testing.T) {
	// Autonomous mode: the aggregator always approves once every reviewer
	// slot has produced a token. Reviewer blocking_issues become
	// suggestions on the downstream Verdict; the refine guard is now
	// dormant (kept for back-compat but never fires).
	c := BuildToolCreatorTopology("test-session", ToolCreatorDeps{})
	app := c.Transitions[TrAggregateApprove]
	ref := c.Transitions[TrAggregateRefine]
	if app.Guard == nil {
		t.Fatal("approve aggregator has no guard")
	}
	if ref.Guard == nil {
		t.Fatal("refine aggregator has no guard")
	}
	mk := func(role string, approved bool, blocking []string) *cpn.Token {
		b, _ := json.Marshal(Review{Role: role, Approved: approved, BlockingIssues: blocking})
		return &cpn.Token{Color: cpn.ColorJSON, Payload: string(b)}
	}
	allApproved := make([]*cpn.Token, 0, len(reviewerRoles))
	for _, r := range reviewerRoles {
		allApproved = append(allApproved, mk(r.ProfileID, true, nil))
	}
	if !app.Guard(allApproved) {
		t.Error("approve guard should fire when all slots present")
	}
	if ref.Guard(allApproved) {
		t.Error("refine guard must never fire (dormant)")
	}

	// One blocker → approve still fires (autonomous), refine still dormant.
	oneBlocker := append([]*cpn.Token{}, allApproved...)
	oneBlocker[0] = mk(reviewerRoles[0].ProfileID, false, []string{"missing non-goals"})
	if !app.Guard(oneBlocker) {
		t.Error("approve guard should fire with a blocker (autonomous mode)")
	}
	if ref.Guard(oneBlocker) {
		t.Error("refine guard must never fire even with blockers")
	}
}

func TestDispatchHandler_PassesRequestToAllThree(t *testing.T) {
	req := Request{Name: "echo-tool", Module: "brae.tools/echo-tool", Description: "prints args"}
	verdict := TriageVerdict{Ready: true, NormalisedRequest: req}
	b, _ := json.Marshal(verdict)
	h := dispatchHandler()
	out, err := h(context.Background(), []cpn.Token{{Color: cpn.ColorJSON, Payload: string(b)}})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	for _, place := range []string{PlaceReqInvestigate, PlaceReqScaffold, PlaceReqDraft} {
		if _, ok := out[place]; !ok {
			t.Errorf("dispatch did not emit to %s", place)
		}
	}
}

func TestDispatchHandler_NotReadyFails(t *testing.T) {
	verdict := TriageVerdict{Ready: false, ClarifyQuestion: "what should it do?"}
	b, _ := json.Marshal(verdict)
	h := dispatchHandler()
	_, err := h(context.Background(), []cpn.Token{{Color: cpn.ColorJSON, Payload: string(b)}})
	if err == nil {
		t.Fatal("expected error on not-ready triage")
	}
}

func TestAuthorizeTotalsHandler_ValidBatch(t *testing.T) {
	batch := TotalsBatch{
		SpecName: "echo-tool",
		Totals: []TotalSpec{
			{ID: "T1", Name: "domain", Deliverables: []string{"value objects"}, ParallelSafe: true},
			{ID: "T2", Name: "adapter", Deliverables: []string{"stdout echo"}, DependsOn: []string{"T1"}},
		},
	}
	b, _ := json.Marshal(batch)
	out, err := authorizeTotalsHandler()(context.Background(),
		[]cpn.Token{{Color: cpn.ColorJSON, Payload: string(b)}})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if _, ok := out[PlaceDoIt]; !ok {
		t.Errorf("missing doit output")
	}
	if _, ok := out[PlaceTotalsEcho]; !ok {
		t.Errorf("missing echo output")
	}
	var doit DoItBatch
	_ = decodeAs(out[PlaceDoIt].Payload, &doit)
	if len(doit.Entries) != 2 {
		t.Errorf("doit entries = %d, want 2", len(doit.Entries))
	}
	for _, e := range doit.Entries {
		if !e.Approved {
			t.Errorf("entry %s not approved", e.TotalID)
		}
	}
}

func TestAuthorizeTotalsHandler_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		batch TotalsBatch
	}{
		{"empty", TotalsBatch{}},
		{"too-many", TotalsBatch{Totals: func() []TotalSpec {
			s := make([]TotalSpec, MaxTotals+1)
			for i := range s {
				s[i] = TotalSpec{ID: string(rune('A' + i)), Deliverables: []string{"x"}}
			}
			return s
		}()}},
		{"dup-id", TotalsBatch{Totals: []TotalSpec{
			{ID: "T1", Deliverables: []string{"a"}},
			{ID: "T1", Deliverables: []string{"b"}},
		}}},
		{"empty-deliverables", TotalsBatch{Totals: []TotalSpec{{ID: "T1"}}}},
		{"missing-dep", TotalsBatch{Totals: []TotalSpec{
			{ID: "T1", Deliverables: []string{"x"}, DependsOn: []string{"Tghost"}},
		}}},
		{"cycle", TotalsBatch{Totals: []TotalSpec{
			{ID: "T1", Deliverables: []string{"x"}, DependsOn: []string{"T2"}},
			{ID: "T2", Deliverables: []string{"y"}, DependsOn: []string{"T1"}},
		}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := json.Marshal(c.batch)
			_, err := authorizeTotalsHandler()(context.Background(),
				[]cpn.Token{{Color: cpn.ColorJSON, Payload: string(b)}})
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestGateTotalHandler_PairsByTotalID(t *testing.T) {
	totals := TotalsBatch{
		SpecName: "echo-tool",
		Totals: []TotalSpec{
			{ID: "T1", Deliverables: []string{"a"}},
			{ID: "T2", Deliverables: []string{"b"}},
		},
	}
	doit := DoItBatch{Entries: []DoIt{
		{TotalID: "T1", Approved: true},
		{TotalID: "T2", Approved: false},
	}}
	tb, _ := json.Marshal(totals)
	db, _ := json.Marshal(doit)

	out, err := gateTotalHandler()(context.Background(), []cpn.Token{
		{Color: cpn.ColorJSON, Payload: string(db)},
		{Color: cpn.ColorJSON, Payload: string(tb)},
	})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	var ready TotalReadyBatch
	_ = decodeAs(out[PlaceTotalReady].Payload, &ready)
	if len(ready.Entries) != 1 {
		t.Fatalf("ready entries = %d, want 1 (T2 should be skipped)", len(ready.Entries))
	}
	if ready.Entries[0].Total.ID != "T1" {
		t.Errorf("expected T1 in ready, got %s", ready.Entries[0].Total.ID)
	}
}

func TestFanoutReviewers_ProducesClonesForEveryReviewer(t *testing.T) {
	draft := SpecDraft{Name: "echo-tool", Purpose: "prints args"}
	b, _ := json.Marshal(draft)
	out, err := fanoutReviewersHandler()(context.Background(),
		[]cpn.Token{{Color: cpn.ColorJSON, Payload: string(b)}})
	if err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if len(out) != len(reviewerRoles) {
		t.Errorf("clones = %d, want %d", len(out), len(reviewerRoles))
	}
	for _, r := range reviewerRoles {
		if _, ok := out[r.InputPlace]; !ok {
			t.Errorf("missing clone for %s", r.InputPlace)
		}
	}
}

func TestReviewerRoles_StableOrder(t *testing.T) {
	got := ReviewerRoles()
	want := []string{ProfileGoEng, ProfileDevOps, ProfileArch}
	if len(got) != len(want) {
		t.Fatalf("len got=%d want=%d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got=%s want=%s", i, got[i], want[i])
		}
	}
}

func TestFlowName(t *testing.T) {
	if FlowName != "tool-creator" {
		t.Errorf("FlowName = %q, want tool-creator", FlowName)
	}
}

func TestFinalizeInstallHandler_HappyPath(t *testing.T) {
	result := cpn.ShellResultPayload{
		ExitCode:   0,
		Stdout:     "building...\ninstalled: /home/brae/workspace/tools/bin/echo-tool\n",
		DurationMs: 42,
	}
	b, _ := json.Marshal(result)
	h := finalizeInstallHandler()
	out, err := h(context.Background(), []cpn.Token{{Color: cpn.ColorShellResult, Payload: string(b)}})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	tok, ok := out[PlaceRegistered]
	if !ok {
		t.Fatal("missing PlaceRegistered")
	}
	if tok.Color != cpn.ColorArtifact {
		t.Errorf("color=%s, want ColorArtifact", tok.Color)
	}
	var manifest map[string]any
	_ = json.Unmarshal([]byte(tok.Payload.(string)), &manifest)
	if got, want := manifest["binary_path"], "/home/brae/workspace/tools/bin/echo-tool"; got != want {
		t.Errorf("binary_path=%v, want %q", got, want)
	}
}

func TestFinalizeInstallHandler_NonZeroExitFails(t *testing.T) {
	result := cpn.ShellResultPayload{ExitCode: 1, Stderr: "boom"}
	b, _ := json.Marshal(result)
	h := finalizeInstallHandler()
	if _, err := h(context.Background(), []cpn.Token{{Color: cpn.ColorShellResult, Payload: string(b)}}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}
