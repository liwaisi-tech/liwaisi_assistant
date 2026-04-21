package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ModelRegistryRepository is the Postgres-backed implementation of
// cpn.ModelRegistry. Spec: spec-architecture-model-registry-and-a2ui-management.md.
type ModelRegistryRepository struct {
	pool pgxPool
}

var _ cpn.ModelRegistry = (*ModelRegistryRepository)(nil)

// NewModelRegistryRepository constructs a new adapter bound to the given pool.
func NewModelRegistryRepository(pool pgxPool) *ModelRegistryRepository {
	return &ModelRegistryRepository{pool: pool}
}

// ── Internal select column list (kept as one constant so every read path
// scans into scanEntry in the same order). ────────────────────────────────────
const modelSelectColumns = `
	m.id, m.registry_id, m.vendor, m.family, m.version, m.variant,
	m.display_name, m.description, m.hugging_face_id,
	m.modalities, m.capabilities,
	m.context_length, m.tokenizer,
	m.supported_parameters, m.default_parameters,
	m.pricing_input_per_token, m.pricing_output_per_token, m.pricing_currency,
	m.license_kind, m.license_spdx_id, m.license_community_slug,
	m.license_name, m.license_url, m.license_source, m.license_status,
	m.license_reviewed_by, m.license_reviewed_at,
	m.lifecycle_state, m.lifecycle_registered_at, m.lifecycle_activated_at,
	m.lifecycle_deprecated_at, m.lifecycle_sunset_at, m.lifecycle_replaced_by,
	m.lifecycle_reason,
	m.routes, m.source_metadata,
	m.created_at, m.updated_at,
	COALESCE(rc.product_default_model_id = m.id, FALSE) AS is_product_default
`

const modelFromJoin = `
	FROM models m
	LEFT JOIN registry_config rc ON rc.id = 1
`

// scanEntry reads a single row into a ModelRegistryEntry. Rows is either
// pgx.Rows or pgx.Row.
func scanEntry(row interface {
	Scan(dest ...any) error
}) (*cpn.ModelRegistryEntry, error) {
	e := &cpn.ModelRegistryEntry{}
	var (
		variant, huggingFace, spdx, communitySlug, licenseName, licenseURL *string
		reviewedBy, lifecycleReplacedBy, lifecycleReason                   *string
		reviewedAt, activatedAt, deprecatedAt, sunsetAt                    *time.Time

		modalitiesJSON, capabilitiesJSON          []byte
		supportedParamsJSON, defaultParamsJSON    []byte
		routesJSON, sourceMetadataJSON            []byte
		pricingInput, pricingOutput               float64
		contextLength                             int
		currency, tokenizer                       string
		licenseKind, licenseSource, licenseStatus string
		lifecycleState                            string
		registeredAt, createdAt, updatedAt        time.Time
		isProductDefault                          bool
	)
	err := row.Scan(
		&e.ID, &e.RegistryID, &e.Vendor, &e.Family, &e.Version, &variant,
		&e.DisplayName, &e.Description, &huggingFace,
		&modalitiesJSON, &capabilitiesJSON,
		&contextLength, &tokenizer,
		&supportedParamsJSON, &defaultParamsJSON,
		&pricingInput, &pricingOutput, &currency,
		&licenseKind, &spdx, &communitySlug,
		&licenseName, &licenseURL, &licenseSource, &licenseStatus,
		&reviewedBy, &reviewedAt,
		&lifecycleState, &registeredAt, &activatedAt,
		&deprecatedAt, &sunsetAt, &lifecycleReplacedBy,
		&lifecycleReason,
		&routesJSON, &sourceMetadataJSON,
		&createdAt, &updatedAt,
		&isProductDefault,
	)
	if err != nil {
		return nil, err
	}
	e.Variant = variant
	e.HuggingFaceID = huggingFace
	e.Context = cpn.ContextInfo{Length: contextLength, Tokenizer: tokenizer}
	e.Pricing = cpn.Pricing{InputPerToken: pricingInput, OutputPerToken: pricingOutput, Currency: currency}
	e.License = cpn.License{
		Kind:          licenseKind,
		SPDXID:        spdx,
		CommunitySlug: communitySlug,
		Name:          licenseName,
		URL:           licenseURL,
		Source:        licenseSource,
		Status:        licenseStatus,
		ReviewedBy:    reviewedBy,
		ReviewedAt:    reviewedAt,
	}
	e.Lifecycle = cpn.Lifecycle{
		State:        lifecycleState,
		RegisteredAt: registeredAt,
		ActivatedAt:  activatedAt,
		DeprecatedAt: deprecatedAt,
		SunsetAt:     sunsetAt,
		ReplacedBy:   lifecycleReplacedBy,
		Reason:       lifecycleReason,
	}
	e.CreatedAt = createdAt
	e.UpdatedAt = updatedAt
	e.IsProductDefault = isProductDefault

	// REQ-FIX-004: routes is load-bearing for invocation — a corrupt value
	// MUST propagate, not silently default to []. The other JSONB columns
	// are advisory and may default, but emit a warn so the corruption is
	// visible in the log stream.
	if err := json.Unmarshal(routesJSON, &e.Routes); err != nil {
		return nil, fmt.Errorf("postgres model registry: decode routes for %q: %w", e.RegistryID, err)
	}
	if err := json.Unmarshal(modalitiesJSON, &e.Modalities); err != nil {
		slog.Warn("postgres model registry: decode modalities failed; using default",
			"registry_id", e.RegistryID, "error", err)
		e.Modalities = cpn.Modalities{Input: []string{"text"}, Output: []string{"text"}}
	}
	if err := json.Unmarshal(capabilitiesJSON, &e.Capabilities); err != nil {
		slog.Warn("postgres model registry: decode capabilities failed; using default",
			"registry_id", e.RegistryID, "error", err)
		e.Capabilities = cpn.Capabilities{}
	}
	if err := json.Unmarshal(supportedParamsJSON, &e.SupportedParams); err != nil {
		slog.Warn("postgres model registry: decode supported_parameters failed; using default",
			"registry_id", e.RegistryID, "error", err)
		e.SupportedParams = []string{}
	}
	if err := json.Unmarshal(defaultParamsJSON, &e.DefaultParams); err != nil {
		slog.Warn("postgres model registry: decode default_parameters failed; using default",
			"registry_id", e.RegistryID, "error", err)
		e.DefaultParams = map[string]any{}
	}
	if err := json.Unmarshal(sourceMetadataJSON, &e.SourceMetadata); err != nil {
		slog.Warn("postgres model registry: decode source_metadata failed; using default",
			"registry_id", e.RegistryID, "error", err)
		e.SourceMetadata = map[string]any{}
	}
	return e, nil
}

