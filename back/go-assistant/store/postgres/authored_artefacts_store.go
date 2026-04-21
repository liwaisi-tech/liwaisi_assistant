package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// AuthoredArtefactStore is the Postgres-backed implementation of
// persist.AuthoredArtefactLedger (GAP-10). It is metadata-only; filesystem
// side-effects belong to persist.RollbackService / PurgeService.
type AuthoredArtefactStore struct {
	pool pgxPool
}

var _ persist.AuthoredArtefactLedger = (*AuthoredArtefactStore)(nil)

// NewAuthoredArtefactStore constructs the adapter.
func NewAuthoredArtefactStore(pool pgxPool) *AuthoredArtefactStore {
	return &AuthoredArtefactStore{pool: pool}
}

const artefactSelectColumns = `
	id, path, classification, set_id, forge_run_id, host_id,
	flow_hash, authoring_cpn_id, transition_id, session_id,
	sha256, size_bytes, mime, mode, state,
	created_at, quarantined_at, restored_at, purged_at, quarantine_path
`

func scanArtefact(row interface {
	Scan(dest ...any) error
}) (persist.Artefact, error) {
	var (
		a              persist.Artefact
		quarantinedAt  *time.Time
		restoredAt     *time.Time
		purgedAt       *time.Time
		classification string
		state          string
	)
	err := row.Scan(
		&a.ID, &a.Path, &classification, &a.SetID, &a.ForgeRunID, &a.HostID,
		&a.FlowHash, &a.AuthoringCPNID, &a.TransitionID, &a.SessionID,
		&a.SHA256, &a.Size, &a.MIME, &a.Mode, &state,
		&a.CreatedAt, &quarantinedAt, &restoredAt, &purgedAt, &a.QuarantinePath,
	)
	if err != nil {
		return persist.Artefact{}, err
	}
	a.Classification = persist.ArtefactClassification(classification)
	a.State = persist.ArtefactState(state)
	a.QuarantinedAt = quarantinedAt
	a.RestoredAt = restoredAt
	a.PurgedAt = purgedAt
	return a, nil
}

// PreWrite inserts a pending artefact row and returns its ID.
func (s *AuthoredArtefactStore) PreWrite(ctx context.Context, intent persist.WriteIntent, path string, mode uint32) (string, error) {
	if path == "" {
		return "", persist.ErrInvalidInput
	}
	setID := intent.SetID
	id := uuid.New().String()
	if setID == "" {
		setID = "singleton-" + id
	}
	classification := intent.Classification
	if classification == "" {
		classification = persist.ClassOther
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO authored_artefacts (
			id, path, classification, set_id, forge_run_id, host_id,
			flow_hash, authoring_cpn_id, transition_id, session_id,
			mode, state
		 ) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, 'pending'
		 )`,
		id, path, string(classification), setID, intent.ForgeRunID, intent.HostID,
		intent.FlowHash, intent.AuthoringCPNID, intent.TransitionID, intent.SessionID,
		int32(mode), //nolint:gosec // G115: POSIX file mode fits in int32; column type is bigint in schema
	)
	if err != nil {
		return "", fmt.Errorf("postgres artefact pre-write: %w", err)
	}
	return id, nil
}

// PostWrite records the hash, size and MIME and transitions state → active.
func (s *AuthoredArtefactStore) PostWrite(ctx context.Context, artefactID, sha256 string, size int64, mime string) error {
	if artefactID == "" {
		return persist.ErrInvalidInput
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE authored_artefacts
			SET sha256 = $2, size_bytes = $3, mime = $4, state = 'active'
		  WHERE id = $1 AND state = 'pending'`,
		artefactID, sha256, size, mime,
	)
	if err != nil {
		return fmt.Errorf("postgres artefact post-write: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrArtefactNotFound
	}
	return nil
}

// GetByID fetches a single artefact row.
func (s *AuthoredArtefactStore) GetByID(ctx context.Context, id string) (persist.Artefact, error) {
	if id == "" {
		return persist.Artefact{}, persist.ErrInvalidInput
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+artefactSelectColumns+` FROM authored_artefacts WHERE id = $1`,
		id,
	)
	a, err := scanArtefact(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return persist.Artefact{}, persist.ErrArtefactNotFound
		}
		return persist.Artefact{}, fmt.Errorf("postgres artefact get: %w", err)
	}
	return a, nil
}

