package health

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/port/output"
)

// systemRepo is an implementation of HealthRepository for go-liwaisi-os.
// Standard health checks here might verify OS capabilities or system binaries later.
type systemRepo struct{}

// NewSystemRepository returns a Repository that checks the system health.
func NewSystemRepository() output.HealthRepository {
	return &systemRepo{}
}

func (s *systemRepo) CheckHealth(ctx context.Context) error {
	// For now, always healthy as we just test if the API responds
	return nil
}
