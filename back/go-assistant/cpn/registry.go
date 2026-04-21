package cpn

import (
	"context"
	"errors"
	"time"
)

// ── Model Registry Port ──────────────────────────────────────────────────────
// Spec: spec/spec-architecture-model-registry-and-a2ui-management.md §4.3
//
// ModelRegistry is the domain port for the database-backed LLM model catalog.
// It supersedes the compile-time constants in infra/openrouter (AvailableModels,
// modelCostTable, DefaultModelRegistry) by reading every model from the `models`
// Postgres table and resolving the product default via the `registry_config`
// singleton pointer (REQ-REG-008).
//
// The infrastructure implementation lives in store/postgres/model_registry.go
// (PostgresModelRegistry — workstream B2). Tests use mocks defined in
// cpn/registry_mock_test.go.

// ErrModelNotInvokable is returned by GetInvokable when the target row exists
// but fails the REQ-GATE-001 predicate (lifecycle=active AND license approved).
// Callers MUST treat it as a fallback-to-default signal, not an infrastructure
// error (see REQ-GATE-002: the session service falls back to the product default
// and logs a WARN with the reason).
var ErrModelNotInvokable = errors.New("cpn: model is not invokable")

// ErrModelNotFound is returned when no row exists for the given registry_id.
// Distinct from ErrModelNotInvokable because callers handle them differently:
// NotFound → "typo, fallback silently"; NotInvokable → "legal/operational gate".
var ErrModelNotFound = errors.New("cpn: model not found")

// ErrRegistryConflict is returned by Update/SetProductDefault when the supplied
// If-Match UpdatedAt token does not match the current row (REQ-API-004 optimistic
// concurrency). Callers should re-fetch and retry.
var ErrRegistryConflict = errors.New("cpn: registry row was modified by another writer")

// ErrCannotDeleteDefault is returned by Delete when the caller tries to remove
// the row referenced by registry_config. The user MUST reassign the default
// first (REQ-API-006).
var ErrCannotDeleteDefault = errors.New("cpn: cannot delete the product default; reassign it first")

// ErrInvalidInput is returned when required arguments (e.g. registry_id) are empty.
var ErrInvalidInput = errors.New("cpn: invalid input")

// UpdatedAt is a value type used for optimistic concurrency on PATCH operations
// (REQ-API-004 If-Match guard). The adapter compares this against the current
// row's updated_at column and rejects the update if they differ.
type UpdatedAt time.Time

// ModelRegistry is the domain port. The adapter implementation lives in
// store/postgres.
//
// GetInvokable is the PRIMARY runtime-path lookup — it enforces REQ-GATE-001 at
// the repo layer so transition-firing code paths CANNOT bypass the license +
// lifecycle gate (defense in depth per REQ-GATE-004). Admin UIs use GetByID /
// ListAll, which return any row regardless of invokability.
type ModelRegistry interface {
	// GetInvokable returns the entry IFF it satisfies REQ-GATE-001
	// (lifecycle = active AND license.status ∈ {approved-commercial,
	// approved-non-commercial, restricted}). Non-invokable rows return
	// ErrModelNotInvokable; missing rows return ErrModelNotFound.
	//
	// All transition firing paths MUST call this method, not GetByID.
	GetInvokable(ctx context.Context, registryID string) (*ModelRegistryEntry, error)

	// GetByID is the ADMIN-path lookup. Returns any row regardless of
	// invokability. MUST NOT be used from the transition firing path.
	GetByID(ctx context.Context, registryID string) (*ModelRegistryEntry, error)

	// GetProductDefault resolves the singleton registry_config pointer and
	// returns the entry it references. Never returns ErrModelNotFound
	// after migration 020 has run (the FK RESTRICT + NOT NULL guarantees
	// "at least one").
	GetProductDefault(ctx context.Context) (*ModelRegistryEntry, error)

	// ListInvokable returns every row that satisfies REQ-GATE-001. Used by
	// GET /api/v1/models for the public model picker.
	ListInvokable(ctx context.Context) ([]*ModelRegistryEntry, error)

	// ListAll returns every row matching the filter along with the total
	// number of rows matching the filter (ignoring Page/PageSize). The total
	// is used by the admin UI to render pagination — len(entries) is a page,
	// not the full count (REQ-FIX-003 / AC-003).
	ListAll(ctx context.Context, filter ModelListFilter) (entries []*ModelRegistryEntry, total int, err error)

	// Insert registers a new model. Initial license_status = 'unreviewed' and
	// lifecycle_state = 'registered' per REQ-LIC-003 — the caller cannot
	// short-circuit the human review gate by passing approved values on insert.
	Insert(ctx context.Context, entry *ModelRegistryEntry) error

	// Update applies a partial change. The ifMatch argument implements REQ-API-004
	// optimistic concurrency: if the current updated_at differs, the adapter
	// returns ErrConflict.
	Update(ctx context.Context, entry *ModelRegistryEntry, ifMatch UpdatedAt) error

	// Delete hard-deletes the row. The adapter MUST refuse if the row is
	// referenced by registry_config.product_default_model_id (the DB's
	// ON DELETE RESTRICT is the last line of defense; a structured
	// ErrCannotDeleteDefault is preferred at the service layer — REQ-API-006).
	Delete(ctx context.Context, registryID string) error

	// SetProductDefault atomically validates invokability, promotes lifecycle
	// to 'active' if currently 'registered' or 'disabled' with an approved
	// license, and swaps the registry_config pointer (REQ-API-007).
	SetProductDefault(ctx context.Context, registryID string, updatedBy string) error

	// SetLicenseReview transitions license_status and records the reviewer.
	// If the target row was pending-license-review, its lifecycle auto-advances
	// to 'registered' per AC-LIC-003.
	SetLicenseReview(ctx context.Context, registryID string, review LicenseReview) error

	// GetRoleDefault returns the registry_id bound to the given role.
	GetRoleDefault(ctx context.Context, role string) (string, error)

	// SetRoleDefault updates the role → model mapping.
	SetRoleDefault(ctx context.Context, role, registryID string) error
}

