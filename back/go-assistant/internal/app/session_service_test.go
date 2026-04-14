package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Mocks ───────────────────────────────────────────────────────────────────

type mockLLMClient struct {
	completeFunc func(ctx context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error)
}

func (m *mockLLMClient) Complete(ctx context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return cpn.LLMResponse{Content: "ok"}, nil
}

func (m *mockLLMClient) CompleteStream(ctx context.Context, req *cpn.LLMRequest, _ func(string)) (cpn.LLMResponse, error) {
	return m.Complete(ctx, req)
}

func (m *mockLLMClient) EstimateCost(_ *cpn.LLMRequest) (float64, error) {
	return 0.001, nil
}

type mockCostProvider struct{ cost float64 }

func (m *mockCostProvider) SessionCostUSD(_ string) float64 { return m.cost }

// ── Helpers ─────────────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// testTopologyFactory creates a minimal CPN: p-input → t-llm → p-output.
func testTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorString, cpn.SpaceSurface),
	}
	transitions := map[string]*cpn.Transition{
		"t-llm": cpn.NewTransition("t-llm", cpn.NodeKindLLM,
			[]string{"p-input"}, []string{"p-output"}),
	}
	return cpn.NewCPN("cpn-"+sessionID, "test-root", 0, cpn.ModeMAS, sessionID, places, transitions)
}

func newTestService() *SessionService {
	return NewSessionService(
		&mockLLMClient{},
		&mockCostProvider{cost: 0.0},
		testLogger(),
		testTopologyFactory,
	)
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestCreateSession(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-1", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if len(info.ID) != 32 {
		t.Errorf("expected 32-char hex ID, got %d chars: %s", len(info.ID), info.ID)
	}
	if info.UserID != "user-1" {
		t.Errorf("expected userID user-1, got %s", info.UserID)
	}
	if info.Channel != cpn.ChannelWeb {
		t.Errorf("expected channel web, got %s", info.Channel)
	}
	if info.State != cpn.StateIdle {
		t.Errorf("expected state idle, got %s", info.State)
	}
}

func TestCreateSession_InvalidInput(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	_, err := svc.CreateSession(context.Background(), "", cpn.ChannelWeb)
	if err == nil {
		t.Fatal("expected error for empty userID")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	created, err := svc.CreateSession(context.Background(), "user-2", cpn.ChannelWhatsApp)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	info, err := svc.GetSession(created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, info.ID)
	}
	if info.UserID != "user-2" {
		t.Errorf("expected userID user-2, got %s", info.UserID)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	_, err := svc.GetSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-3", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = svc.SendMessage(context.Background(), info.ID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Verify message was appended.
	got, err := svc.GetSession(info.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", got.Messages[0].Content)
	}
	if got.Messages[0].Role != cpn.RoleUser {
		t.Errorf("expected role user, got %s", got.Messages[0].Role)
	}
}

func TestSendMessage_SessionNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	err := svc.SendMessage(context.Background(), "nonexistent", "hello")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestCloseStream_DoubleClose(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-dc", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.CloseStream(info.ID); err != nil {
		t.Fatalf("first CloseStream: %v", err)
	}

	// Second call must not panic and should return nil.
	if err := svc.CloseStream(info.ID); err != nil {
		t.Fatalf("second CloseStream should return nil, got: %v", err)
	}
}

func TestSendMessage_SessionBusy(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-busy", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// First message starts CPN.
	err = svc.SendMessage(context.Background(), info.ID, "first")
	if err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Force state to Running to simulate in-flight CPN.
	svc.mu.RLock()
	st := svc.states[info.ID]
	svc.mu.RUnlock()
	st.set(cpn.StateRunning)

	err = svc.SendMessage(context.Background(), info.ID, "second")
	if err == nil {
		t.Fatal("expected ErrSessionBusy for second message")
	}
	if !errors.Is(err, ErrSessionBusy) {
		t.Errorf("expected ErrSessionBusy, got: %v", err)
	}
}

func TestSendMessage_SessionBusyWhenWaiting(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-wait", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Force state to Waiting to simulate HITL pause.
	svc.mu.RLock()
	st := svc.states[info.ID]
	svc.mu.RUnlock()
	st.set(cpn.StateWaiting)

	err = svc.SendMessage(context.Background(), info.ID, "second")
	if err == nil {
		t.Fatal("expected ErrSessionBusy when state is Waiting")
	}
	if !errors.Is(err, ErrSessionBusy) {
		t.Errorf("expected ErrSessionBusy, got: %v", err)
	}
}

func TestCancelSession(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-cancel", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = svc.SendMessage(context.Background(), info.ID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// CancelSession should not panic even if CPN already finished.
	svc.CancelSession(info.ID)

	// Cancel nonexistent session should not panic.
	svc.CancelSession("nonexistent")
}

