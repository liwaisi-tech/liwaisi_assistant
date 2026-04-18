package persist

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Provenance is the audit trail attached to a registered tool, per
// spec-architecture-dynamic-tool-registry.md §4.
type Provenance struct {
	AuthoringCPNID string `json:"authoring_cpn_id,omitempty"`
	FlowHash       string `json:"flow_hash,omitempty"`
	ForgeRunID     string `json:"forge_run_id,omitempty"`
	PromptDigest   string `json:"prompt_digest,omitempty"`
	SourcePath     string `json:"source_path,omitempty"`
	SourceSHA256   string `json:"source_sha256,omitempty"`
}

// ToolRegistryEntry is the persistence DTO for the dynamic tool registry. It
// mirrors cpn/tools.ToolEntry field-for-field minus the in-process executor
// closure, which is never persisted.
type ToolRegistryEntry struct {
	ID                string          `json:"id"`
	Namespace         string          `json:"namespace"`
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	Schema            json.RawMessage `json:"schema"`
	HelpText          string          `json:"help_text"`
	ManPage           string          `json:"man_page"`
	BinaryPath        string          `json:"binary_path"`
	BinarySHA256      string          `json:"binary_sha256"`
	Origin            string          `json:"origin"`
	Provenance        Provenance      `json:"provenance"`
	RegisteredAt      time.Time       `json:"registered_at"`
	RegisteredBy      string          `json:"registered_by"`
	Deprecated        bool            `json:"deprecated"`
	DeprecatedAt      time.Time       `json:"deprecated_at,omitempty"`
	DeprecationReason string          `json:"deprecation_reason,omitempty"`
}

// QualifiedName returns "namespace/name@version".
func (e ToolRegistryEntry) QualifiedName() string {
	return e.Namespace + "/" + e.Name + "@" + e.Version
}

// ToolRegistryRepository persists the dynamic tool registry. Implementations
// live in store/postgres (production) and cpn/persist (in-memory, for tests).
type ToolRegistryRepository interface {
	// Upsert persists the entry. Implementations MUST reject duplicates on
	// (namespace, name, version) with ErrToolDuplicate.
	Upsert(ctx context.Context, entry ToolRegistryEntry) error

	// Get resolves a qualified name. The form is either "namespace/name"
	// (latest non-deprecated) or "namespace/name@version" (exact).
	Get(ctx context.Context, qualifiedName string) (ToolRegistryEntry, error)

	// GetVersion fetches a specific (namespace, name, version).
	GetVersion(ctx context.Context, namespace, name, version string) (ToolRegistryEntry, error)

	// ListByNamespace returns every version persisted under the given namespace,
	// including deprecated rows.
	ListByNamespace(ctx context.Context, namespace string) ([]ToolRegistryEntry, error)

	// ListAll returns every persisted row, including deprecated rows.
	ListAll(ctx context.Context) ([]ToolRegistryEntry, error)

	// Deprecate flags a specific (qualified name WITH version) as deprecated.
	Deprecate(ctx context.Context, qualifiedName, reason string) error

	// Latest returns the highest-version non-deprecated entry for a
	// (namespace, name) pair.
	Latest(ctx context.Context, namespace, name string) (ToolRegistryEntry, error)

	// Delete hard-deletes a qualified name. Only callers who know the
	// origin is agent-authored should invoke this.
	Delete(ctx context.Context, qualifiedName string) error
}

// Sentinel errors for the tool registry repository. The cpn/tools package
// wraps these into its own RegistryError type before surfacing to callers.
var (
	// ErrToolDuplicate is returned when persisting an entry whose
	// (namespace, name, version) triple collides with an existing row.
	ErrToolDuplicate = errors.New("persist: tool already registered")

	// ErrToolNotFound is returned when a qualified-name lookup misses.
	ErrToolNotFound = errors.New("persist: tool not found")
)