// ── Reads ────────────────────────────────────────────────────────────────────

// GetInvokable enforces REQ-GATE-001 at the SQL layer (defense in depth with
// the Go predicate in cpn.Invokable).
func (r *ModelRegistryRepository) GetInvokable(ctx context.Context, registryID string) (*cpn.ModelRegistryEntry, error) {
	if registryID == "" {
		return nil, cpn.ErrInvalidInput
	}
	// Step 1 — does the row exist at all?
	entry, err := r.GetByID(ctx, registryID)
	if err != nil {
		return nil, err
	}
	// Step 2 — gate.
	if !entry.Invokable() {
		return nil, cpn.ErrModelNotInvokable
	}
	return entry, nil
}

// GetByID returns any row, regardless of invokability.
func (r *ModelRegistryRepository) GetByID(ctx context.Context, registryID string) (*cpn.ModelRegistryEntry, error) {
	if registryID == "" {
		return nil, cpn.ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx,
		`SELECT `+modelSelectColumns+modelFromJoin+` WHERE m.registry_id = $1`,
		registryID,
	)
	e, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cpn.ErrModelNotFound
		}
		return nil, fmt.Errorf("postgres model registry get by id %s: %w", registryID, err)
	}
	return e, nil
}

// GetProductDefault resolves the singleton registry_config pointer.
func (r *ModelRegistryRepository) GetProductDefault(ctx context.Context) (*cpn.ModelRegistryEntry, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+modelSelectColumns+modelFromJoin+` WHERE m.id = (SELECT product_default_model_id FROM registry_config WHERE id = 1)`,
	)
	e, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cpn.ErrModelNotFound
		}
		return nil, fmt.Errorf("postgres model registry get product default: %w", err)
	}
	return e, nil
}

// ListInvokable returns every row satisfying REQ-GATE-001.
func (r *ModelRegistryRepository) ListInvokable(ctx context.Context) ([]*cpn.ModelRegistryEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+modelSelectColumns+modelFromJoin+`
		 WHERE m.lifecycle_state = 'active'
		   AND m.license_status IN ('approved-commercial','approved-non-commercial','restricted')
		 ORDER BY m.vendor, m.family, m.version`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres model registry list invokable: %w", err)
	}
	defer rows.Close()
	return collectRows(rows)
}