func TestResolveHITL_SessionNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	err := svc.ResolveHITL(context.Background(), "nonexistent", "t-hitl", cpn.HITLResponse{
		Action: cpn.HITLApprove,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-del", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = svc.DeleteSession(info.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Session must no longer be accessible.
	_, err = svc.GetSession(info.ID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound after delete, got: %v", err)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	err := svc.DeleteSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestDeleteSession_CancelsRunning(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-del-run", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Force state to Running and set a cancel func to verify it gets called.
	svc.mu.RLock()
	st := svc.states[info.ID]
	svc.mu.RUnlock()

	canceled := make(chan struct{})
	st.set(cpn.StateRunning)
	st.mu.Lock()
	st.cancel = func() { close(canceled) }
	st.mu.Unlock()

	// Delete while CPN is "running" — should cancel and not panic.
	err = svc.DeleteSession(info.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify cancel was called.
	select {
	case <-canceled:
		// ok
	default:
		t.Error("expected cancel to be called on delete")
	}

	// Verify session is gone.
	_, err = svc.GetSession(info.ID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound after delete, got: %v", err)
	}
}

func TestStreamChannel(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	info, err := svc.CreateSession(context.Background(), "user-4", cpn.ChannelTelegram)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ch, err := svc.StreamChannel(info.ID)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestStreamChannel_SessionNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	_, err := svc.StreamChannel("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

// hitlA2UITopologyFactory builds a minimal session topology with a single HITL
// transition whose A2UIPayloadBuilder emits a questionnaire-shaped payload.
// Used to exercise the fireHITL → c.History → session.Messages persistence
// pipeline end-to-end via SessionService.SendMessage + ResolveHITL, proving
// INV-002 (single persistence site) and INV-004 (no t-ask raw JSON rows).
func hitlA2UITopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorHuman, cpn.SpaceSurface),
	}
	hitl := cpn.NewTransition("t-hitl", cpn.NodeKindHITL,
		[]string{"p-input"}, []string{"p-output"})
	hitl.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please answer",
		A2UIPayloadBuilder: func(consumed []cpn.Token) (any, error) {
			return map[string]any{
				"components": []any{
					map[string]any{
						"type": "questionnaire",
						"props": map[string]any{
							"componentId": "t-hitl",
						},
					},
				},
			}, nil
		},
	}
	transitions := map[string]*cpn.Transition{"t-hitl": hitl}
	return cpn.NewCPN("cpn-"+sessionID, "test-root", 0, cpn.ModeMAS, sessionID, places, transitions)
}

// TestSessionService_HITLClarifyFlow_PersistsOnlyA2UIRow_NotTAskRaw asserts
// INV-002 and INV-004 end-to-end: after SendMessage triggers fireHITL (which
// emits the A2UI surface) and ResolveHITL unblocks the transition, the
// session's persisted messages contain exactly ONE assistant row starting
// with "$$a2ui:" and zero rows whose CPNID is "t-ask" (there is no t-ask
// transition in this topology; REQ-016 is enforced separately in the
// topology test).
func TestSessionService_HITLClarifyFlow_PersistsOnlyA2UIRow_NotTAskRaw(t *testing.T) {
	t.Parallel()
	svc := NewSessionService(
		&mockLLMClient{},
		&mockCostProvider{cost: 0.0},
		testLogger(),
		hitlA2UITopologyFactory,
	)

	// Register an event callback that signals when the HITL transition
	// has emitted EventHITLRequested (fireHITL has already taken the
	// mu.Lock path to append the A2UI row by then, so subsequent
	// assertions are race-free).
	hitlReady := make(chan struct{}, 1)
	svc.SetEventCallback(func(sessionID string, evt cpn.Event) {
		if evt.Type == cpn.EventHITLRequested {
			select {
			case hitlReady <- struct{}{}:
			default:
			}
		}
	})

	info, err := svc.CreateSession(context.Background(), "user-hitl-a2ui", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := svc.SendMessage(context.Background(), info.ID, "hola"); err != nil {
		t.Fatalf("send message: %v", err)
	}

	select {
	case <-hitlReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventHITLRequested")
	}

	// Resolve the HITL with an approve action.
	if err := svc.ResolveHITL(context.Background(), info.ID, "t-hitl", cpn.HITLResponse{
		Action: cpn.HITLApprove,
	}); err != nil {
		t.Fatalf("resolve HITL: %v", err)
	}

	// Wait for the CPN goroutine to drain and sync history.
	svc.mu.RLock()
	st := svc.states[info.ID]
	svc.mu.RUnlock()
	deadline := time.Now().Add(2 * time.Second)
	for st.get() == cpn.StateRunning || st.get() == cpn.StateWaiting {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for CPN to finish, state=%s", st.get())
		}
		time.Sleep(5 * time.Millisecond)
	}

	got, err := svc.GetSession(info.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	// INV-002 + AC-011: exactly one assistant row starts with the A2UI marker.
	a2uiRows := 0
	tAskRows := 0
	for _, m := range got.Messages {
		if m.Role == cpn.RoleAssistant && strings.HasPrefix(m.Content, cpn.A2UIMarker) {
			a2uiRows++
		}
		if m.CPNID == "t-ask" {
			tAskRows++
		}
	}
	if a2uiRows != 1 {
		t.Errorf("expected exactly 1 A2UI row (INV-002), got %d\nmessages: %+v", a2uiRows, got.Messages)
	}
	// INV-004: no t-ask raw JSON rows in the persisted history.
	if tAskRows != 0 {
		t.Errorf("expected zero t-ask rows (INV-004), got %d", tAskRows)
	}
}
