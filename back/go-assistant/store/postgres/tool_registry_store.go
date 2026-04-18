package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ToolRegistryStore is the Postgres-backed implementation of
// persist.ToolRegistryRepository. Spec: spec-architecture-dynamic-tool-registry.md.
type ToolRegistryStore struct {
	pool pgxPool
}

var _ persist.ToolRegistryRepository = (*ToolRegistryStore)(nil)

// NewToolRegistryStore constructs a new adapter bound to the given pool.
func NewToolRegistryStore(pool pgxPool) *ToolRegistryStore {
	return &ToolRegistryStore{pool: pool}
}

const toolSelectColumns = `
	id, namespace, name, version, schema,
	help_text, man_page, binary_path, binary_sha256,
	origin, provenance,
	registered_at, registered_by,
	deprecated, deprecated_at, deprecation_reason
`

func scanToolEntry(row interface {
	Scan(dest ...any) error
}) (persist.ToolRegistryEntry, error) {
	var (
		e             persist.ToolRegistryEntry
		schema        []byte
		provenance    []byte
		deprecatedAt  *time.Time
		deprecationRz *string
	)
	err := row.Scan(
		&e.ID, &e.Namespace, &e.Name, &e.Version, &schema,
		&e.HelpText, &e.ManPage, &e.BinaryPath, &e.BinarySHA256,
		&e.Origin, &provenance,
		&e.RegisteredAt, &e.RegisteredBy,
		&e.Deprecated, &deprecatedAt, &deprecationRz,
	)
	if err != nil {
		return persist.ToolRegistryEntry{}, err
	}
	e.Schema = json.RawMessage(schema)
	if len(provenance) > 0 {
		if err := json.Unmarshal(provenance, &e.Provenance); err != nil {
			// Keep the row rather than failing the whole list — provenance
			// is advisory metadata.
			e.Provenance = persist.Provenance{}
		}
	}
	if deprecatedAt != nil {
		e.DeprecatedAt = *deprecatedAt
	}
	if deprecationRz != nil {
		e.DeprecationReason = *deprecationRz
	}
	return e, nil
}

