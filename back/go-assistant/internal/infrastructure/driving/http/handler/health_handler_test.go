package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/pkg/response"
)

// mockHealthService implements input.HealthService for testing.
type mockHealthService struct {
	health *entity.Health
	err    error
}

func (m *mockHealthService) GetHealth(_ context.Context) (*entity.Health, error) {
	return m.health, m.err
}

func TestHealthHandler_GetHealth(t *testing.T) {
	tests := []struct {
		name           string
		mockHealth     *entity.Health
		mockErr        error
		expectedCode   int
		expectedStatus string
		expectVersion  bool
	}{
		{
			name: "healthy service returns 200 with full body",
			mockHealth: &entity.Health{
				Status:  valueobject.StatusUp,
				Version: "1.0.0",
				Checks:  map[string]valueobject.Status{"database": valueobject.StatusUp},
			},
			mockErr:        nil,
			expectedCode:   http.StatusOK,
			expectedStatus: "UP",
			expectVersion:  true,
		},
		{
			name: "database DOWN returns 503 with DOWN status",
			mockHealth: &entity.Health{
				Status:  valueobject.StatusDown,
				Version: "1.0.0",
				Checks:  map[string]valueobject.Status{"database": valueobject.StatusDown},
			},
			mockErr:        nil,
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "DOWN",
			expectVersion:  true,
		},
		{
			name:           "service error returns 503",
			mockHealth:     nil,
			mockErr:        errors.New("internal failure"),
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "DOWN",
			expectVersion:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			svc := &mockHealthService{health: tt.mockHealth, err: tt.mockErr}
			h := NewHealthHandler(svc)

			if err := h.GetHealth(c); err != nil {
				t.Fatalf("GetHealth returned unexpected error: %v", err)
			}

			if rec.Code != tt.expectedCode {
				t.Errorf("HTTP status = %d, want %d", rec.Code, tt.expectedCode)
			}

			var body response.HealthResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to unmarshal response body: %v", err)
			}

			if body.Status != tt.expectedStatus {
				t.Errorf("body.Status = %q, want %q", body.Status, tt.expectedStatus)
			}

			if tt.expectVersion && body.Version == "" {
				t.Error("expected Version to be set, got empty string")
			}

			if !tt.expectVersion && body.Version != "" {
				t.Errorf("expected empty Version, got %q", body.Version)
			}

			if tt.mockErr == nil {
				if body.Checks == nil {
					t.Error("expected Checks map to be present in response")
				}
				if body.Timestamp == "" {
					t.Error("expected Timestamp to be set in response")
				}
			}
		})
	}
}
