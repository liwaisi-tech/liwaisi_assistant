package service

import (
	"context"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// mockHealthRepository implements output.HealthRepository for testing.
type mockHealthRepository struct {
	err error
}

func (m *mockHealthRepository) CheckHealth(_ context.Context) error {
	return m.err
}

func TestHealthService_GetHealth(t *testing.T) {
	tests := []struct {
		name           string
		repoErr        error
		ctx            context.Context
		expectedStatus valueobject.Status
		expectedDBChk  valueobject.Status
	}{
		{
			name:           "database UP returns healthy status",
			repoErr:        nil,
			ctx:            context.Background(),
			expectedStatus: valueobject.StatusUp,
			expectedDBChk:  valueobject.StatusUp,
		},
		{
			name:           "database DOWN returns unhealthy status",
			repoErr:        errors.New("connection refused"),
			ctx:            context.Background(),
			expectedStatus: valueobject.StatusDown,
			expectedDBChk:  valueobject.StatusDown,
		},
		{
			name:           "canceled context with healthy repo still returns result",
			repoErr:        nil,
			ctx:            canceledContext(),
			expectedStatus: valueobject.StatusUp,
			expectedDBChk:  valueobject.StatusUp,
		},
		{
			name:           "canceled context with repo error returns DOWN",
			repoErr:        context.Canceled,
			ctx:            canceledContext(),
			expectedStatus: valueobject.StatusDown,
			expectedDBChk:  valueobject.StatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockHealthRepository{err: tt.repoErr}
			svc := NewHealthService(repo, "1.0.0-test")

			health, err := svc.GetHealth(tt.ctx)
			if err != nil {
				t.Fatalf("GetHealth returned unexpected error: %v", err)
			}

			if health.Status != tt.expectedStatus {
				t.Errorf("Status = %q, want %q", health.Status, tt.expectedStatus)
			}

			if health.Version != "1.0.0-test" {
				t.Errorf("Version = %q, want %q", health.Version, "1.0.0-test")
			}

			if health.Timestamp.IsZero() {
				t.Error("Timestamp should not be zero")
			}

			dbCheck, ok := health.Checks["database"]
			if !ok {
				t.Fatal("Checks map missing 'database' key")
			}
			if dbCheck != tt.expectedDBChk {
				t.Errorf("Checks[database] = %q, want %q", dbCheck, tt.expectedDBChk)
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