// ListAll returns every row matching the filter.
func (r *ModelRegistryRepository) ListAll(ctx context.Context, filter cpn.ModelListFilter) ([]*cpn.ModelRegistryEntry, int, error) {
	var (
		wheres []string
		args   []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		wheres = append(wheres, fmt.Sprintf(cond, len(args)))
	}
	if filter.Vendor != "" {
		add("m.vendor = $%d", filter.Vendor)
	}
	if filter.LifecycleState != "" {
		add("m.lifecycle_state = $%d", filter.LifecycleState)
	}
	if filter.LicenseStatus != "" {
		add("m.license_status = $%d", filter.LicenseStatus)
	}
	if filter.Invokable != nil {
		if *filter.Invokable {
			wheres = append(wheres,
				"m.lifecycle_state = 'active' AND m.license_status IN ('approved-commercial','approved-non-commercial','restricted')")
		} else {
			wheres = append(wheres,
				"(m.lifecycle_state <> 'active' OR m.license_status NOT IN ('approved-commercial','approved-non-commercial','restricted'))")
		}
	}
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		wheres = append(wheres,
			fmt.Sprintf("(m.registry_id ILIKE $%d OR m.display_name ILIKE $%d)", len(args), len(args)))
	}

	order := "m.vendor, m.family, m.version"
	if len(filter.OrderBy) > 0 {
		order = safeOrderBy(filter.OrderBy)
	}

	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	pageClause := ""
	if filter.Page > 0 {
		pageSize := filter.PageSize
		if pageSize <= 0 {
			pageSize = 20
		}
		offset := (filter.Page - 1) * pageSize
		pageClause = fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)
	}

	// Total matches filter before pagination is applied (REQ-FIX-003). The
	// admin UI uses this value to render page counts; returning len(entries)
	// would lie whenever Page/PageSize trims the result set.
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)`+modelFromJoin+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres model registry list all count: %w", err)
	}

	query := `SELECT ` + modelSelectColumns + modelFromJoin + whereClause +
		` ORDER BY ` + order + pageClause
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres model registry list all: %w", err)
	}
	defer rows.Close()
	entries, err := collectRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// allowedOrderColumns is the whitelist of columns the admin UI may sort by.
// Anything not on this list is silently dropped to avoid SQL injection via
// OrderBy concatenation.
var allowedOrderColumns = map[string]string{
	"vendor":      "m.vendor",
	"family":      "m.family",
	"version":     "m.version",
	"registry_id": "m.registry_id",
	"lifecycle":   "m.lifecycle_state",
	"license":     "m.license_status",
	"created_at":  "m.created_at",
	"updated_at":  "m.updated_at",
}

func safeOrderBy(cols []string) string {
	var parts []string
	for _, c := range cols {
		if mapped, ok := allowedOrderColumns[c]; ok {
			parts = append(parts, mapped)
		}
	}
	if len(parts) == 0 {
		return "m.vendor, m.family, m.version"
	}
	return strings.Join(parts, ", ")
}

func collectRows(rows pgx.Rows) ([]*cpn.ModelRegistryEntry, error) {
	var out []*cpn.ModelRegistryEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres model registry scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres model registry rows err: %w", err)
	}
	return out, nil
}

// ── Writes ───────────────────────────────────────────────────────────────────

// Insert enforces REQ-LIC-003: new rows ALWAYS enter with
// license_status='unreviewed' and lifecycle_state='registered'. Any value
// passed by the caller is ignored — the admin MUST go through the
// license-review flow to activate a model.
func (r *ModelRegistryRepository) Insert(ctx context.Context, entry *cpn.ModelRegistryEntry) error {
	if entry == nil || entry.RegistryID == "" || entry.Vendor == "" || entry.Family == "" || entry.Version == "" {
		return cpn.ErrInvalidInput
	}
	modalities, err := marshalWithFallback(entry.Modalities, []byte(`{"input":["text"],"output":["text"]}`))
	if err != nil {
		return err
	}
	capabilities, err := marshalWithFallback(entry.Capabilities, []byte(`{}`))
	if err != nil {
		return err
	}
	routes, err := marshalWithFallback(entry.Routes, []byte(`[]`))
	if err != nil {
		return err
	}
	supportedParams, err := marshalWithFallback(entry.SupportedParams, []byte(`[]`))
	if err != nil {
		return err
	}
	defaultParams, err := marshalWithFallback(entry.DefaultParams, []byte(`{}`))
	if err != nil {
		return err
	}
	sourceMeta, err := marshalWithFallback(entry.SourceMetadata, []byte(`{}`))
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO models (
			registry_id, vendor, family, version, variant, display_name, description, hugging_face_id,
			modalities, capabilities, context_length, tokenizer,
			supported_parameters, default_parameters,
			pricing_input_per_token, pricing_output_per_token, pricing_currency,
			license_kind, license_spdx_id, license_community_slug,
			license_name, license_url, license_source,
			routes, source_metadata
		 ) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			$9,$10,$11,$12,
			$13,$14,
			$15,$16,$17,
			$18,$19,$20,
			$21,$22,$23,
			$24,$25
		 )`,
		entry.RegistryID, entry.Vendor, entry.Family, entry.Version, entry.Variant,
		entry.DisplayName, entry.Description, entry.HuggingFaceID,
		modalities, capabilities, entry.Context.Length, entry.Context.Tokenizer,
		supportedParams, defaultParams,
		entry.Pricing.InputPerToken, entry.Pricing.OutputPerToken, entry.Pricing.Currency,
		entry.License.Kind, entry.License.SPDXID, entry.License.CommunitySlug,
		entry.License.Name, entry.License.URL, entry.License.Source,
		routes, sourceMeta,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("postgres model registry insert: registry_id=%q: %w", entry.RegistryID, cpn.ErrDuplicateRegistryID)
		}
		return fmt.Errorf("postgres model registry insert: %w", err)
	}
	return nil
}

