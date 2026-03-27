package cpn

import "context"

// Transition represents a CPN transition — a unit of computation that fires
// when its input places contain tokens and its guard condition is satisfied.
//
// This struct is extended incrementally by later blocks. Block 3 defines
// the core identity and firing rule fields. Fields for retry, LLM, validation,
// sub-nets, observers, and HITL are added in their respective blocks.
type Transition struct {
	// ID is the unique identifier for this transition within the CPN.
	ID string

	// Kind classifies the transition's behavior when it fires.
	// See NodeKind constants: NodeKindTool, NodeKindLLM, etc.
	Kind NodeKind

	// InputPlaces lists the IDs of places this transition reads from.
	// CanFire checks that all input places have at least one token.
	InputPlaces []string

	// OutputPlaces lists the IDs of places this transition writes to
	// after successful execution.
	OutputPlaces []string

	// ErrorPlace is the fallback output place for error tokens.
	// Used by the executor when retry is exhausted (Block 4+).
	// Empty string means no error routing — failure propagates.
	ErrorPlace string

	// Guard is an optional predicate evaluated on the aggregated tokens
	// from all input places. If nil, the transition fires whenever all
	// input places have tokens. Guards should be pure functions with
	// no side effects.
	Guard func(tokens []*Token) bool

	// Retry configures retry behavior when this transition's execution fails.
	// If nil, no retry — a single failure is final.
	Retry *RetryPolicy

	// cbState holds the runtime circuit breaker state.
	// Initialized by the CPN when Retry.CircuitBreaker is configured.
	cbState *CircuitBreakerState

	// SystemPrompt is the static system prompt for NodeKindLLM transitions.
	// Assembled into T1 tier by BuildContext.
	// Should not change between calls — dynamic prompts make behavior unpredictable.
	SystemPrompt string

	// ToolName is the name of the tool this transition invokes.
	// Only meaningful when Kind == NodeKindTool.
	ToolName string

	// Executor is called when this tool transition fires.
	// Receives context and the first consumed input token.
	// Returns a result token deposited into OutputPlaces.
	// Only used when Kind == NodeKindTool.
	Executor func(ctx context.Context, in Token) (Token, error)

	// LLMConfig holds per-transition model selection and budget.
	// Only meaningful when Kind == NodeKindLLM. Nil for other kinds.
	LLMConfig *LLMConfig

	// LLMTools lists Transition IDs (in the same CPN) the LLM may invoke as tools.
	// Only meaningful when Kind == NodeKindLLM.
	LLMTools []string

	// ValidateConfig holds per-transition validation configuration.
	// Only meaningful when Kind == NodeKindValidate. Nil for other kinds.
	ValidateConfig *ValidateConfig

	// SubNet is a static CPN topology template (for crystallized/reusable flows).
	// Used when SubNetFactory is nil. cloneCPN creates a fresh instance per firing.
	// Only meaningful when Kind == NodeKindSubNet.
	SubNet *CPN

	// SubNetFactory creates a new CPN instance per firing (prototype + fork).
	// Takes precedence over SubNet when both are set.
	// Only meaningful when Kind == NodeKindSubNet.
	SubNetFactory func() *CPN
}

// SetCircuitBreaker sets the runtime circuit breaker state.
func (t *Transition) SetCircuitBreaker(cb *CircuitBreakerState) {
	t.cbState = cb
}

// CircuitBreaker returns the circuit breaker state, or nil.
func (t *Transition) CircuitBreaker() *CircuitBreakerState {
	return t.cbState
}

// NewTransition creates a Transition with the given identity and arc configuration.
// Guard and ErrorPlace can be set after construction via direct field assignment.
func NewTransition(id string, kind NodeKind, inputPlaces, outputPlaces []string) *Transition {
	return &Transition{
		ID:           id,
		Kind:         kind,
		InputPlaces:  inputPlaces,
		OutputPlaces: outputPlaces,
	}
}

// CanFire returns true if all input places have at least one token
// and the guard condition (if set) is satisfied.
//
// Returns false if:
//   - InputPlaces is empty
//   - Any input place ID is not found in the places map
//   - Any input place has zero tokens
//   - The Guard function returns false
//
// CanFire does not modify any place state — it uses Peek (read-only).
// Safe for concurrent calls (Place.Peek is mutex-protected).
func (t *Transition) CanFire(places map[string]*Place) bool {
	// Circuit breaker check (Block 4).
	if t.cbState != nil && !t.cbState.Allow() {
		return false
	}

	if len(t.InputPlaces) == 0 {
		return false
	}

	var tokens []*Token
	for _, pid := range t.InputPlaces {
		p, ok := places[pid]
		if !ok {
			return false
		}
		ts, ok := p.Peek()
		if !ok {
			return false
		}
		tokens = append(tokens, ts...)
	}

	if t.Guard != nil {
		return t.Guard(tokens)
	}
	return true
}
