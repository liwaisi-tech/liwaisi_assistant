package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// MutationStore implements persist.MutationRepository against a Postgres database.
type MutationStore struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.MutationRepository = (*MutationStore)(nil)

// NewMutationStore creates a new MutationStore backed by the given pool.
func NewMutationStore(pool pgxPool) *MutationStore {
	return &MutationStore{pool: pool}
}

// Insert persists a mutation record. If r.ID is empty, a random hex ID is generated.
func (s *MutationStore) Insert(ctx context.Context, r *persist.MutationRecord) error {
	if r.ID == "" {
		var b [16]byte
		_, _ = rand.Read(b[:])
		r.ID = hex.EncodeToString(b[:])
	}
	if r.AppliedAt.IsZero() {
		r.AppliedAt = time.Now()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cpn_mutations
         (id, cpn_id, session_id, mutation_kind, requested_by, reason, approved, rejected_reason, applied_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		r.ID, r.CPNID, r.SessionID, r.MutationKind, r.RequestedBy, r.Reason,
		r.Approved, r.RejectedReason, r.AppliedAt,
	)
	if err != nil {
		return fmt.Errorf("cpn_mutations insert: %w", err)
	}
	return nil
}

// ListByCPN returns mutation records for the given CPN ID, newest first.
func (s *MutationStore) ListByCPN(ctx context.Context, cpnID string) ([]*persist.MutationRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, cpn_id, session_id, mutation_kind, requested_by, reason,
                approved, rejected_reason, applied_at
         FROM cpn_mutations
         WHERE cpn_id = $1
         ORDER BY applied_at DESC`,
		cpnID,
	)
	if err != nil {
		return nil, fmt.Errorf("cpn_mutations list: %w", err)
	}
	defer rows.Close()

	var records []*persist.MutationRecord
	for rows.Next() {
		r := &persist.MutationRecord{}
		if err := rows.Scan(&r.ID, &r.CPNID, &r.SessionID, &r.MutationKind, &r.RequestedBy,
			&r.Reason, &r.Approved, &r.RejectedReason, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("cpn_mutations scan: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
