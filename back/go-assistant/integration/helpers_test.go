//go:build integration

package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/httpapi"
)

// testEnv holds the running server state shared across all tests.
type testEnv struct {
	baseURL    string
	httpClient *http.Client
	server     *httpapi.Server
	listener   net.Listener
	appService *app.SessionService
}

var env *testEnv

func TestMain(m *testing.M) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Println("SKIP: OPENROUTER_API_KEY not set — skipping integration tests")
		os.Exit(0)
	}

	// INTEGRATION_MODEL: opt-in override for integration tests only. Lets
	// the CI pipeline point tests at a cheaper model without reintroducing
	// the removed DEFAULT_MODEL env in the production surface
	// (REQ-CFG-002). Defaults to a known-working minimax build.
	model := os.Getenv("INTEGRATION_MODEL")
	if model == "" {
		model = "minimax/minimax-m2.7"
	}

	logLevel := slog.LevelError
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Build driven adapters
	llmClient := openrouter.NewClient(apiKey, model)
	costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

	// Build application layer
	appService := app.NewSessionService(llmClient, costProvider, logger, defaultTopologyFactory)

	// Build HTTP server
	cfg := httpapi.ServerConfig{
		Addr:            "127.0.0.1:0",
		ReadTimeout:     10 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		AllowedOrigins:  []string{"*"},
	}
	srv := httpapi.NewServer(cfg, appService, logger, nil)

	// Wire event callback
	appService.SetEventCallback(func(sessionID string, evt cpn.Event) {
		srv.Broker().PublishEvent(sessionID, &evt)
	})

	// Start on random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: listen: %v\n", err)
		os.Exit(1)
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "FATAL: serve: %v\n", err)
		}
	}()

	env = &testEnv{
		baseURL:    "http://" + ln.Addr().String(),
		httpClient: &http.Client{Timeout: 90 * time.Second},
		server:     srv,
		listener:   ln,
		appService: appService,
	}

	fmt.Printf("Integration test server running at %s\n", env.baseURL)
	fmt.Printf("Model: %s\n\n", model)

	code := m.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	os.Exit(code)
}

// ── Topology + Adapter (replicate from cmd/server/main.go) ──────────────────

type ledgerCostAdapter struct {
	ledger *openrouter.TokenLedger
}

func (a *ledgerCostAdapter) SessionCostUSD(sessionID string) float64 {
	rec := a.ledger.Get(sessionID)
	if rec == nil {
		return 0
	}
	return rec.TotalCostUSD
}

func defaultTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}
	tLLM := cpn.NewTransition("t-llm", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-output"})
	tLLM.SystemPrompt = "You are a helpful assistant. Be concise."
	tLLM.LLMConfig = &cpn.LLMConfig{
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	transitions := map[string]*cpn.Transition{
		"t-llm": tLLM,
	}
	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant", 0, cpn.ModeMAS, sessionID,
		places, transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// ── HTTP Helpers ────────────────────────────────────────────────────────────

// doRequest performs an HTTP request and returns the response. Does NOT close the body.
func doRequest(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			bodyReader = strings.NewReader(v)
		case []byte:
			bodyReader = bytes.NewReader(v)
		default:
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request body: %v", err)
			}
			bodyReader = bytes.NewReader(b)
		}
	}

	req, err := http.NewRequest(method, env.baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := env.httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// doRequestWithHeader performs an HTTP request with custom headers.
func doRequestWithHeader(t *testing.T, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := env.httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

type sessionResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Channel   string `json:"channel"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

type sessionDetailResponse struct {
	sessionResponse
	Messages []messageResponse `json:"messages"`
}

type messageResponse struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CPNID     string `json:"cpn_id"`
	Timestamp string `json:"timestamp"`
}

type statusResp struct {
	Status string `json:"status"`
}

type errorResp struct {
	Error string `json:"error"`
}

type versionResp struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

// createSession creates a session and returns the parsed response.
func createSession(t *testing.T, userID, channel string) sessionResponse {
	t.Helper()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", map[string]string{
		"user_id": userID,
		"channel": channel,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create session: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var s sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode create session: %v", err)
	}
	return s
}

// getSession returns the session detail response.
func getSession(t *testing.T, sessionID string) sessionDetailResponse {
	t.Helper()
	resp := doRequest(t, http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("get session: expected 200, got %d: %s", resp.StatusCode, body)
	}
	var s sessionDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode get session: %v", err)
	}
	return s
}

// sendMessage sends a message and returns the raw response.
func sendMessage(t *testing.T, sessionID, content string) *http.Response {
	t.Helper()
	return doRequest(t, http.MethodPost, "/api/v1/sessions/"+sessionID+"/messages", map[string]string{
		"content": content,
	})
}

// pollSessionUntil polls GET /session/{id} until state matches or timeout.
func pollSessionUntil(t *testing.T, sessionID string, wantStates []string, timeout time.Duration) sessionDetailResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	want := make(map[string]bool, len(wantStates))
	for _, s := range wantStates {
		want[s] = true
	}

	for time.Now().Before(deadline) {
		s := getSession(t, sessionID)
		if want[s.State] {
			return s
		}
		time.Sleep(500 * time.Millisecond)
	}
	// One final check
	s := getSession(t, sessionID)
	if want[s.State] {
		return s
	}
	t.Fatalf("session %s: state=%q, want one of %v (timeout %s)", sessionID, s.State, wantStates, timeout)
	return sessionDetailResponse{} // unreachable
}

// ── SSE Helpers ─────────────────────────────────────────────────────────────

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	ID    string
	Event string
	Data  string
}

// sseReader reads and parses SSE events from an HTTP response body.
type sseReader struct {
	scanner *bufio.Scanner
	body    io.ReadCloser
}

func newSSEReader(body io.ReadCloser) *sseReader {
	return &sseReader{
		scanner: bufio.NewScanner(body),
		body:    body,
	}
}

// ReadRawLine reads one raw line (for checking retry directive, comments).
func (r *sseReader) ReadRawLine(timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if r.scanner.Scan() {
			ch <- result{line: r.scanner.Text()}
		} else {
			ch <- result{err: r.scanner.Err()}
		}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		return res.line, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout reading SSE line after %s", timeout)
	}
}

// NextEvent reads the next complete SSE event (blocks until event or timeout).
// Also detects heartbeat comments and returns them as Event="heartbeat".
func (r *sseReader) NextEvent(timeout time.Duration) (*sseEvent, error) {
	type result struct {
		evt *sseEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var evt sseEvent
		for r.scanner.Scan() {
			line := r.scanner.Text()

			// SSE comment (heartbeat)
			if strings.HasPrefix(line, ": ") {
				comment := strings.TrimPrefix(line, ": ")
				ch <- result{evt: &sseEvent{Event: "heartbeat", Data: comment}}
				return
			}

			if line == "" {
				// End of event block
				if evt.Event != "" || evt.Data != "" {
					ch <- result{evt: &evt}
					return
				}
				continue
			}

			if strings.HasPrefix(line, "id: ") {
				evt.ID = strings.TrimPrefix(line, "id: ")
			} else if strings.HasPrefix(line, "event: ") {
				evt.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				evt.Data = strings.TrimPrefix(line, "data: ")
			} else if strings.HasPrefix(line, "retry: ") {
				// retry directive — return as special event
				ch <- result{evt: &sseEvent{Event: "retry", Data: strings.TrimPrefix(line, "retry: ")}}
				return
			}
		}
		if err := r.scanner.Err(); err != nil {
			ch <- result{err: err}
		} else {
			ch <- result{err: io.EOF}
		}
	}()

	select {
	case res := <-ch:
		return res.evt, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for SSE event after %s", timeout)
	}
}

func (r *sseReader) Close() {
	r.body.Close()
}

// connectSSE opens an SSE connection to /api/v1/sessions/{id}/events.
// Returns the response and an SSE reader. Caller must close the reader.
func connectSSE(t *testing.T, sessionID string) (*http.Response, *sseReader) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.baseURL+"/api/v1/sessions/"+sessionID+"/events", nil)
	if err != nil {
		t.Fatalf("new SSE request: %v", err)
	}

	// Use a client with no timeout for SSE (long-lived)
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}

	return resp, newSSEReader(resp.Body)
}

// assertStatus checks the HTTP status code.
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", want, resp.StatusCode, body)
	}
}

// decodeJSONResp decodes the response body into dst.
func decodeJSONResp(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