// ── Filters & value types ────────────────────────────────────────────────────

// ModelListFilter narrows ListAll results. Zero value returns all rows.
type ModelListFilter struct {
	Vendor         string   // empty → any
	LifecycleState string   // empty → any
	LicenseStatus  string   // empty → any
	Invokable      *bool    // nil → any; true → only invokable; false → only non-invokable
	Search         string   // case-insensitive match against registry_id OR display_name
	Page           int      // 1-indexed; 0 → all rows (admin internal use)
	PageSize       int      // 0 → use default (20 for A2UI list surface per REQ-A2UI-007)
	OrderBy        []string // e.g. ["vendor","family"]
}

// LicenseReview is the payload to SetLicenseReview.
type LicenseReview struct {
	Status     string // one of LicenseStatus* constants below
	ReviewerID string // the admin's email
	Note       string // optional; preserved in source_metadata
}

// ── ModelRegistryEntry ──────────────────────────────────────────────────────

// ModelRegistryEntry is the domain representation of one row in the `models`
// table, enriched with the derived IsProductDefault flag (computed by the
// adapter via JOIN with registry_config).
type ModelRegistryEntry struct {
	ID          string // UUID
	RegistryID  string
	Vendor      string
	Family      string
	Version     string
	Variant     *string
	DisplayName string
	Description string

	HuggingFaceID *string

	Modalities      Modalities
	Capabilities    Capabilities
	Context         ContextInfo
	Pricing         Pricing
	SupportedParams []string
	DefaultParams   map[string]any

	License   License
	Lifecycle Lifecycle

	Routes         []Route
	SourceMetadata map[string]any

	// IsProductDefault is DERIVED: set by the adapter iff this row's id equals
	// registry_config.product_default_model_id. Never written to the DB.
	IsProductDefault bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Modalities describes input/output media.
type Modalities struct {
	Input  []string `json:"input"` // "text" | "image" | "audio" | "video"
	Output []string `json:"output"`
}

// Capabilities is a flag set for feature support.
type Capabilities struct {
	Text             bool `json:"text"`
	Tools            bool `json:"tools"`
	Streaming        bool `json:"streaming"`
	Reasoning        bool `json:"reasoning"`
	StructuredOutput bool `json:"structured_output"`
	Vision           bool `json:"vision"`
	Audio            bool `json:"audio"`
}

// ContextInfo captures context window + tokenizer metadata.
type ContextInfo struct {
	Length    int    `json:"length"`
	Tokenizer string `json:"tokenizer"`
}

// Pricing is per-token in the given currency (USD by default). Stored as
// strings in the JSON schema to preserve precision across languages; the Go
// type uses float64 because Postgres NUMERIC(20,12) fits comfortably in
// float64 and downstream cost arithmetic is already float-based.
type Pricing struct {
	InputPerToken  float64 `json:"input_per_token"`
	OutputPerToken float64 `json:"output_per_token"`
	Currency       string  `json:"currency"`
}

// License captures both the legal metadata and the product's clearance state.
type License struct {
	Kind          string     `json:"kind"`                     // "community" | "proprietary-api" | "proprietary-weights" | "unknown"
	SPDXID        *string    `json:"spdx_id,omitempty"`        // e.g. "apache-2.0", "mit"
	CommunitySlug *string    `json:"community_slug,omitempty"` // e.g. "gemma", "llama3.3", "qwen"
	Name          *string    `json:"name,omitempty"`           // e.g. "Anthropic Commercial Terms", "Gemma Terms of Use"
	URL           *string    `json:"url,omitempty"`
	Source        string     `json:"source"` // "huggingface" | "manual"
	Status        string     `json:"status"` // LicenseStatus* constant
	ReviewedBy    *string    `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
}

// Lifecycle captures the operational state.
type Lifecycle struct {
	State        string     `json:"state"` // LifecycleState* constant
	RegisteredAt time.Time  `json:"registered_at"`
	ActivatedAt  *time.Time `json:"activated_at,omitempty"`
	DeprecatedAt *time.Time `json:"deprecated_at,omitempty"`
	SunsetAt     *time.Time `json:"sunset_at,omitempty"`
	ReplacedBy   *string    `json:"replaced_by,omitempty"`
	Reason       *string    `json:"reason,omitempty"`
}

// Route describes one way to invoke a model. One model MAY have many routes
// (e.g., OpenRouter and direct Anthropic), ordered by Priority.
type Route struct {
	ProviderAdapter string  `json:"provider_adapter"` // "openrouter" | "anthropic" | "openai" | "google-gemini" | ...
	ProviderModelID string  `json:"provider_model_id"`
	EndpointBaseURL *string `json:"endpoint_base_url,omitempty"`
	Priority        int     `json:"priority"`
	Enabled         bool    `json:"enabled"`
	Region          *string `json:"region,omitempty"`
	IsModerated     *bool   `json:"is_moderated,omitempty"`
}

// ── State constants ──────────────────────────────────────────────────────────

// Lifecycle states (REQ-REG-004).
const (
	LifecycleDiscovered           = "discovered"
	LifecyclePendingLicenseReview = "pending-license-review"
	LifecycleRegistered           = "registered"
	LifecycleActive               = "active"
	LifecycleDisabled             = "disabled"
	LifecycleDeprecated           = "deprecated"
	LifecycleSunset               = "sunset"
	LifecycleRemoved              = "removed"
)

// License statuses (REQ-LIC-*).
const (
	LicenseUnreviewed         = "unreviewed"
	LicenseReviewInProgress   = "review-in-progress"
	LicenseApprovedCommercial = "approved-commercial"
	LicenseApprovedNonCommerc = "approved-non-commercial"
	LicenseRestricted         = "restricted"
	LicenseBlocked            = "blocked"
	LicenseUnknown            = "unknown"
)

// Canonical roles that can have a default bound via model_role_defaults.
var CanonicalRoles = []string{
	"classifier", "structured", "reasoning",
	"long-context", "summarize", "thinking",
}

// ── Invokability predicate (REQ-GATE-001) ────────────────────────────────────

// Invokable reports whether the entry can be stamped onto an LLMConfig.Model
// at runtime. Mirrored in SQL by the adapter's GetInvokable filter so the
// check is enforced at both layers (defense in depth — REQ-GATE-004).
func (m *ModelRegistryEntry) Invokable() bool {
	if m == nil {
		return false
	}
	if m.Lifecycle.State != LifecycleActive {
		return false
	}
	switch m.License.Status {
	case LicenseApprovedCommercial,
		LicenseApprovedNonCommerc,
		LicenseRestricted:
		return true
	}
	return false
}

// CanInvoke is a pure helper with the same predicate, exposed for the admin
// UI read path (the API stamps IsInvokable on every returned entry so the
// client doesn't need to duplicate the rule).
func CanInvoke(m *ModelRegistryEntry) bool { return m.Invokable() }
