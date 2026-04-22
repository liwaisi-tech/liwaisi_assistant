package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

// PendingToolRepository is the Postgres-backed implementation of
// toolsynth.PendingToolStore. It promotes the previously in-memory staging
// store (SC-12) to durable storage so synthesized manifests survive restarts.
//
// Rows are keyed on the composite (session_id, tool_name, provenance_sha256)
// — provenance-SHA256 drift therefore writes a new row rather than mutating
// the existing one. Stage upserts a row matching all three components,
// preserving the latest manifest_json/source_sha256/created_at for that key.
type PendingToolRepository struct {
	pool pgxPool
}

var _ toolsynth.PendingToolStore = (*PendingToolRepository)(nil)

// NewPendingToolRepository constructs a PendingToolRepository bound to pool.
func NewPendingToolRepository(pool pgxPool) *PendingToolRepository {
	return &PendingToolRepository{pool: pool}
}

// Stage upserts a PendingTool. The composite key
// (session_id, tool_name, provenance_sha256) is idempotent — an arrival with
// the same triple overwrites manifest_json, source_sha256 and created_at so
// the freshest representation wins.
func (r *PendingToolRepository) Stage(ctx context.Context, tool toolsynth.PendingTool) error {
	if tool.Manifest.Name == "" {
		return errors.New("postgres-pending-tool: manifest missing name")
	}
	manifestJSON, err := json.Marshal(tool.Manifest)
	if err != nil {
		return fmt.Errorf("postgres-pending-tool: marshal manifest: %w", err)
	}
	createdAt := tool.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	const q = `
		INSERT INTO pending_tools
			(session_id, tool_name, provenance_sha256, manifest_json, source_sha256, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (session_id, tool_name, provenance_sha256) DO UPDATE
		  SET manifest_json = EXCLUDED.manifest_json,
		      source_sha256 = EXCLUDED.source_sha256,
		      created_at    = EXCLUDED.created_at
	`
	if _, err := r.pool.Exec(ctx, q,
		tool.SessionID, tool.Manifest.Name, tool.ProvenanceSHA256,
		manifestJSON, tool.SourceSHA256, createdAt,
	); err != nil {
		return fmt.Errorf("postgres-pending-tool: stage: %w", err)
	}
	return nil
}

// List returns every PendingTool for the given session ordered by tool_name
// then provenance_sha256 (deterministic, matches the in-memory ordering).
// When sessionID is empty, every staged row is returned.
func (r *PendingToolRepository) List(ctx context.Context, sessionID string) ([]toolsynth.PendingTool, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if sessionID == "" {
		const q = `
			SELECT session_id, tool_name, provenance_sha256, manifest_json, source_sha256, created_at
			FROM pending_tools
			ORDER BY tool_name ASC, provenance_sha256 ASC
		`
		rows, err = r.pool.Query(ctx, q)
	} else {
		const q = `
			SELECT session_id, tool_name, provenance_sha256, manifest_json, source_sha256, created_at
			FROM pending_tools
			WHERE session_id = $1
			ORDER BY tool_name ASC, provenance_sha256 ASC
		`
		rows, err = r.pool.Query(ctx, q, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres-pending-tool: list: %w", err)
	}
	defer rows.Close()

	out := make([]toolsynth.PendingTool, 0)
	for rows.Next() {
		t, err := scanPendingTool(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres-pending-tool: scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres-pending-tool: rows: %w", err)
	}
	return out, nil
}

// Get returns the most-recently-staged PendingTool for the given tool_name
// across all sessions. The (false, nil) tuple indicates a clean miss — the
// caller distinguishes "absent" from "errored" without inspecting err.
func (r *PendingToolRepository) Get(ctx context.Context, toolName string) (toolsynth.PendingTool, bool, error) {
	const q = `
		SELECT session_id, tool_name, provenance_sha256, manifest_json, source_sha256, created_at
		FROM pending_tools
		WHERE tool_name = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	row := r.pool.QueryRow(ctx, q, toolName)
	t, err := scanPendingTool(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return toolsynth.PendingTool{}, false, nil
		}
		return toolsynth.PendingTool{}, false, fmt.Errorf("postgres-pending-tool: get: %w", err)
	}
	return t, true, nil
}

// Delete removes every row for tool_name across all sessions. Mirrors the
// in-memory store's "purge by name" semantics. Returns
// toolsynth.ErrPendingNotFound when no rows match so the caller can treat
// repeated deletes as a no-op.
func (r *PendingToolRepository) Delete(ctx context.Context, toolName string) error {
	const q = `DELETE FROM pending_tools WHERE tool_name = $1`
	tag, err := r.pool.Exec(ctx, q, toolName)
	if err != nil {
		return fmt.Errorf("postgres-pending-tool: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return toolsynth.ErrPendingNotFound
	}
	return nil
}

// pendingScanner abstracts pgx.Row and pgx.Rows for a shared scan helper.
type pendingScanner interface {
	Scan(dest ...any) error
}

func scanPendingTool(s pendingScanner) (toolsynth.PendingTool, error) {
	var (
		t            toolsynth.PendingTool
		manifestJSON []byte
		createdAt    time.Time
		toolName     string
	)
	if err := s.Scan(
		&t.SessionID,
		&toolName,
		&t.ProvenanceSHA256,
		&manifestJSON,
		&t.SourceSHA256,
		&createdAt,
	); err != nil {
		return toolsynth.PendingTool{}, err
	}
	var manifest cpn.ToolManifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return toolsynth.PendingTool{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	// Defensive: make sure the row's tool_name matches the manifest name —
	// if a writer ever drifts, prefer the column value as the source of truth.
	if manifest.Name == "" {
		manifest.Name = toolName
	}
	t.Manifest = manifest
	t.CreatedAt = createdAt
	return t, nil
}
