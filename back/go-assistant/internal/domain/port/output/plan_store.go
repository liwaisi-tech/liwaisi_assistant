package output

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

// PlanStore defines the output port for persisting and retrieving
// PlanDocuments. Implementations may use the filesystem, a database,
// or any other storage backend.
type PlanStore interface {
	// Save persists a PlanDocument. If a document with the same SessionID
	// already exists, it is overwritten.
	Save(ctx context.Context, doc *entity.PlanDocument) error

	// Load retrieves a PlanDocument by its SessionID.
	// Returns an error if the document does not exist.
	Load(ctx context.Context, sessionID string) (*entity.PlanDocument, error)

	// List returns all persisted PlanDocuments, ordered by creation time
	// (most recent first).
	List(ctx context.Context) ([]*entity.PlanDocument, error)

	// GetActive returns the currently active PlanDocument, or nil if none.
	GetActive(ctx context.Context) (*entity.PlanDocument, error)
}
