package cpn

import (
	"context"
	"encoding/json"
	"time"
)

// ToolRegistry is the minimal port the CPN runtime uses to publish new tools
// (NodeKindRegisterTool) and resolve existing ones (NodeKindTool). The
// concrete implementation lives in cpn/tools and carries the full GAP-3
// surface (bootstrap, list, deprecate, admin reads); the port here captures
// only what the executor needs so the cpn package stays adapter-free.
type ToolRegistry interface {
	// RegisterManifest persists the given manifest and returns the
	// resulting qualified name and UUID. The implementation wraps its
	// own ToolEntry; we keep the port payload-shaped to avoid a cross-
	// package type dependency.
	RegisterManifest(ctx context.Context, manifest ToolManifest) (ToolManifestResult, error)
}

// ToolManifest is the struct form of RegisterToolConfig the executor passes
// to the registry. It mirrors the RegisterToolConfig fields verbatim (see
// spec §4).
type ToolManifest struct {
	Namespace    string             `json:"namespace"`
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Schema       json.RawMessage    `json:"schema"`
	HelpText     string             `json:"help_text"`
	ManPage      string             `json:"man_page"`
	BinaryPath   string             `json:"binary_path"`
	BinarySHA256 string             `json:"binary_sha256"`
	Origin       string             `json:"origin"`
	Provenance   ProvenanceSnapshot `json:"provenance"`
	RegisteredBy string             `json:"registered_by"`

	// Kind classifies the manifest source. Empty for historical builtin/
	// agent-authored manifests; "synthesized" marks outputs of the SC-12
	// tool-synthesis sub-CPN (REQ-1201).
	Kind string `json:"kind,omitempty"`

	// Toolbox is the named domain grouping (spec-architecture-brae-toolbox-
	// taxonomy.md REQ-002). Empty defaults to Namespace at registration time.
	Toolbox string `json:"toolbox,omitempty"`
	// Hashtags is the controlled-vocabulary capability set (REQ-001). Tokens
	// MUST be pre-normalised (REQ-NORM-001); unknown tokens are dropped at
	// registration and surface as a lexicon.entry.dropped event (REQ-007).
	Hashtags []string `json:"hashtags,omitempty"`
}

// ProvenanceSnapshot is the cpn-package mirror of persist.Provenance. Split
// off the persist package to avoid pulling persist into cpn.
type ProvenanceSnapshot struct {
	AuthoringCPNID string `json:"authoring_cpn_id,omitempty"`
	FlowHash       string `json:"flow_hash,omitempty"`
	ForgeRunID     string `json:"forge_run_id,omitempty"`
	PromptDigest   string `json:"prompt_digest,omitempty"`
	SourcePath     string `json:"source_path,omitempty"`
	SourceSHA256   string `json:"source_sha256,omitempty"`
}

// ToolManifestResult is the return shape of RegisterManifest. The qualified
// name is the canonical "ns/name@ver" string the executor deposits on the
// ColorArtifact output place.
type ToolManifestResult struct {
	QualifiedName string    `json:"qualified_name"`
	ID            string    `json:"id"`
	RegisteredAt  time.Time `json:"registered_at"`
}

// RegisterToolConfig is the per-transition manifest for NodeKindRegisterTool
// (spec §4). Matches ToolManifest field-for-field; kept as a separate type so
// topology authors can reason about config independently of the runtime port.
type RegisterToolConfig struct {
	Namespace    string
	Name         string
	Version      string
	Schema       json.RawMessage
	HelpText     string
	ManPage      string
	BinaryPath   string
	BinarySHA256 string
	Origin       string
	Provenance   ProvenanceSnapshot
	RegisteredBy string

	// Toolbox + Hashtags mirror ToolManifest (taxonomy spec REQ-001/002).
	Toolbox  string
	Hashtags []string
}

// ToManifest converts the transition config into the runtime manifest.
func (c *RegisterToolConfig) ToManifest() ToolManifest {
	if c == nil {
		return ToolManifest{}
	}
	return ToolManifest{
		Namespace:    c.Namespace,
		Name:         c.Name,
		Version:      c.Version,
		Schema:       c.Schema,
		HelpText:     c.HelpText,
		ManPage:      c.ManPage,
		BinaryPath:   c.BinaryPath,
		BinarySHA256: c.BinarySHA256,
		Origin:       c.Origin,
		Provenance:   c.Provenance,
		RegisteredBy: c.RegisteredBy,
		Toolbox:      c.Toolbox,
		Hashtags:     c.Hashtags,
	}
}

// FirstRunLedger is the GAP-6 port consulted before a binary-backed
// manifest hits the registry. Dev builds leave this nil; the executor
// proceeds with a warning (fall-open per REQ-034).
type FirstRunLedger interface {
	// Approve MUST return nil if and only if the (path, sha256) pair is
	// approved for first-run execution. A non-nil error aborts the
	// register_tool firing and routes the error to the ErrorPlace.
	Approve(ctx context.Context, binaryPath, binarySHA256 string) error
}
