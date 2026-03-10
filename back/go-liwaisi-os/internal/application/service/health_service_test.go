package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	outputmocks "github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/port/output/mocks"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
)

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

			repoMock := outputmocks.NewHealthRepository(t)
			repoMock.On("CheckHealth", mock.Anything).Return(tt.repoResult)

			svc := NewHealthService(repoMock, "1.0.0")

			health, err := svc.GetHealth(context.Background())

			assert.NoError(t, err)
			assert.NotNil(t, health)
			assert.Equal(t, tt.expectedStatus, health.Status)
			assert.Equal(t, tt.expectedCheck, health.Checks["system"])
			assert.Equal(t, "1.0.0", health.Version)
		})
	}
}
