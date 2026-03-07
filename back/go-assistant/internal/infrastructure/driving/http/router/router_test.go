package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appservice "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/service"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/handler"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/pkg/response"
)

// mockHealthRepo implements output.HealthRepository for router testing.
type mockHealthRepo struct{}

func (m *mockHealthRepo) CheckHealth(_ context.Context) error { return nil }

func TestNew(t *testing.T) {
	repo := &mockHealthRepo{}
	svc := appservice.NewHealthService(repo, "1.0.0-test")
	h := handler.NewHealthHandler(svc)

	e := New(h, "go-assistant-test")

	if e == nil {
		t.Fatal("New returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestNew_HealthEndpointResponse(t *testing.T) {
	repo := &mockHealthRepo{}
	svc := appservice.NewHealthService(repo, "1.0.0-test")
	h := handler.NewHealthHandler(svc)

	e := New(h, "go-assistant-test")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body response.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if body.Status != "UP" {
		t.Errorf("expected status UP, got %q", body.Status)
	}

	if body.Version == "" {
		t.Error("expected version to be set")
	}

	if body.Checks["database"] != "UP" {
		t.Errorf("expected database check UP, got %q", body.Checks["database"])
	}
}
