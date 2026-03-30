package app

import (
	"context"
	"log/slog"
	"os"
	"testing"

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
	if !isWrapping(err, ErrInvalidInput) {
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
	if !isWrapping(err, ErrSessionNotFound) {
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
	if !isWrapping(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
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
	if !isWrapping(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
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
	if !isWrapping(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

// isWrapping checks if err's message contains target's message.
// Used instead of errors.Is because we use fmt.Errorf with %w wrapping.
func isWrapping(err, target error) bool {
	if err == nil {
		return false
	}
	// errors.Is traverses the chain.
	return containsError(err, target)
}

func containsError(err, target error) bool {
	// Use standard library unwrap chain.
	for e := err; e != nil; {
		if e.Error() == target.Error() {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	// Fallback: check if the target error text is contained in err.
	return contains(err.Error(), target.Error())
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
