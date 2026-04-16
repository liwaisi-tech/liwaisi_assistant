package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// ── Test Mocks ─────────────────────────────────────────────────────────────

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

// ── Test Helpers ───────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

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

func testHandlers() *Handlers {
	logger := testLogger()
	svc := app.NewSessionService(
		&mockLLMClient{},
		&mockCostProvider{cost: 0.0},
		logger,
		testTopologyFactory,
	)
	broker := NewSSEBroker(logger)
	return &Handlers{App: svc, Broker: broker}
}

func testMux(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sessions", h.HandleCreateSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.HandleGetSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.HandleDeleteSession)
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages", h.HandleSendMessage)
	mux.HandleFunc("POST /api/v1/sessions/{id}/hitl/{transitionID}", h.HandleResolveHITL)
	return mux
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
	return &buf
}

// ── Session Tests ──────────────────────────────────────────────────────────

func TestHandleCreateSession(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	body := jsonBody(t, CreateSessionRequest{UserID: "user-1", Channel: "web"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if resp.UserID != "user-1" {
		t.Errorf("expected user_id 'user-1', got %q", resp.UserID)
	}
	if resp.Channel != "web" {
		t.Errorf("expected channel 'web', got %q", resp.Channel)
	}
	if resp.State != "idle" {
		t.Errorf("expected state 'idle', got %q", resp.State)
	}
}

func TestHandleCreateSession_InvalidBody(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	body := bytes.NewBufferString(`{invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateSession_MissingFields(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	body := jsonBody(t, CreateSessionRequest{UserID: "", Channel: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetSession(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	// Create a session first.
	createBody := jsonBody(t, CreateSessionRequest{UserID: "user-2", Channel: "web"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created SessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Get the session.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var resp SessionDetailResponse
	if err := json.NewDecoder(getRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, resp.ID)
	}
	if resp.Messages == nil {
		t.Error("expected messages array (possibly empty), got nil")
	}
}

func TestHandleGetSession_NotFound(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteSession(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	// Create a session first.
	createBody := jsonBody(t, CreateSessionRequest{UserID: "user-del", Channel: "web"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created SessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Delete the session.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+created.ID, nil)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}

	var resp StatusResponse
	if err := json.NewDecoder(delRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "deleted" {
		t.Errorf("expected status 'deleted', got %q", resp.Status)
	}

	// Verify session is gone.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getRec.Code)
	}
}

func TestHandleDeleteSession_NotFound(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendMessage(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	// Create a session first.
	createBody := jsonBody(t, CreateSessionRequest{UserID: "user-3", Channel: "web"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created SessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Send a message.
	msgBody := jsonBody(t, SendMessageRequest{Content: "hello"})
	msgReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+created.ID+"/messages", msgBody)
	msgReq.Header.Set("Content-Type", "application/json")
	msgRec := httptest.NewRecorder()
	mux.ServeHTTP(msgRec, msgReq)

	if msgRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", msgRec.Code, msgRec.Body.String())
	}

	var resp StatusResponse
	if err := json.NewDecoder(msgRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "accepted" {
		t.Errorf("expected status 'accepted', got %q", resp.Status)
	}
}

func TestHandleSendMessage_NotFound(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	msgBody := jsonBody(t, SendMessageRequest{Content: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent/messages", msgBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeriveClientSessionState locks in the rehydration-oriented state
// vocabulary returned by GET /api/v1/sessions/{id} per REQ-404 of
// spec-process-bugfix-a2ui-rehydration-completion.md. The frontend reducer
// depends on these exact strings (idle | running | hitl_pending | terminal).
func TestDeriveClientSessionState(t *testing.T) {
	t.Parallel()

	a2uiMsg := cpn.Message{Role: cpn.RoleAssistant, Content: cpn.A2UIMarker + `{"components":[]}`}
	plainMsg := cpn.Message{Role: cpn.RoleAssistant, Content: "Hola, ¿cómo estás?"}
	userMsg := cpn.Message{Role: cpn.RoleUser, Content: "Necesito ayuda"}

	cases := []struct {
		name     string
		state    cpn.State
		messages []cpn.Message
		want     string
	}{
		{"idle_empty", cpn.StateIdle, nil, "idle"},
		{"running_no_messages", cpn.StateRunning, nil, "running"},
		{"running_with_user_msg", cpn.StateRunning, []cpn.Message{userMsg}, "running"},
		{"waiting_a2ui_pending", cpn.StateWaiting, []cpn.Message{userMsg, a2uiMsg}, "hitl_pending"},
		{"waiting_no_a2ui_falls_back_to_running", cpn.StateWaiting, []cpn.Message{userMsg, plainMsg}, "running"},
		{"waiting_user_only", cpn.StateWaiting, []cpn.Message{userMsg}, "running"},
		{"completed_terminal", cpn.StateCompleted, []cpn.Message{userMsg, plainMsg}, "terminal"},
		{"failed_terminal", cpn.StateFailed, []cpn.Message{userMsg}, "terminal"},
		{"a2ui_with_leading_whitespace_recognized", cpn.StateWaiting, []cpn.Message{
			userMsg, {Role: cpn.RoleAssistant, Content: "  \n" + cpn.A2UIMarker + `{"components":[]}`},
		}, "hitl_pending"},
		{"non_assistant_a2ui_ignored", cpn.StateWaiting, []cpn.Message{
			{Role: cpn.RoleUser, Content: cpn.A2UIMarker + `{"components":[]}`},
		}, "running"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveClientSessionState(tc.state, tc.messages)
			if got != tc.want {
				t.Errorf("deriveClientSessionState(%v, …) = %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

func TestHandleSendMessage_EmptyContent(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	// Create a session first.
	createBody := jsonBody(t, CreateSessionRequest{UserID: "user-4", Channel: "web"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created SessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Send empty message.
	msgBody := jsonBody(t, SendMessageRequest{Content: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+created.ID+"/messages", msgBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
