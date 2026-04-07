package postgres

import (
	"context"
	"fmt"
)

// AuditRepository persists admin config mutations to admin_audit_log.
type AuditRepository struct {
	pool pgxPool
}

// NewAuditRepository creates an audit log repository backed by pgx.
func NewAuditRepository(pool pgxPool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// LogConfigChange records a single admin config mutation. The secret value
// is never persisted; only the key, action, and the user that made the change.
func (r *AuditRepository) LogConfigChange(ctx context.Context, key, action, updatedBy string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO admin_audit_log (key, action, updated_by) VALUES ($1, $2, $3)`,
		key, action, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("postgres audit log %s/%s: %w", action, key, err)
	}
	return nil
}
