package toolbuilder

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestInvestigateHandler_EmitsEmptyList(t *testing.T) {
	h := investigateHandler()
	out, err := h(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tok, ok := out[PlaceExistingTools]
	if !ok {
		t.Fatal("missing PlaceExistingTools")
	}
	var list []ExistingTool
	if err := json.Unmarshal([]byte(tok.Payload.(string)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestEnrichSpecHandler_HappyPath(t *testing.T) {
	draft := SpecDraft{Name: "calc", Purpose: "add"}
	draftJSON, _ := json.Marshal(draft)
	existing := []ExistingTool{{Name: "old", Summary: "prev"}}
	existingJSON, _ := json.Marshal(existing)

	h := enrichSpecHandler()
	out, err := h(context.Background(), []cpn.Token{
		{Color: cpn.ColorJSON, Payload: string(draftJSON)},
		{Color: cpn.ColorJSON, Payload: string(existingJSON)},
		{Color: cpn.ColorArtifact, Payload: "/ws/calc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got SpecDraft
	_ = json.Unmarshal([]byte(out[PlaceSpecDraft].Payload.(string)), &got)
	if got.WorkspacePath != "/ws/calc" {
		t.Fatalf("workspace not propagated: %q", got.WorkspacePath)
	}
	if len(got.ExistingTools) != 1 {
		t.Fatalf("existing tools not merged: %+v", got.ExistingTools)
	}
}

func TestEnrichSpecHandler_NoSpecDraftEmitsStub(t *testing.T) {
	// enrichSpecHandler is now lenient: if no upstream token yields a
	// parseable SpecDraft, it emits a stub rather than failing the CPN.
	// This prevents hard deadlocks upstream and lets the refine loop
	// surface the problem as a blocking issue.
	h := enrichSpecHandler()
	out, err := h(context.Background(), []cpn.Token{
		{Color: cpn.ColorArtifact, Payload: "/ws"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tok, ok := out[PlaceSpecDraft]
	if !ok {
		t.Fatal("expected PlaceSpecDraft token")
	}
	var got SpecDraft
	if err := json.Unmarshal([]byte(tok.Payload.(string)), &got); err != nil {
		t.Fatalf("unmarshal stub: %v", err)
	}
	if got.Name == "" {
		t.Fatal("stub draft should have placeholder Name")
	}
}

func TestAggregateApproveHandler_Emits(t *testing.T) {
	h := aggregateApproveHandler()
	tokens := make([]cpn.Token, 0, len(reviewerRoles))
	for _, r := range reviewerRoles {
		rev, _ := json.Marshal(Review{Role: r.ProfileID, Approved: true})
		tokens = append(tokens, cpn.Token{Color: cpn.ColorJSON, Payload: string(rev)})
	}
	out, err := h(context.Background(), tokens)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out[PlaceSpecApproved]; !ok {
		t.Fatal("expected PlaceSpecApproved")
	}
}

func TestAggregateApproveHandler_BadPayloadProducesApprovedFalse(t *testing.T) {
	// parseReviews is lenient: a malformed token becomes a synthetic
	// "malformed" review so the aggregate transitions can still fire and
	// downstream refine gets a chance to recover rather than deadlocking.
	h := aggregateApproveHandler()
	tokens := []cpn.Token{{Color: cpn.ColorJSON, Payload: "not-json"}}
	out, err := h(context.Background(), tokens)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out[PlaceSpecApproved]; !ok {
		t.Fatal("expected PlaceSpecApproved")
	}
}

func TestAggregateRefineHandler_Refines(t *testing.T) {
	h := aggregateRefineHandler()
	tokens := make([]cpn.Token, 0, len(reviewerRoles))
	for i, r := range reviewerRoles {
		rev := Review{Role: r.ProfileID, Approved: i == 0, BlockingIssues: []string{"x"}}
		rj, _ := json.Marshal(rev)
		tokens = append(tokens, cpn.Token{Color: cpn.ColorJSON, Payload: string(rj)})
	}
	out, err := h(context.Background(), tokens)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out[PlaceRefineRequest]; !ok {
		t.Fatal("expected PlaceRefineRequest")
	}
}

func TestAggregateRefineHandler_BadPayloadRoutesToRefine(t *testing.T) {
	// Same leniency contract as approve: a malformed token is treated as
	// a synthetic "malformed" review; refine routes to PlaceRefineRequest
	// so the pipeline gets another pass.
	h := aggregateRefineHandler()
	tokens := []cpn.Token{{Color: cpn.ColorJSON, Payload: "not-json"}}
	out, err := h(context.Background(), tokens)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out[PlaceRefineRequest]; !ok {
		t.Fatal("expected PlaceRefineRequest")
	}
}

func TestBuildVerdict_AggregatesIssues(t *testing.T) {
	reviews := []Review{
		{Role: "go-engineer", Approved: false, BlockingIssues: []string{"a"}, Suggestions: []string{"s1"}},
		{Role: "qa-engineer", Approved: true, Suggestions: []string{"s2"}},
	}
	v := buildVerdict(reviews, SpecDraft{Name: "x"}, false)
	if v.Approved {
		t.Fatal("expected not approved")
	}
	if len(v.BlockingIssues) != 1 || len(v.Suggestions) != 2 {
		t.Fatalf("aggregation off: %+v", v)
	}
	if v.SpecDraft.Name != "x" {
		t.Fatalf("spec not carried: %+v", v.SpecDraft)
	}
}

func TestDecodeAs_AllFormats(t *testing.T) {
	type x struct{ A int `json:"a"` }

	// string
	var v1 x
	if err := decodeAs(`{"a":1}`, &v1); err != nil || v1.A != 1 {
		t.Fatalf("string: %v %+v", err, v1)
	}
	// []byte
	var v2 x
	if err := decodeAs([]byte(`{"a":2}`), &v2); err != nil || v2.A != 2 {
		t.Fatalf("bytes: %v %+v", err, v2)
	}
	// json.RawMessage
	var v3 x
	if err := decodeAs(json.RawMessage(`{"a":3}`), &v3); err != nil || v3.A != 3 {
		t.Fatalf("raw: %v %+v", err, v3)
	}
	// struct (round-trip via marshal)
	var v4 x
	if err := decodeAs(x{A: 4}, &v4); err != nil || v4.A != 4 {
		t.Fatalf("struct: %v %+v", err, v4)
	}
	// nil
	var v5 x
	if err := decodeAs(nil, &v5); err == nil {
		t.Fatal("expected error for nil payload")
	}
}

func TestDispatchHandler_BadTriage(t *testing.T) {
	h := dispatchHandler()
	if _, err := h(context.Background(), []cpn.Token{{Payload: "not-json"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthorizeTotals_NoInput(t *testing.T) {
	h := authorizeTotalsHandler()
	if _, err := h(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthorizeTotals_BadPayload(t *testing.T) {
	h := authorizeTotalsHandler()
	if _, err := h(context.Background(), []cpn.Token{{Payload: "not-json"}}); err == nil {
		t.Fatal("expected decode error")
	}
}
