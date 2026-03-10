package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/pkg/response"
)

// mockHealthService implements input.HealthService for testing.
type mockHealthService struct {
	getHealthFunc func(ctx context.Context) (*entity.Health, error)
}

func (m *mockHealthService) GetHealth(ctx context.Context) (*entity.Health, error) {
	if m.getHealthFunc != nil {
		return m.getHealthFunc(ctx)
	}
	return nil, nil
}

func TestHealthHandler_GetHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mockSvcResult  *entity.Health
		mockSvcErr     error
		expectedStatus int
		expectedBody   response.HealthResponse
	}{
		{
			name: "successful health check",
			mockSvcResult: &entity.Health{
				Status:    valueobject.StatusUp,
				Version:   "1.0.0",
				Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Checks: map[string]valueobject.Status{
					"system": valueobject.StatusUp,
				},
			},
			mockSvcErr:     nil,
			expectedStatus: http.StatusOK,
			expectedBody: response.HealthResponse{
				Status:    "UP",
				Version:   "1.0.0",
				Timestamp: "2025-01-01T00:00:00Z",
				Checks: map[string]string{
					"system": "UP",
				},
			},
		},
		{
			name: "service unavailable due to unhealthy status",
			mockSvcResult: &entity.Health{
				Status:    valueobject.StatusDown,
				Version:   "1.0.0",
				Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Checks: map[string]valueobject.Status{
					"system": valueobject.StatusDown,
				},
			},
			mockSvcErr:     nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody: response.HealthResponse{
				Status:    "DOWN",
				Version:   "1.0.0",
				Timestamp: "2025-01-01T00:00:00Z",
				Checks: map[string]string{
					"system": "DOWN",
				},
			},
		},
		{
			name:           "service error",
			mockSvcResult:  nil,
			mockSvcErr:     errors.New("internal error"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody: response.HealthResponse{
				Status: "DOWN",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			svc := &mockHealthService{
				getHealthFunc: func(ctx context.Context) (*entity.Health, error) {
					return tt.mockSvcResult, tt.mockSvcErr
				},
			}

			h := NewHealthHandler(svc)
			if err := h.GetHealth(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			var body response.HealthResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if body.Status != tt.expectedBody.Status {
				t.Errorf("expected status %s, got %s", tt.expectedBody.Status, body.Status)
			}

			if tt.expectedBody.Version != "" && body.Version != tt.expectedBody.Version {
				t.Errorf("expected version %s, got %s", tt.expectedBody.Version, body.Version)
			}

			if tt.expectedBody.Timestamp != "" && body.Timestamp != tt.expectedBody.Timestamp {
				t.Errorf("expected timestamp %s, got %s", tt.expectedBody.Timestamp, body.Timestamp)
			}

			if tt.expectedBody.Checks != nil {
				for k, v := range tt.expectedBody.Checks {
					if body.Checks[k] != v {
						t.Errorf("expected check %s to be %s, got %s", k, v, body.Checks[k])
					}
				}
			}
		})
	}
}
