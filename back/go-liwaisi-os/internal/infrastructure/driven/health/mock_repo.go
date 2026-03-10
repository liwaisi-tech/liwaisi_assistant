package health

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/port/output"
)

// mockRepo is a placeholder implementation of HealthRepository for go-liwaisi-os.
// Standard health checks here might verify OS capabilities or system binaries later.
type mockRepo struct{}

// NewMockRepository returns a stub Repository that consistently passes.
func NewMockRepository() output.HealthRepository {
	return &mockRepo{}
}

func (m *mockRepo) CheckHealth(ctx context.Context) error {
	// For now, always healthy as we just test if the API responds
	return nil
}
