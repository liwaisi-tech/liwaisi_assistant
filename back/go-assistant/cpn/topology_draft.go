package cpn

// TopologyDraft is the in-memory pre-canonical representation produced by
// the JIT composer before JSON serialisation. Kept in cpn/ so neither
// cpn/synthesis/jit nor persist needs to cross-reference (Axiom A13).
type TopologyDraft struct {
	ID             string
	Role           string
	Metadata       map[string]string
	Places         []PlaceSpec
	Transitions    []TransitionSpec
	InitialMarking []MarkingSpec
}

// PlaceSpec mirrors persist.PlaceTopology by shape.
type PlaceSpec struct {
	ID    string
	Color string
	Space string
}

// TransitionSpec is the draft form of a transition. The composer populates
// ToolName for NodeKindTool transitions and ExecutorFunc for observer /
// compute transitions. Inputs / Outputs name places by ID.
type TransitionSpec struct {
	ID           string
	Kind         string
	Inputs       []string
	Outputs      []string
	ToolName     string
	ExecutorFunc string
}

// MarkingSpec seeds the initial marking of a place.
type MarkingSpec struct {
	Place string
	Count int
}
