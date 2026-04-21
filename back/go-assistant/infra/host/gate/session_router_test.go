package gate

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// newTestCPN builds a minimal CPN wired with an EventEmitter so Publish
// can fan its events out to the test assertion. No transitions are
// needed — the router only consults CPN.emit via the public helpers.
func newTestCPN(bufferSize int) (*cpn.CPN, chan cpn.Event) {
	ch := make(chan cpn.Event, bufferSize)
	c := cpn.NewCPN(
		"cpn-test",
		"test",
		0,
		cpn.ModeMAS,
		"sess-1",
		map[string]*cpn.Place{},
		map[string]*cpn.Transition{},
	)
	c.EventEmitter = ch
	return c, ch
}

func TestSessionHITLRouter_Publish_EmitsSurfaceAndRequested(t *testing.T) {
	c, events := newTestCPN(4)
	router := &SessionHITLRouter{
		CPN:          c,
		TransitionID: "t-bash",
	}

	err := router.Publish(context.Background(), HostApprovalPrompt{
		Operation: "exec",
		Command:   "/bin/sh -c uname",
		RiskBand:  RiskCaution,
		Rationale: "first-run unknown",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := drainEvents(t, events, 2)
	if got[0].Type != cpn.EventStreamChunk {
		t.Fatalf("event 0 type = %s, want %s", got[0].Type, cpn.EventStreamChunk)
	}
	chunk, ok := got[0].Payload.(cpn.StreamChunk)
	if !ok {
		t.Fatalf("event 0 payload type = %T, want cpn.StreamChunk", got[0].Payload)
	}
	if !contains(chunk.Content, cpn.A2UIMarker) {
		t.Fatalf("surface chunk missing A2UI marker: %q", chunk.Content)
	}
	if !contains(chunk.Content, `"schema":"host.approval"`) {
		t.Fatalf("surface chunk missing host.approval schema: %q", chunk.Content)
	}

	if got[1].Type != cpn.EventHITLRequested {
		t.Fatalf("event 1 type = %s, want %s", got[1].Type, cpn.EventHITLRequested)
	}
	payload, ok := got[1].Payload.(cpn.HITLRequestedPayload)
	if !ok {
		t.Fatalf("event 1 payload type = %T, want cpn.HITLRequestedPayload", got[1].Payload)
	}
	if !payload.CustomSurface {
		t.Fatalf("HITLRequestedPayload.CustomSurface = false, want true")
	}
}

func TestSessionHITLRouter_AwaitResponse_DecodesExtendedAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		action  cpn.HITLAction
		want    string
	}{
		{"approve-once", `{"action":"approve-once"}`, cpn.HITLApprove, ActionApproveOnce},
		{"approve-and-remember", `{"action":"approve-and-remember"}`, cpn.HITLApprove, ActionApproveAndRemember},
		{"deny", `{"action":"deny"}`, cpn.HITLReject, ActionDeny},
		{"deny-and-blacklist", `{"action":"deny-and-blacklist"}`, cpn.HITLReject, ActionDenyAndBlacklist},
		{"empty-content-approve", "", cpn.HITLApprove, ActionApproveOnce},
		{"empty-content-reject", "", cpn.HITLReject, ActionDeny},
		{"garbage-json-approve", "not-json", cpn.HITLApprove, ActionApproveOnce},
		{"garbage-json-reject", "not-json", cpn.HITLReject, ActionDeny},
		{"empty-object", "{}", cpn.HITLApprove, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inject := make(chan cpn.Token, 1)
			router := &SessionHITLRouter{
				CPN:          mustCPN(t),
				TransitionID: "t-bash",
				Inject:       inject,
			}
			inject <- cpn.Token{
				Color:   cpn.ColorHuman,
				Payload: cpn.HITLResponse{Action: tc.action, Content: tc.content},
			}

			resp, err := router.AwaitResponse(context.Background())
			if err != nil {
				t.Fatalf("AwaitResponse: %v", err)
			}
			if resp.Action != tc.want {
				t.Fatalf("Action = %q, want %q", resp.Action, tc.want)
			}
			if resp.RespondedAt.IsZero() {
				t.Fatalf("RespondedAt not set")
			}
		})
	}
}

func TestSessionHITLRouter_AwaitResponse_NonHITLResponsePayload(t *testing.T) {
	inject := make(chan cpn.Token, 1)
	router := &SessionHITLRouter{
		CPN:          mustCPN(t),
		TransitionID: "t-bash",
		Inject:       inject,
	}
	inject <- cpn.Token{Color: cpn.ColorHuman, Payload: "raw-string"}

	resp, err := router.AwaitResponse(context.Background())
	if err != nil {
		t.Fatalf("AwaitResponse: %v", err)
	}
	if resp.Action != "" {
		t.Fatalf("Action = %q, want empty (deny)", resp.Action)
	}
}

func TestSessionHITLRouter_AwaitResponse_ContextCancelled(t *testing.T) {
	inject := make(chan cpn.Token)
	router := &SessionHITLRouter{
		CPN:          mustCPN(t),
		TransitionID: "t-bash",
		Inject:       inject,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := router.AwaitResponse(ctx)
	if err == nil {
		t.Fatalf("expected ctx error, got nil")
	}
	if resp.Action != "" {
		t.Fatalf("Action on cancel = %q, want empty", resp.Action)
	}
}

func TestSessionHITLRouter_AwaitResponse_NilInject_WaitsForCtx(t *testing.T) {
	router := &SessionHITLRouter{
		CPN:          mustCPN(t),
		TransitionID: "t-bash",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := router.AwaitResponse(ctx)
	if err == nil {
		t.Fatalf("expected ctx err, got nil")
	}
}

func TestSessionHITLRouter_AwaitResponse_ClosedChannel(t *testing.T) {
	inject := make(chan cpn.Token)
	close(inject)
	router := &SessionHITLRouter{
		CPN:          mustCPN(t),
		TransitionID: "t-bash",
		Inject:       inject,
	}
	_, err := router.AwaitResponse(context.Background())
	if err == nil {
		t.Fatalf("expected closed-channel error")
	}
}

func TestSessionHITLRouter_Publish_MissingCPN(t *testing.T) {
	router := &SessionHITLRouter{TransitionID: "t"}
	if err := router.Publish(context.Background(), HostApprovalPrompt{}); err == nil {
		t.Fatalf("expected error when CPN is nil")
	}
}

func TestSessionHITLRouter_Publish_MissingTransitionID(t *testing.T) {
	c, _ := newTestCPN(1)
	router := &SessionHITLRouter{CPN: c}
	if err := router.Publish(context.Background(), HostApprovalPrompt{}); err == nil {
		t.Fatalf("expected error when TransitionID is empty")
	}
}

func mustCPN(t *testing.T) *cpn.CPN {
	t.Helper()
	c, _ := newTestCPN(4)
	return c
}

func drainEvents(t *testing.T, ch <-chan cpn.Event, n int) []cpn.Event {
	t.Helper()
	out := make([]cpn.Event, 0, n)
	for i := range n {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timed out waiting for event %d/%d", i+1, n)
		}
	}
	return out
}

func contains(s, sub string) bool {
	return len(sub) == 0 || indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
