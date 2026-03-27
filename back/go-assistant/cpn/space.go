package cpn

// SpaceKind identifies the communication layer a place belongs to.
// Tokens cannot cross space boundaries without explicit arc routing.
type SpaceKind string

const (
	// SpaceSurface is the user-facing layer for prompts, responses, and artifacts.
	SpaceSurface SpaceKind = "surface"

	// SpaceObservation carries events emitted by sub-CPNs for observer transitions.
	SpaceObservation SpaceKind = "observation"

	// SpaceComputation is the internal layer for intermediate results and tool outputs.
	SpaceComputation SpaceKind = "computation"
)
