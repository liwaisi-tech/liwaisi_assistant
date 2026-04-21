// Package toolsynth implements SC-12: the Tool-Synthesis Sub-CPN. It
// consumes HelpSchema records emitted by SC-11 (cpn/awakens/helpparse) and
// produces synthesized ToolManifests materialised into a PendingToolStore —
// never auto-registered (REQ-1203). Downstream SC-13 gates activation via
// HITL approval before the manifest reaches cpn/tools.Registry.
//
// SC-12 is library-only: Compose is consumed by a later wiring chunk, not
// the root awakening topology.
package toolsynth

import (
	"errors"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

const (
	// KindSynthesized marks a manifest produced by toolsynth (REQ-1201).
	KindSynthesized = "synthesized"
	// OriginHelpParser is the origin label on synthesized manifests
	// (REQ-1201).
	OriginHelpParser = "brae-awakens/help-parser"
	// DefaultVersion is the initial version stamped on synthesized
	// manifests when the help-schema supplies none.
	DefaultVersion = "0.0.1"
	// DefaultNamespace groups synthesized manifests under a distinct
	// namespace so operators can audit what the help-parser produced.
	DefaultNamespace = "synthesized"
)

const (
	PlaceSchemaPrefix   = "p-synth-schema-"
	PlacePendingPrefix  = "p-synth-pending-"
	PlaceStagedPrefix   = "p-synth-staged-"
	TxMaterialisePrefix = "t-synth-materialise-"
	TxStagePrefix       = "t-synth-stage-"
)

// SynthesisInput wraps a single HelpSchema for the materialise transition.
type SynthesisInput struct {
	Schema helpparse.HelpSchema
}

// SynthesisOutput is the materialise-stage token before staging.
type SynthesisOutput struct {
	Manifest         cpn.ToolManifest
	ProvenanceSHA256 string
}

// PendingTool is the staged record awaiting SC-13 approval.
type PendingTool struct {
	Manifest         cpn.ToolManifest `json:"manifest"`
	SourceSHA256     string           `json:"source_sha256"`
	ProvenanceSHA256 string           `json:"provenance_sha256"`
	SessionID        string           `json:"session_id,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// SynthesisResult is the per-branch aggregator input. Success carries
// PendingTool; failure carries Err + Binary so the reducer can filter and
// the caller can surface a failure event.
type SynthesisResult struct {
	Binary  string
	Pending PendingTool
	Err     string
}

// ErrEmptyBinary is returned by SynthesiseManifest when the schema has no
// usable binary name (REQ-1205 pre-condition).
var ErrEmptyBinary = errors.New("toolsynth: help-schema has empty binary")

// ErrMalformedFlag is returned when a flag lacks a long name.
var ErrMalformedFlag = errors.New("toolsynth: flag missing long name")
