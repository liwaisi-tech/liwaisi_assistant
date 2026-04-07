package a2a

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// noopLogger returns a logger that discards all output.
func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAgentCardEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	logger := noopLogger()

	card := BuildAgentCard(Config{BaseURL: "https://test.example.com"}, nil, nil, "0.1.0-test")
	executor := &BRAEExecutor{mapper: NewMapper(), logger: logger}

	RegisterHandlers(mux, executor, card, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}

	var gotCard AgentCard
	if err := json.Unmarshal(rec.Body.Bytes(), &gotCard); err != nil {
		t.Fatalf("unmarshal agent card: %v", err)
	}

	if gotCard.Name != "BRAE" {
		t.Errorf("name = %q, want %q", gotCard.Name, "BRAE")
	}
	if gotCard.Version != "0.1.0-test" {
		t.Errorf("version = %q, want %q", gotCard.Version, "0.1.0-test")
	}
	if !gotCard.Capabilities.Streaming {
		t.Error("expected streaming capability to be true")
	}
	if len(gotCard.Skills) != 3 {
		t.Errorf("skills len = %d, want 3", len(gotCard.Skills))
	}
	if gotCard.SupportedInterfaces[0].URL != "https://test.example.com/a2a" {
		t.Errorf("interface URL = %q, want %q", gotCard.SupportedInterfaces[0].URL, "https://test.example.com/a2a")
	}
}

func TestJSONRPCDispatch_MethodNotFound(t *testing.T) {
	mux := http.NewServeMux()
	logger := noopLogger()

	card := BuildAgentCard(Config{BaseURL: "http://localhost"}, nil, nil, "0.1.0")
	executor := &BRAEExecutor{mapper: NewMapper(), logger: logger}

	RegisterHandlers(mux, executor, card, nil, logger)

	body := `{"jsonrpc":"2.0","id":1,"method":"unknown.method","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestJSONRPCDispatch_InvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	logger := noopLogger()

	card := BuildAgentCard(Config{BaseURL: "http://localhost"}, nil, nil, "0.1.0")
	executor := &BRAEExecutor{mapper: NewMapper(), logger: logger}

	RegisterHandlers(mux, executor, card, nil, logger)

	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != CodeParseError {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeParseError)
	}
}

func TestJSONRPCDispatch_InvalidVersion(t *testing.T) {
	mux := http.NewServeMux()
	logger := noopLogger()

	card := BuildAgentCard(Config{BaseURL: "http://localhost"}, nil, nil, "0.1.0")
	executor := &BRAEExecutor{mapper: NewMapper(), logger: logger}

	RegisterHandlers(mux, executor, card, nil, logger)

	body := `{"jsonrpc":"1.0","id":1,"method":"tasks/get","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeInvalidRequest)
	}
}

func TestJSONRPCDispatch_InvalidParams(t *testing.T) {
	mux := http.NewServeMux()
	logger := noopLogger()

	card := BuildAgentCard(Config{BaseURL: "http://localhost"}, nil, nil, "0.1.0")
	executor := &BRAEExecutor{mapper: NewMapper(), logger: logger}

	RegisterHandlers(mux, executor, card, nil, logger)

	// Send tasks/get with invalid params (string instead of object).
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestBuildAgentCard_WithPersonality(t *testing.T) {
	personality := &stubPersonality{name: "Custom Agent", desc: "Custom description"}
	card := BuildAgentCard(Config{BaseURL: "https://custom.example.com"}, personality, nil, "1.0.0")

	if card.Name != "Custom Agent" {
		t.Errorf("name = %q, want %q", card.Name, "Custom Agent")
	}
	if card.Description != "Custom description" {
		t.Errorf("description = %q, want %q", card.Description, "Custom description")
	}
}

func TestBuildAgentCard_WithTools(t *testing.T) {
	tools := &stubToolProvider{
		tools: []ToolInfo{
			{Name: "calculator", Namespace: "math", Description: "Performs calculations"},
		},
	}
	card := BuildAgentCard(Config{BaseURL: "https://test.example.com"}, nil, tools, "1.0.0")

	// 3 topology skills + 1 tool skill = 4.
	if len(card.Skills) != 4 {
		t.Fatalf("skills len = %d, want 4", len(card.Skills))
	}
	toolSkill := card.Skills[3]
	if toolSkill.ID != "math/calculator" {
		t.Errorf("tool skill ID = %q, want %q", toolSkill.ID, "math/calculator")
	}
}

func TestConfigFromEnv(t *testing.T) {
	// Default config (no env vars set).
	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Error("expected disabled by default")
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want %q", cfg.BaseURL, "http://localhost:8080")
	}
}

func TestConfig_IsEnabled(t *testing.T) {
	cfg := Config{Enabled: true}
	if !cfg.IsEnabled() {
		t.Error("expected enabled")
	}

	cfg.Enabled = false
	if cfg.IsEnabled() {
		t.Error("expected disabled")
	}
}

// ── Test Stubs ─────────────────────────────────────────────────────────────

type stubPersonality struct {
	name string
	desc string
}

func (s *stubPersonality) Name() string        { return s.name }
func (s *stubPersonality) Description() string { return s.desc }

type stubToolProvider struct {
	tools []ToolInfo
}

func (s *stubToolProvider) Tools() []ToolInfo { return s.tools }
