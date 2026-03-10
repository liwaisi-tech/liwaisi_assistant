package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/entity"
	inputmocks "github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/port/input/mocks"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/pkg/response"
)

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

			svcMock := inputmocks.NewHealthService(t)
			svcMock.On("GetHealth", mock.Anything).Return(tt.mockSvcResult, tt.mockSvcErr)

			h := NewHealthHandler(svcMock)
			err := h.GetHealth(c)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			var body response.HealthResponse
			err = json.Unmarshal(rec.Body.Bytes(), &body)
			assert.NoError(t, err)

			assert.Equal(t, tt.expectedBody.Status, body.Status)
			
			if tt.expectedBody.Version != "" {
				assert.Equal(t, tt.expectedBody.Version, body.Version)
			}
			if tt.expectedBody.Timestamp != "" {
				assert.Equal(t, tt.expectedBody.Timestamp, body.Timestamp)
			}
			if tt.expectedBody.Checks != nil {
				for k, v := range tt.expectedBody.Checks {
					assert.Equal(t, v, body.Checks[k])
				}
			}
		})
	}
}

