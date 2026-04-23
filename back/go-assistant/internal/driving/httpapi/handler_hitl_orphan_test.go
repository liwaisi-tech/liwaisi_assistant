package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// stubSessionPort lets HTTP tests inject arbitrary ResolveHITL error returns
// without standing up a full SessionService + in-memory CPN.
type stubSessionPort struct {
	info             *app.SessionInfo
	resolveHITLError error
}

func (s *stubSessionPort) CreateSession(context.Context, string, cpn.ChannelType) (*app.SessionInfo, error) {
	return s.info, nil
}
func (s *stubSessionPort) CreateSessionWithFactory(context.Context, string, cpn.ChannelType, app.TopologyFactory) (*app.SessionInfo, error) {
	return s.info, nil
}
func (s *stubSessionPort) GetSession(string) (*app.SessionInfo, error) { return s.info, nil }
func (s *stubSessionPort) UpdateSession(context.Context, string, *string, bool) error {
	return nil
}
func (s *stubSessionPort) DeleteSession(string) error { return nil }
func (s *stubSessionPort) ListSessions(context.Context, string, *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error) {
	return nil, nil
}
func (s *stubSessionPort) ForkSession(context.Context, string, string, cpn.ChannelType, int) (*app.SessionInfo, error) {
	return s.info, nil
}
func (s *stubSessionPort) SendMessage(context.Context, string, string) error { return nil }
func (s *stubSessionPort) StreamChannel(string) (<-chan cpn.StreamChunk, error) {
	return nil, nil
}
func (s *stubSessionPort) SendStreamChunk(string, cpn.StreamChunk) error { return nil }
func (s *stubSessionPort) CloseStream(string) error                      { return nil }
func (s *stubSessionPort) ResolveHITL(context.Context, string, string, cpn.HITLResponse) error {
	return s.resolveHITLError
}
func (s *stubSessionPort) SetEventCallback(func(string, cpn.Event)) {}

// unused-but-required to keep tools import live if the interface changes.
var _ tools.ToolSchema

// TestHandleResolveHITL_OrphanTransition_Returns409WithEnvelope asserts the
// §4.3 JSON envelope and status code for REQ-006 / AC-004.
func TestHandleResolveHITL_OrphanTransition_Returns409WithEnvelope(t *testing.T) {
	t.Parallel()

	stub := &stubSessionPort{
		info: &app.SessionInfo{
			ID: "sess-1", UserID: "user-1", Channel: cpn.ChannelWeb,
			State: cpn.StateRunning, CreatedAt: time.Now().UTC(),
		},
		resolveHITLError: fmt.Errorf("%w: transition %q on session %s: %w",
			app.ErrTransitionOrphaned, "t-stale", "sess-1", cpn.ErrNoHITLWaiting),
	}
	h := &Handlers{App: stub, Logger: testLogger(), Broker: NewSSEBroker(testLogger())}
	mux := testMux(h)

	body := jsonBody(t, ResolveHITLRequest{Action: string(cpn.HITLApprove)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/hitl/t-stale", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}

	// Decode into the exact spec shape; raw map also works but struct
	// assertion locks the field names (§4.3 is load-bearing for the frontend).
	var body409 struct {
		Error struct {
			Code         string `json:"code"`
			Message      string `json:"message"`
			TransitionID string `json:"transitionId"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body409); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body409.Error.Code != "HITL_TRANSITION_ORPHANED" {
		t.Errorf("code = %q, want HITL_TRANSITION_ORPHANED", body409.Error.Code)
	}
	if body409.Error.Message != "Esta aprobación ya fue resuelta" {
		t.Errorf("message = %q, want 'Esta aprobación ya fue resuelta'", body409.Error.Message)
	}
	if body409.Error.TransitionID != "t-stale" {
		t.Errorf("transitionId = %q, want t-stale", body409.Error.TransitionID)
	}
}

// TestHandleResolveHITL_InactiveSession_Returns409InactiveEnvelope pins the
// unchanged contract for dead/idle sessions: 409 with {"error":"session inactive"}.
func TestHandleResolveHITL_InactiveSession_Returns409InactiveEnvelope(t *testing.T) {
	t.Parallel()

	stub := &stubSessionPort{
		info: &app.SessionInfo{
			ID: "sess-2", UserID: "user-1", Channel: cpn.ChannelWeb,
			State: cpn.StateIdle, CreatedAt: time.Now().UTC(),
		},
		resolveHITLError: fmt.Errorf("%w: %s", app.ErrSessionInactive, "sess-2"),
	}
	h := &Handlers{App: stub, Logger: testLogger(), Broker: NewSSEBroker(testLogger())}
	mux := testMux(h)

	body := jsonBody(t, ResolveHITLRequest{Action: string(cpn.HITLApprove)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-2/hitl/t-any", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The inactive envelope is the legacy flat shape, NOT the structured
	// orphan envelope — the two must remain distinguishable for the
	// frontend toast routing.
	if envelope["error"] != "session inactive" {
		t.Errorf("envelope = %+v, want flat string error=session inactive", envelope)
	}
	if _, ok := envelope["error"].(map[string]any); ok {
		t.Errorf("inactive envelope must not use the nested orphan shape")
	}
}
