package cpn

// ColorSet classifies the type of data a token carries.
// Used by Place to enforce type constraints on deposited tokens.
type ColorSet string

const (
	// ColorString represents raw text, queries, and prompts.
	ColorString ColorSet = "STRING"

	// ColorJSON represents structured data between transitions.
	ColorJSON ColorSet = "JSON"

	// ColorArtifact represents final deliverables: reports, code, specs.
	ColorArtifact ColorSet = "ARTIFACT"

	// ColorScore represents numeric analysis results.
	ColorScore ColorSet = "SCORE"

	// ColorEvent carries an Event emitted by a sub-CPN.
	// Only valid in SpaceObservation places.
	ColorEvent ColorSet = "EVENT"

	// ColorHuman carries input explicitly provided by a human.
	// Only produced by NodeKindHITL transitions.
	ColorHuman ColorSet = "HUMAN"

	// ColorCPN carries a *CPN reference as a token payload.
	// Used when a CPN instance is passed as data.
	ColorCPN ColorSet = "CPN"

	// ColorSchema represents a JSON schema used by NodeKindValidate.
	ColorSchema ColorSet = "SCHEMA"

	// ColorError represents an error payload for recovery transitions.
	ColorError ColorSet = "ERROR"

	// ColorIdentity carries a Personality state token.
	ColorIdentity ColorSet = "IDENTITY"

	// ColorToolManifest carries a RegisterToolConfig payload destined for a
	// NodeKindRegisterTool transition (GAP-3). Default space is
	// SpaceComputation (REQ-033).
	ColorToolManifest ColorSet = "TOOL_MANIFEST"

	// ColorShellCmd carries a shell command request to be dispatched to a
	// NodeKindBash transition (GAP-1).
	ColorShellCmd ColorSet = "SHELL_CMD"

	// ColorShellChunk carries a single line of streamed stdout/stderr from a
	// NodeKindBash transition (GAP-1).
	ColorShellChunk ColorSet = "SHELL_CHUNK"

	// ColorShellResult carries the terminal result of a bash transition
	// (GAP-1). Payload is a ShellResultPayload.
	ColorShellResult ColorSet = "SHELL_RESULT"

	// ColorHostFact carries a typed fact discovered about the host (GAP-2).
	// Default space is SpaceComputation.
	ColorHostFact ColorSet = "HOST_FACT"

	// ColorProcess carries process-level metadata for long-lived bash
	// sessions (GAP-1).
	ColorProcess ColorSet = "PROCESS"

	// ColorTopology carries a CPN topology document (GAP-4). Payload is a
	// persist.CPNTopology (or json.RawMessage carrying one). Default
	// space is SpaceComputation — topologies never cross into the surface.
	ColorTopology ColorSet = "TOPOLOGY"

	// ColorFlowRef carries a reference to a persisted topology in the
	// FlowRepository (GAP-4). Payload is a FlowRef { FlowID, Summary }.
	// Produced by NodeKindSynthesize, consumed by NodeKindInstantiate.
	ColorFlowRef ColorSet = "FLOW_REF"
)

// IsShellColor reports whether c is one of the shell-family colors
// introduced by GAP-1/2. Used by Validate to enforce that bash transitions
// do not leak their output tokens into unrelated downstream transitions.
func IsShellColor(c ColorSet) bool {
	switch c {
	case ColorShellCmd, ColorShellChunk, ColorShellResult, ColorHostFact, ColorProcess:
		return true
	}
	return false
}
