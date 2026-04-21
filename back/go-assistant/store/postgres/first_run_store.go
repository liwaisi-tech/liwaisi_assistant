package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// FirstRunStore persists the first-run ledger (GAP-6).
type FirstRunStore struct {
	pool pgxPool
}

var _ persist.FirstRunRepository = (*FirstRunStore)(nil)

// NewFirstRunStore constructs a FirstRunStore bound to pool.
func NewFirstRunStore(pool pgxPool) *FirstRunStore {
	return &FirstRunStore{pool: pool}
}

// Record idempotently inserts (host_id, sha256). Returns the current row
// (existing or freshly inserted).
func (s *FirstRunStore) Record(ctx context.Context, hostID, binaryPath, sha256 string) (*persist.FirstRunLedgerEntry, error) {
	if hostID == "" || sha256 == "" {
		return nil, fmt.Errorf("first_run: host_id and sha256 required")
	}
	const q = `
		INSERT INTO first_run_ledger (host_id, binary_path, binary_sha256)
		VALUES ($1, $2, $3)
		ON CONFLICT (host_id, binary_sha256) DO UPDATE
		  SET binary_path = EXCLUDED.binary_path
		RETURNING id, host_id, binary_path, binary_sha256, first_seen,
		          first_approved_by, first_approved_at, revoked
	`
	row := s.pool.QueryRow(ctx, q, hostID, binaryPath, sha256)
	return scanFirstRunEntry(row)
}

// GetBySHA returns the ledger entry for a (host, sha). Returns (nil, nil)
// when the row is absent.
func (s *FirstRunStore) GetBySHA(ctx context.Context, hostID, sha256 string) (*persist.FirstRunLedgerEntry, error) {
	const q = `
		SELECT id, host_id, binary_path, binary_sha256, first_seen,
		       first_approved_by, first_approved_at, revoked
		FROM first_run_ledger
		WHERE host_id = $1 AND binary_sha256 = $2
	`
	row := s.pool.QueryRow(ctx, q, hostID, sha256)
	entry, err := scanFirstRunEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return entry, nil
}

// Approve stamps the row as approved.
func (s *FirstRunStore) Approve(ctx context.Context, hostID, sha256, userID string) error {
	const q = `
		UPDATE first_run_ledger
		SET first_approved_by = $3,
		    first_approved_at = NOW(),
		    revoked = FALSE
		WHERE host_id = $1 AND binary_sha256 = $2
	`
	tag, err := s.pool.Exec(ctx, q, hostID, sha256, userID)
	if err != nil {
		return fmt.Errorf("first_run: approve: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("first_run: approve: row not found for sha=%s", sha256)
	}
	return nil
}

// Revoke flips the row to revoked.
func (s *FirstRunStore) Revoke(ctx context.Context, hostID, sha256 string) error {
	const q = `UPDATE first_run_ledger SET revoked = TRUE WHERE host_id = $1 AND binary_sha256 = $2`
	tag, err := s.pool.Exec(ctx, q, hostID, sha256)
	if err != nil {
		return fmt.Errorf("first_run: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("first_run: revoke: row not found for sha=%s", sha256)
	}
	return nil
}

// List returns up to limit rows ordered by first_seen DESC.
func (s *FirstRunStore) List(ctx context.Context, limit int) ([]*persist.FirstRunLedgerEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT id, host_id, binary_path, binary_sha256, first_seen,
		       first_approved_by, first_approved_at, revoked
		FROM first_run_ledger
		ORDER BY first_seen DESC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("first_run: list: %w", err)
	}
	defer rows.Close()
	var out []*persist.FirstRunLedgerEntry
	for rows.Next() {
		entry, err := scanFirstRunEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

type firstRunScanner interface {
	Scan(dest ...any) error
}

func scanFirstRunEntry(row firstRunScanner) (*persist.FirstRunLedgerEntry, error) {
	entry := &persist.FirstRunLedgerEntry{}
	var approvedBy *string
	var approvedAt *time.Time
	err := row.Scan(
		&entry.ID,
		&entry.HostID,
		&entry.BinaryPath,
		&entry.BinarySHA256,
		&entry.FirstSeen,
		&approvedBy,
		&approvedAt,
		&entry.Revoked,
	)
	if err != nil {
		return nil, err
	}
	if approvedBy != nil {
		entry.FirstApprovedBy = *approvedBy
	}
	if approvedAt != nil {
		entry.FirstApprovedAt = approvedAt
	}
	return entry, nil
}