// Update applies a partial change. ifMatch is the previously-fetched UpdatedAt
// token (REQ-API-004). The updated_at trigger refreshes the timestamp on a
// successful update — the caller MUST re-read if they want the new value.
//
// Update does NOT change license_status or lifecycle_state — those go through
// SetLicenseReview / SetProductDefault so the audit trail and activation
// guards stay co-located.
func (r *ModelRegistryRepository) Update(ctx context.Context, entry *cpn.ModelRegistryEntry, ifMatch cpn.UpdatedAt) error {
	if entry == nil || entry.RegistryID == "" {
		return cpn.ErrInvalidInput
	}
	modalities, err := marshalWithFallback(entry.Modalities, []byte(`{"input":["text"],"output":["text"]}`))
	if err != nil {
		return err
	}
	capabilities, err := marshalWithFallback(entry.Capabilities, []byte(`{}`))
	if err != nil {
		return err
	}
	routes, err := marshalWithFallback(entry.Routes, []byte(`[]`))
	if err != nil {
		return err
	}
	supportedParams, err := marshalWithFallback(entry.SupportedParams, []byte(`[]`))
	if err != nil {
		return err
	}
	defaultParams, err := marshalWithFallback(entry.DefaultParams, []byte(`{}`))
	if err != nil {
		return err
	}
	sourceMeta, err := marshalWithFallback(entry.SourceMetadata, []byte(`{}`))
	if err != nil {
		return err
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE models SET
			variant              = $2,
			display_name         = $3,
			description          = $4,
			hugging_face_id      = $5,
			modalities           = $6,
			capabilities         = $7,
			context_length       = $8,
			tokenizer            = $9,
			supported_parameters = $10,
			default_parameters   = $11,
			pricing_input_per_token  = $12,
			pricing_output_per_token = $13,
			pricing_currency         = $14,
			license_kind           = $15,
			license_spdx_id        = $16,
			license_community_slug = $17,
			license_name           = $18,
			license_url            = $19,
			license_source         = $20,
			routes                 = $21,
			source_metadata        = $22
		 WHERE registry_id = $1 AND updated_at = $23`,
		entry.RegistryID, entry.Variant, entry.DisplayName, entry.Description, entry.HuggingFaceID,
		modalities, capabilities, entry.Context.Length, entry.Context.Tokenizer,
		supportedParams, defaultParams,
		entry.Pricing.InputPerToken, entry.Pricing.OutputPerToken, entry.Pricing.Currency,
		entry.License.Kind, entry.License.SPDXID, entry.License.CommunitySlug,
		entry.License.Name, entry.License.URL, entry.License.Source,
		routes, sourceMeta,
		time.Time(ifMatch),
	)
	if err != nil {
		return fmt.Errorf("postgres model registry update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Disambiguate: does the row exist at all, or is it just a stale
		// If-Match? The admin UX should tell the user to refresh.
		var exists bool
		qerr := r.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM models WHERE registry_id = $1)`,
			entry.RegistryID,
		).Scan(&exists)
		if qerr != nil {
			return fmt.Errorf("postgres model registry update existence check: %w", qerr)
		}
		if !exists {
			return cpn.ErrModelNotFound
		}
		return cpn.ErrRegistryConflict
	}
	return nil
}

