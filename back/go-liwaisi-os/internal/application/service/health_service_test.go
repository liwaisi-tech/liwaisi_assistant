package service

import (
	"context"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
)

// mockHealthRepository implements output.HealthRepository for testing.
type mockHealthRepository struct {
	checkHealthFunc func(ctx context.Context) error
}

func (m *mockHealthRepository) CheckHealth(ctx context.Context) error {
	if m.checkHealthFunc != nil {
		return m.checkHealthFunc(ctx)
	}
	return nil
}

func TestHealthService_GetHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		repoResult     error
		expectedStatus valueobject.Status
		expectedCheck  valueobject.Status
	}{
		{
			name:           "healthy system",
			repoResult:     nil,
			expectedStatus: valueobject.StatusUp,
			expectedCheck:  valueobject.StatusUp,
		},
		{
			name:           "unhealthy system",
			repoResult:     errors.New("mock error"),
			expectedStatus: valueobject.StatusDown,
			expectedCheck:  valueobject.StatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockHealthRepository{
				checkHealthFunc: func(ctx context.Context) error {
					return tt.repoResult
				},
			}

			svc := NewHealthService(repo, "1.0.0")

			health, err := svc.GetHealth(context.Background())
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if health.Status != tt.expectedStatus {
				t.Errorf("expected status %v, got %v", tt.expectedStatus, health.Status)
			}

			if checkStatus, ok := health.Checks["system"]; !ok || checkStatus != tt.expectedCheck {
				t.Errorf("expected system check status %v, got %v", tt.expectedCheck, checkStatus)
			}

			if health.Version != "1.0.0" {
				t.Errorf("expected version 1.0.0, got %v", health.Version)
			}
		})
	}
}