// SetForForgeRun returns every artefact authored by a forge run.
func (s *AuthoredArtefactStore) SetForForgeRun(ctx context.Context, forgeRunID string) ([]persist.Artefact, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+artefactSelectColumns+` FROM authored_artefacts
			WHERE forge_run_id = $1
			ORDER BY created_at`,
		forgeRunID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres artefact set-for-forge: %w", err)
	}
	defer rows.Close()
	return collectArtefactRows(rows)
}

// SetForSet returns every artefact sharing a set_id.
func (s *AuthoredArtefactStore) SetForSet(ctx context.Context, setID string) ([]persist.Artefact, error) {
	if setID == "" {
		return nil, persist.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+artefactSelectColumns+` FROM authored_artefacts
			WHERE set_id = $1
			ORDER BY created_at`,
		setID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres artefact set-for-set: %w", err)
	}
	defer rows.Close()
	out, err := collectArtefactRows(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, persist.ErrArtefactSetEmpty
	}
	return out, nil
}

// Rollback flips every row in set_id to state="quarantined".
func (s *AuthoredArtefactStore) Rollback(ctx context.Context, setID, quarantinePath, actor string) error {
	if setID == "" {
		return persist.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres rollback begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // deferred rollback of already-committed tx is a no-op; error is unactionable

	tag, err := tx.Exec(ctx,
		`UPDATE authored_artefacts
			SET state = 'quarantined',
			    quarantined_at = COALESCE(quarantined_at, NOW()),
			    quarantine_path = $2
		  WHERE set_id = $1 AND state IN ('active', 'restored', 'pending')`,
		setID, quarantinePath,
	)
	if err != nil {
		return fmt.Errorf("postgres rollback update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Nothing to quarantine — either empty set or already quarantined.
		// Distinguish via a follow-up count.
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM authored_artefacts WHERE set_id = $1`,
			setID,
		).Scan(&n); err != nil {
			return fmt.Errorf("postgres rollback count: %w", err)
		}
		if n == 0 {
			return persist.ErrArtefactSetEmpty
		}
		return persist.ErrArtefactAlreadyRolledBack
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO artefact_events (set_id, kind, actor) VALUES ($1, 'rollback', $2)`,
		setID, actor,
	); err != nil {
		return fmt.Errorf("postgres rollback event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres rollback commit: %w", err)
	}
	return nil
}

// Restore flips every row in set_id back to state="restored".
func (s *AuthoredArtefactStore) Restore(ctx context.Context, setID, actor string) error {
	if setID == "" {
		return persist.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres restore begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // deferred rollback of already-committed tx is a no-op; error is unactionable

	tag, err := tx.Exec(ctx,
		`UPDATE authored_artefacts
			SET state = 'restored', restored_at = NOW()
		  WHERE set_id = $1 AND state = 'quarantined'`,
		setID,
	)
	if err != nil {
		return fmt.Errorf("postgres restore update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrArtefactNotQuarantined
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO artefact_events (set_id, kind, actor) VALUES ($1, 'restore', $2)`,
		setID, actor,
	); err != nil {
		return fmt.Errorf("postgres restore event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres restore commit: %w", err)
	}
	return nil
}

// PurgeExpired hard-deletes every quarantined row older than cutoff.
func (s *AuthoredArtefactStore) PurgeExpired(ctx context.Context, cutoff time.Time, actor string) ([]persist.Artefact, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres purge begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // deferred rollback of already-committed tx is a no-op; error is unactionable

	rows, err := tx.Query(ctx,
		`SELECT `+artefactSelectColumns+` FROM authored_artefacts
			WHERE state = 'quarantined' AND quarantined_at < $1`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres purge select: %w", err)
	}
	out, err := collectArtefactRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("postgres purge commit: %w", err)
		}
		return nil, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE authored_artefacts
			SET state = 'purged', purged_at = NOW()
		  WHERE state = 'quarantined' AND quarantined_at < $1`,
		cutoff,
	); err != nil {
		return nil, fmt.Errorf("postgres purge update: %w", err)
	}

	// One event per affected set.
	sets := make(map[string]struct{})
	for _, art := range out {
		sets[art.SetID] = struct{}{}
	}
	for setID := range sets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO artefact_events (set_id, kind, actor) VALUES ($1, 'purge', $2)`,
			setID, actor,
		); err != nil {
			return nil, fmt.Errorf("postgres purge event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres purge commit: %w", err)
	}
	// Re-fetch the now-purged rows so callers observe the new state/
	// purged_at timestamps.
	fresh := make([]persist.Artefact, 0, len(out))
	for _, art := range out {
		got, err := s.GetByID(ctx, art.ID)
		if err != nil {
			// Row may have been hard-deleted by an external actor between
			// the select and the re-fetch; keep the pre-update snapshot.
			fresh = append(fresh, art)
			continue
		}
		fresh = append(fresh, got)
	}
	return fresh, nil
}

// ListByHost returns artefacts matching the filter.
func (s *AuthoredArtefactStore) ListByHost(ctx context.Context, hostID string, filter persist.ArtefactFilter) ([]persist.Artefact, error) {
	// Build the query dynamically — classification filter is variadic.
	q := `SELECT ` + artefactSelectColumns + ` FROM authored_artefacts WHERE 1=1`
	args := []any{}
	idx := 1
	if hostID != "" {
		q += fmt.Sprintf(" AND host_id = $%d", idx)
		args = append(args, hostID)
		idx++
	}
	if filter.ForgeRunID != "" {
		q += fmt.Sprintf(" AND forge_run_id = $%d", idx)
		args = append(args, filter.ForgeRunID)
		idx++
	}
	if filter.SetID != "" {
		q += fmt.Sprintf(" AND set_id = $%d", idx)
		args = append(args, filter.SetID)
		idx++
	}
	if filter.State != "" {
		q += fmt.Sprintf(" AND state = $%d", idx)
		args = append(args, string(filter.State))
		idx++
	}
	if len(filter.ClassificationIn) > 0 {
		placeholders := ""
		for i, c := range filter.ClassificationIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", idx)
			args = append(args, string(c))
			idx++
		}
		q += " AND classification IN (" + placeholders + ")"
	}
	q += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", idx)
		args = append(args, filter.Limit)
		idx++
	}
	if filter.Offset > 0 {
		q += fmt.Sprintf(" OFFSET $%d", idx)
		args = append(args, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres artefact list: %w", err)
	}
	defer rows.Close()
	return collectArtefactRows(rows)
}

// RecordEvent appends an artefact_events row.
func (s *AuthoredArtefactStore) RecordEvent(ctx context.Context, ev persist.ArtefactEvent) error {
	if ev.SetID == "" || ev.Kind == "" {
		return persist.ErrInvalidInput
	}
	if ev.EventID == "" {
		ev.EventID = uuid.New().String()
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO artefact_events (event_id, set_id, kind, actor, at)
		 VALUES ($1, $2, $3, $4, $5)`,
		ev.EventID, ev.SetID, ev.Kind, ev.Actor, ev.At,
	)
	if err != nil {
		return fmt.Errorf("postgres artefact event: %w", err)
	}
	return nil
}

// ListEvents returns the audit trail for a set_id.
func (s *AuthoredArtefactStore) ListEvents(ctx context.Context, setID string) ([]persist.ArtefactEvent, error) {
	if setID == "" {
		return nil, persist.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx,
		`SELECT event_id, set_id, kind, actor, at FROM artefact_events
			WHERE set_id = $1 ORDER BY at`,
		setID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres artefact events: %w", err)
	}
	defer rows.Close()
	var out []persist.ArtefactEvent
	for rows.Next() {
		var ev persist.ArtefactEvent
		if err := rows.Scan(&ev.EventID, &ev.SetID, &ev.Kind, &ev.Actor, &ev.At); err != nil {
			return nil, fmt.Errorf("postgres artefact events scan: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres artefact events rows: %w", err)
	}
	return out, nil
}

func collectArtefactRows(rows pgx.Rows) ([]persist.Artefact, error) {
	var out []persist.Artefact
	for rows.Next() {
		a, err := scanArtefact(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres artefact scan: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres artefact rows: %w", err)
	}
	return out, nil
}