// Upsert persists the entry. Returns persist.ErrToolDuplicate on conflict.
func (s *ToolRegistryStore) Upsert(ctx context.Context, e persist.ToolRegistryEntry) error {
	if e.Namespace == "" || e.Name == "" || e.Version == "" {
		return persist.ErrInvalidInput
	}
	if e.RegisteredAt.IsZero() {
		e.RegisteredAt = time.Now().UTC()
	}
	schema := e.Schema
	if len(schema) == 0 {
		schema = json.RawMessage(`{}`)
	}
	provenance, err := json.Marshal(e.Provenance)
	if err != nil {
		return fmt.Errorf("postgres tool upsert: marshal provenance: %w", err)
	}

	var deprecatedAt any
	if e.Deprecated && !e.DeprecatedAt.IsZero() {
		deprecatedAt = e.DeprecatedAt
	}
	var deprecationReason any
	if e.DeprecationReason != "" {
		deprecationReason = e.DeprecationReason
	}

	if e.ID == "" {
		// Let Postgres generate the UUID and read it back.
		row := s.pool.QueryRow(ctx,
			`INSERT INTO tools (
				namespace, name, version, schema,
				help_text, man_page, binary_path, binary_sha256,
				origin, provenance,
				registered_at, registered_by,
				deprecated, deprecated_at, deprecation_reason
			 ) VALUES (
				$1,$2,$3,$4,
				$5,$6,$7,$8,
				$9,$10,
				$11,$12,
				$13,$14,$15
			 ) RETURNING id`,
			e.Namespace, e.Name, e.Version, schema,
			e.HelpText, e.ManPage, e.BinaryPath, e.BinarySHA256,
			e.Origin, provenance,
			e.RegisteredAt, e.RegisteredBy,
			e.Deprecated, deprecatedAt, deprecationReason,
		)
		if err := row.Scan(&e.ID); err != nil {
			if isToolDuplicate(err) {
				return persist.ErrToolDuplicate
			}
			return fmt.Errorf("postgres tool upsert: %w", err)
		}
		return nil
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO tools (
			id, namespace, name, version, schema,
			help_text, man_page, binary_path, binary_sha256,
			origin, provenance,
			registered_at, registered_by,
			deprecated, deprecated_at, deprecation_reason
		 ) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,
			$10,$11,
			$12,$13,
			$14,$15,$16
		 )`,
		e.ID, e.Namespace, e.Name, e.Version, schema,
		e.HelpText, e.ManPage, e.BinaryPath, e.BinarySHA256,
		e.Origin, provenance,
		e.RegisteredAt, e.RegisteredBy,
		e.Deprecated, deprecatedAt, deprecationReason,
	)
	if err != nil {
		if isToolDuplicate(err) {
			return persist.ErrToolDuplicate
		}
		return fmt.Errorf("postgres tool upsert: %w", err)
	}
	return nil
}

// Get resolves a qualified name — either "ns/name@ver" (exact) or "ns/name"
// (latest non-deprecated).
func (s *ToolRegistryStore) Get(ctx context.Context, qualifiedName string) (persist.ToolRegistryEntry, error) {
	ns, name, version, ok := persist.SplitQualifiedName(qualifiedName)
	if !ok {
		return persist.ToolRegistryEntry{}, persist.ErrInvalidInput
	}
	if version != "" {
		return s.GetVersion(ctx, ns, name, version)
	}
	return s.Latest(ctx, ns, name)
}

// GetVersion fetches a specific (namespace, name, version).
func (s *ToolRegistryStore) GetVersion(ctx context.Context, namespace, name, version string) (persist.ToolRegistryEntry, error) {
	if namespace == "" || name == "" || version == "" {
		return persist.ToolRegistryEntry{}, persist.ErrInvalidInput
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+toolSelectColumns+` FROM tools
		 WHERE namespace = $1 AND name = $2 AND version = $3`,
		namespace, name, version,
	)
	e, err := scanToolEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return persist.ToolRegistryEntry{}, persist.ErrToolNotFound
		}
		return persist.ToolRegistryEntry{}, fmt.Errorf("postgres tool get version: %w", err)
	}
	return e, nil
}

// ListByNamespace returns every version persisted under the given namespace.
func (s *ToolRegistryStore) ListByNamespace(ctx context.Context, namespace string) ([]persist.ToolRegistryEntry, error) {
	if namespace == "" {
		return nil, persist.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+toolSelectColumns+` FROM tools
		 WHERE namespace = $1
		 ORDER BY name, version`,
		namespace,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres tool list by namespace: %w", err)
	}
	defer rows.Close()
	return collectToolRows(rows)
}

// ListAll returns every persisted row.
func (s *ToolRegistryStore) ListAll(ctx context.Context) ([]persist.ToolRegistryEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+toolSelectColumns+` FROM tools
		 ORDER BY namespace, name, version`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres tool list all: %w", err)
	}
	defer rows.Close()
	return collectToolRows(rows)
}

// Deprecate flags the specified qualified-name-with-version as deprecated.
func (s *ToolRegistryStore) Deprecate(ctx context.Context, qualifiedName, reason string) error {
	ns, name, version, ok := persist.SplitQualifiedName(qualifiedName)
	if !ok || version == "" {
		return persist.ErrInvalidInput
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE tools SET
			deprecated = TRUE,
			deprecated_at = COALESCE(deprecated_at, NOW()),
			deprecation_reason = $4
		 WHERE namespace = $1 AND name = $2 AND version = $3`,
		ns, name, version, reason,
	)
	if err != nil {
		return fmt.Errorf("postgres tool deprecate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrToolNotFound
	}
	return nil
}

// Latest returns the highest semver non-deprecated row for (ns, name).
// Sorting is done in Go after fetching every non-deprecated row because
// string ORDER BY on "version" does not give semver ordering.
func (s *ToolRegistryStore) Latest(ctx context.Context, namespace, name string) (persist.ToolRegistryEntry, error) {
	if namespace == "" || name == "" {
		return persist.ToolRegistryEntry{}, persist.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+toolSelectColumns+` FROM tools
		 WHERE namespace = $1 AND name = $2 AND deprecated = FALSE`,
		namespace, name,
	)
	if err != nil {
		return persist.ToolRegistryEntry{}, fmt.Errorf("postgres tool latest: %w", err)
	}
	defer rows.Close()
	entries, err := collectToolRows(rows)
	if err != nil {
		return persist.ToolRegistryEntry{}, err
	}
	if len(entries) == 0 {
		return persist.ToolRegistryEntry{}, persist.ErrToolNotFound
	}
	winner := entries[0]
	for _, e := range entries[1:] {
		if persist.CompareSemver(e.Version, winner.Version) > 0 {
			winner = e
		}
	}
	return winner, nil
}

// Delete hard-deletes a qualified-name-with-version.
func (s *ToolRegistryStore) Delete(ctx context.Context, qualifiedName string) error {
	ns, name, version, ok := persist.SplitQualifiedName(qualifiedName)
	if !ok || version == "" {
		return persist.ErrInvalidInput
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tools WHERE namespace = $1 AND name = $2 AND version = $3`,
		ns, name, version,
	)
	if err != nil {
		return fmt.Errorf("postgres tool delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrToolNotFound
	}
	return nil
}

func collectToolRows(rows pgx.Rows) ([]persist.ToolRegistryEntry, error) {
	var out []persist.ToolRegistryEntry
	for rows.Next() {
		e, err := scanToolEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres tool scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres tool rows err: %w", err)
	}
	return out, nil
}

func isToolDuplicate(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