// Delete hard-deletes. The adapter refuses if the row is the current default;
// this complements the FK RESTRICT on registry_config with a structured
// domain error (REQ-API-006).
func (r *ModelRegistryRepository) Delete(ctx context.Context, registryID string) error {
	if registryID == "" {
		return cpn.ErrInvalidInput
	}
	// Wrap default-check + DELETE in a transaction with FOR UPDATE on the
	// target row so a concurrent SetProductDefault can't swap the pointer
	// between our check and our delete (REQ-FIX-006 / AC-006).
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres model registry delete begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback best-effort on deferred cleanup

	var id string
	if err := tx.QueryRow(ctx,
		`SELECT id FROM models WHERE registry_id = $1 FOR UPDATE`, registryID,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cpn.ErrModelNotFound
		}
		return fmt.Errorf("postgres model registry delete lock target: %w", err)
	}

	var isDefault bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM registry_config
			WHERE id = 1 AND product_default_model_id = $1
		 )`, id,
	).Scan(&isDefault); err != nil {
		return fmt.Errorf("postgres model registry delete default check: %w", err)
	}
	if isDefault {
		return cpn.ErrCannotDeleteDefault
	}

	tag, err := tx.Exec(ctx, `DELETE FROM models WHERE id = $1`, id)
	if err != nil {
		// Either FK RESTRICT from model_role_defaults (typed as ErrModelInUse)
		// or, under a lost race, FK RESTRICT from registry_config itself —
		// treat that as ErrCannotDeleteDefault for parity with AC-006.
		if isForeignKeyViolation(err) {
			var pgErr *pgconn.PgError
			_ = errors.As(err, &pgErr)
			if pgErr != nil && pgErr.ConstraintName != "" &&
				strings.Contains(pgErr.ConstraintName, "registry_config") {
				return cpn.ErrCannotDeleteDefault
			}
			return cpn.ErrModelInUse
		}
		return fmt.Errorf("postgres model registry delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Row disappeared between our FOR UPDATE and the DELETE — shouldn't
		// happen under the tx but surface the truth if it does.
		return cpn.ErrModelNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres model registry delete commit: %w", err)
	}
	return nil
}

// SetProductDefault is atomic: it verifies invokability, auto-promotes
// lifecycle → 'active' when the target is 'registered' or 'disabled' with an
// approved license, and swaps the registry_config pointer. All in one tx.
//
// REQ-API-007. The auto-promotion matches the UX rule "setting a model as
// default implicitly activates it if it was registered-and-approved but not
// yet active — the admin already signaled trust by picking it".
func (r *ModelRegistryRepository) SetProductDefault(ctx context.Context, registryID, updatedBy string) error {
	if registryID == "" {
		return cpn.ErrInvalidInput
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres model registry set default begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback best-effort on deferred cleanup

	var (
		id             string
		lifecycleState string
		licenseStatus  string
	)
	err = tx.QueryRow(ctx,
		`SELECT id, lifecycle_state, license_status FROM models WHERE registry_id = $1 FOR UPDATE`,
		registryID,
	).Scan(&id, &lifecycleState, &licenseStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cpn.ErrModelNotFound
		}
		return fmt.Errorf("postgres model registry set default read target: %w", err)
	}

	// REQ-GATE-001 — license must be approved to enter/remain active.
	approved := licenseStatus == cpn.LicenseApprovedCommercial ||
		licenseStatus == cpn.LicenseApprovedNonCommerc ||
		licenseStatus == cpn.LicenseRestricted
	if !approved {
		return cpn.ErrModelNotInvokable
	}

	// Auto-promote to 'active' when safe to do so.
	if lifecycleState != cpn.LifecycleActive {
		if lifecycleState != cpn.LifecycleRegistered && lifecycleState != cpn.LifecycleDisabled {
			// deprecated/sunset/removed/etc → cannot become default.
			return cpn.ErrModelNotInvokable
		}
		_, err = tx.Exec(ctx,
			`UPDATE models SET lifecycle_state = 'active',
			                   lifecycle_activated_at = COALESCE(lifecycle_activated_at, NOW())
			 WHERE id = $1`, id,
		)
		if err != nil {
			return fmt.Errorf("postgres model registry set default promote: %w", err)
		}
	}

	// Swap the pointer.
	_, err = tx.Exec(ctx,
		`UPDATE registry_config SET product_default_model_id = $1, updated_by = $2, updated_at = NOW() WHERE id = 1`,
		id, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("postgres model registry set default swap pointer: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres model registry set default commit: %w", err)
	}
	return nil
}

// SetLicenseReview transitions license_status + audit fields. If the target
// row is still in 'pending-license-review', lifecycle auto-advances to
// 'registered' (AC-LIC-003).
func (r *ModelRegistryRepository) SetLicenseReview(ctx context.Context, registryID string, review cpn.LicenseReview) error {
	if registryID == "" || review.Status == "" || review.ReviewerID == "" {
		return cpn.ErrInvalidInput
	}
	// Validate status value server-side to avoid SQL roundtrip to the enum.
	switch review.Status {
	case cpn.LicenseUnreviewed, cpn.LicenseReviewInProgress,
		cpn.LicenseApprovedCommercial, cpn.LicenseApprovedNonCommerc,
		cpn.LicenseRestricted, cpn.LicenseBlocked, cpn.LicenseUnknown:
	default:
		return cpn.ErrInvalidInput
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres model registry license review begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback best-effort on deferred cleanup

	var lifecycleState string
	err = tx.QueryRow(ctx,
		`SELECT lifecycle_state FROM models WHERE registry_id = $1 FOR UPDATE`,
		registryID,
	).Scan(&lifecycleState)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cpn.ErrModelNotFound
		}
		return fmt.Errorf("postgres model registry license review read: %w", err)
	}

	note := review.Note
	_, err = tx.Exec(ctx,
		`UPDATE models SET
			license_status      = $2::model_license_status,
			license_reviewed_by = $3,
			license_reviewed_at = NOW(),
			source_metadata     = COALESCE(source_metadata, '{}'::jsonb)
			                      || jsonb_build_object('last_license_review_note', $4::text)
		 WHERE registry_id = $1`,
		registryID, review.Status, review.ReviewerID, note,
	)
	if err != nil {
		return fmt.Errorf("postgres model registry license review update: %w", err)
	}

	if lifecycleState == cpn.LifecyclePendingLicenseReview {
		_, err = tx.Exec(ctx,
			`UPDATE models SET lifecycle_state = 'registered' WHERE registry_id = $1`,
			registryID,
		)
		if err != nil {
			return fmt.Errorf("postgres model registry license review advance lifecycle: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres model registry license review commit: %w", err)
	}
	return nil
}

// GetRoleDefault returns the registry_id bound to the given role, or ErrModelNotFound.
func (r *ModelRegistryRepository) GetRoleDefault(ctx context.Context, role string) (string, error) {
	if role == "" {
		return "", cpn.ErrInvalidInput
	}
	var registryID string
	err := r.pool.QueryRow(ctx,
		`SELECT registry_id FROM model_role_defaults WHERE role = $1`, role,
	).Scan(&registryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", cpn.ErrModelNotFound
		}
		return "", fmt.Errorf("postgres model registry get role default %s: %w", role, err)
	}
	return registryID, nil
}

// SetRoleDefault updates or inserts the role → model mapping.
// The target registry_id MUST correspond to an existing model (DB FK enforces
// this; ErrModelNotFound is surfaced for a cleaner admin UX).
func (r *ModelRegistryRepository) SetRoleDefault(ctx context.Context, role, registryID string) error {
	if role == "" || registryID == "" {
		return cpn.ErrInvalidInput
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO model_role_defaults (role, registry_id, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (role) DO UPDATE SET registry_id = EXCLUDED.registry_id, updated_at = NOW()`,
		role, registryID,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return cpn.ErrModelNotFound
		}
		return fmt.Errorf("postgres model registry set role default: %w", err)
	}
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func marshalWithFallback(v any, fallback []byte) ([]byte, error) {
	if v == nil {
		return fallback, nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("postgres model registry marshal: %w", err)
	}
	if len(out) == 0 || string(out) == "null" {
		return fallback, nil
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
