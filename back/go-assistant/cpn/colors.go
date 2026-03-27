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
)
