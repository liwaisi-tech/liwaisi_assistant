package cpn

import (
	"sync"
	"sync/atomic"
	"time"
)

// CPN is the universal agent type.
// At Depth=0: root coordinator. At Depth=1: domain coordinator. At Depth=2+: worker.
// A CPN contains places and transitions that form a Coloured Petri Net.
type CPN struct {
	ID    string
	Role  string
	Depth int

	Mode        Mode
	initialMode Mode
	State       State
	Error       error

	Places      map[string]*Place
	Transitions map[string]*Transition

	SessionID string

	// RegionalVariant is the BCP-47 tag for the session's user. Loaded once
	// per session by the session service and read by fireLLM to render a
	// per-session prompt preamble (CON-003: never mutates Transition state).
	// Empty means "use language default" — the renderer falls back globally.
	RegionalVariant string

	// LLMClient is the LLM API client shared by all LLM transitions.
	LLMClient LLMClient

	// HostRuntime aggregates the collaborators used by NodeKindBash
	// transitions (HostAdapter, HostGate, BashSessionManager). Nil is
	// legal for topologies that never fire a bash transition; the
	// executor returns a descriptive error when a bash transition
	// fires without a runtime.
	HostRuntime *HostRuntime

	// History holds the conversation history for context assembly.
	History []*Message

	// ContextWindowSize is the max conversational turns for the sliding window.
	// Default: DefaultContextWindowSize (10).
	ContextWindowSize int

	// Group manages this CPN's sub-CPNs (ephemeral team).
	// Nil for leaf CPNs that do not spawn sub-nets.
	Group *GroupAgent

	// EventSink receives events emitted by this CPN.
	// Called synchronously by emit(). Nil means events are only sent to EventEmitter.
	// Decouples from concrete event bus (CON-004).
	EventSink func(*Event)

	// GroupNotifier is called on state changes that affect group membership.
	// Nil means no group coordination (standalone CPN or testing).
	// Implemented by GroupAgent (Block 11).
	GroupNotifier GroupNotifier

	// Metrics records execution metrics. If nil, no metrics are collected (REQ-015).
	Metrics *MetricsRecorder

	// Cost provides session cost data for ranking. If nil, cost defaults to 0 (REQ-016).
	Cost CostProvider

	// EventEmitter is the channel where this CPN's events are sent.
	// Read by the parent CPN's observer transitions and GroupAgent.Deliver.
	// Nil if this CPN is not a sub-CPN.
	EventEmitter chan<- Event

	// subNetBuses maps child CPN IDs to their event bus channels.
	// Used by Observer transitions (Block 13) to drain child events.
	subNetBuses map[string]<-chan Event
	subNetMu    sync.Mutex

	// selfBus carries events originating from THIS CPN's own transitions
	// (e.g. NodeKindBash process events) so that NodeKindObserver
	// transitions in the same CPN can react to them via drainObservers.
	// Lazy-initialised by selfEventBus() when an observer transition is
	// present or when fire_bash first publishes a process event.
	// Capacity is selfEventBusCapacity.
	selfBus chan Event
	selfMu  sync.Mutex

	// childWg tracks active child goroutines for graceful shutdown.
	childWg sync.WaitGroup

	// activeChildren counts running child goroutines.
	// Used by Run to avoid false deadlock when children are still producing tokens.
	activeChildren atomic.Int32

	// StreamedOutput is set to true when an EventStreamChunk is emitted
	// during execution. Used by the caller to determine whether output
	// was already delivered via streaming (avoiding duplicate delivery).
	StreamedOutput bool

	// OnHITLWaiting, if non-nil, is called just before a HITL transition
	// blocks on human input. It receives a snapshot of c.History at that
	// moment so the caller can flush accumulated messages to persistence.
	// This closes the gap where messages streamed during a CPN run are
	// lost if the user disconnects while HITL is pending (the normal
	// persistAfterRun path only fires after Run returns).
	OnHITLWaiting func(historySnapshot []*Message)

	// OnHistoryChanged, if non-nil, is called by the executor after each
	// batch of transition firings completes (post-wg.Wait). It receives a
	// snapshot of c.History so the caller can flush newly accumulated
	// messages to persistence. This covers ALL transition types — LLM
	// streaming, tool calls, subnet completion — not just HITL.
	OnHistoryChanged func(historySnapshot []*Message)

	// SeedFunc, if non-nil, deposits the topology's initial marking —
	// tokens that must be present for the net's guards/arcs to be well-
	// formed even on a fresh run (e.g. a counter place whose absence would
	// deadlock a downstream transition). Invoked by Reset after Clear so
	// seeded places survive session re-runs. Topologies with no mandatory
	// initial marking leave this nil.
	SeedFunc func(*CPN)

	// ToolRegistry is the runtime-mutable tool registry (GAP-3). Wired at
	// boot so NodeKindRegisterTool transitions can publish new tools. Nil
	// is legal for legacy topologies that never fire a register_tool
	// transition.
	ToolRegistry ToolRegistry

	// FirstRunLedger, when non-nil, is consulted by NodeKindRegisterTool
	// before a binary-backed manifest hits the registry (GAP-6 first-run
	// HITL). Dev builds leave it nil; fire_register_tool falls open with
	// a WARN log per REQ-034.
	FirstRunLedger FirstRunLedger

	// FlowRepository, when non-nil, stores agent-authored CPN topologies
	// produced by NodeKindSynthesize and re-loaded by NodeKindInstantiate
	// (GAP-4). Nil is legal for topologies that never synth/instantiate;
	// those transitions fail at dispatch time with a descriptive error.
	FlowRepository AuthoredFlowRepository

	// SafeRegistry is the compile-time catalogue of primitives that
	// agent-authored topologies may reference (GAP-4 REQ-030). Consulted
	// by the linter and by the instantiation materialiser so the child
	// CPN resolves names against a sealed set.
	SafeRegistry SafeRegistryPort

	// TopologyRouter is the HITL router used by NodeKindInstantiate to
	// surface first-instantiation approval prompts (GAP-4 REQ-050). Nil
	// falls open (warning log) so dev mode does not block on approvals —
	// same pattern GAP-3 uses for FirstRunLedger.
	TopologyRouter TopologyHITLRouter

	// ToolboxCatalog, when non-nil, renders a prompt-ready catalogue of
	// the host's toolboxes + their tools. Consumed by fire_synthesize to
	// teach the LLM which tool IDs it can reference inside an authored
	// sub-CPN topology. Nil means "no toolbox hints" — the synthesizer
	// falls back to the SafeRegistry catalogue alone.
	ToolboxCatalog ToolboxCatalogPort

	// approvedFlows records flow_ids that the user has already approved
	// during this session (REQ-051). Subsequent instantiations skip HITL.
	// Allocated lazily under mu on first approval.
	approvedFlows map[string]struct{}

	// HostContextFormatter, when non-nil, produces a host-capability preamble
	// that renderSystemPrompt prepends to every LLM system prompt (GAP-11
	// REQ-012). Set at session-creation time by the composition root so the
	// cpn package stays free of import cycles with cpn/persist.
	HostContextFormatter func(c *CPN) string

	// MutableAfterStart enables GAP-7 hot topology reconfiguration.
	// Defaults to false; topology mutations are rejected unless this is set.
	MutableAfterStart bool

	// TrustMutations skips HITL gating for topology mutations (dev-mode / internal agents).
	// Only honoured when MutableAfterStart is also true.
	TrustMutations bool

	// MutationLog, when non-nil, persists every topology mutation attempt
	// (approved and rejected) to the audit store (GAP-7 REQ-005).
	MutationLog MutationAuditLog

	// RunProvenance, when set before Run(), stamps the current execution
	// with the library FlowID + architect Strategy that produced it
	// (spec-architecture-cpn-agent-architect §11 slice 4). Purely additive
	// metadata — runs without provenance keep the zero value and behave
	// identically to legacy executions.
	RunProvenance RunProvenance

	mu sync.RWMutex
}

// RunProvenance is the architect-lane metadata stamped on an ExecutionRecord
// so downstream consumers (ranking, observability, later-slice learning) know
// which library entry or synthesis strategy produced the run.
type RunProvenance struct {
	FlowID   string
	Strategy string
}

// SetRunProvenance is the setter used by the integration layer (session
// service or architect transition) immediately before c.Run. Equivalent to
// direct assignment on RunProvenance; kept as a method so test doubles and
// future validation (e.g., reject unknown strategies) have a single hook.
func (c *CPN) SetRunProvenance(flowID, strategy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.RunProvenance = RunProvenance{FlowID: flowID, Strategy: strategy}
}

// NewCPN creates a CPN in StateIdle. Call Run(ctx) to start execution.
// Validation is performed at the start of Run, not at construction.
func NewCPN(id, role string, depth int, mode Mode, sessionID string,
	places map[string]*Place, transitions map[string]*Transition) *CPN {
	return &CPN{
		ID:          id,
		Role:        role,
		Depth:       depth,
		Mode:        mode,
		initialMode: mode,
		State:       StateIdle,
		Places:      places,
		Transitions: transitions,
		SessionID:   sessionID,
	}
}

// setState sets the CPN state under write lock.
func (c *CPN) setState(s State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = s
}

// setFailed atomically sets State=StateFailed and Error under a single write lock.
// Prevents concurrent readers from observing StateFailed with a nil Error.
func (c *CPN) setFailed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = StateFailed
	c.Error = err
}

// getState returns the CPN state under read lock.
func (c *CPN) getState() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.State
}

// getError returns the CPN error under read lock.
// Pairs with setFailed which writes Error under write lock.
func (c *CPN) getError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Error
}

// TerminalPlaces returns places not referenced as inputs to any transition.
// These are the "output" places of the CPN.
func (c *CPN) TerminalPlaces() []*Place {
	inputRefs := make(map[string]bool)
	for _, t := range c.Transitions {
		for _, pid := range t.InputPlaces {
			inputRefs[pid] = true
		}
	}

	var terminals []*Place
	for id, p := range c.Places {
		if !inputRefs[id] {
			terminals = append(terminals, p)
		}
	}
	return terminals
}

// selfEventBusCapacity bounds the self-bus so a stuck observer loop never
// starves the executor. Dropped events are reflected via the
// ProcessEventMetrics port when one is wired (GAP-9 REQ-003).
const selfEventBusCapacity = 256

// emit sends an event, stamping CPN identity and timestamp.
// REQ-012: Stamps CPNID, CPNDepth, CPNRole, SessionID, Timestamp on the event.
// Calls EventSink synchronously (if non-nil), then sends to EventEmitter (non-blocking).
// No-op if both EventSink and EventEmitter are nil.
//
// Process events (EventProcessStarted/Stdout/Stderr/Exit) additionally
// fan out onto the per-CPN self-bus so that NodeKindObserver transitions
// in the SAME CPN pick them up via drainObservers. The fan-out is
// strictly non-blocking (GAP-9 REQ-003) — a saturated self-bus drops the
// event and the caller's metrics port is bumped by the fire_bash /
// session_manager layer.
func (c *CPN) emit(e *Event) {
	e.CPNID = c.ID
	e.CPNDepth = c.Depth
	e.CPNRole = c.Role
	e.SessionID = c.SessionID
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	if c.EventSink != nil {
		c.EventSink(e)
	}

	if c.EventEmitter != nil {
		select {
		case c.EventEmitter <- *e:
		default:
		}
	}

	if isProcessEvent(e.Type) {
		bus := c.selfEventBus()
		select {
		case bus <- *e:
		default:
			// Self-bus is saturated: observer isn't draining fast
			// enough. Report the drop via the metrics port so
			// AC-005 accounting stays accurate.
			if c.HostRuntime != nil && c.HostRuntime.Metrics != nil {
				var sid string
				if pe, ok := e.Payload.(ProcessOutputPayload); ok {
					sid = pe.SessionID
				}
				c.HostRuntime.Metrics.OnDrop(sid, e.Type, "self_bus_full")
			}
		}
	}
}

// PublishProcessEvent is the injection point used by the
// BashSessionManager (GAP-9 REQ-002) to stream process events from a
// PTY read loop into THIS CPN's observer drain path. It applies the
// same rate-limit / metrics accounting as fire_bash.go and then hands
// the event to emit() — which fans it out to EventSink, EventEmitter,
// and the self-bus.
//
// The contract is strictly non-blocking: a saturated bus or a refused
// rate-limiter vote drops the event and bumps the drop counter.
// Callers MUST NOT rely on delivery for correctness.
func (c *CPN) PublishProcessEvent(kind EventType, payload ProcessOutputPayload) {
	if c == nil {
		return
	}

	var (
		limiter ProcessEventRateLimiter = AllowAllRateLimiter{}
		metrics ProcessEventMetrics     = NoOpProcessEventMetrics{}
	)
	if c.HostRuntime != nil {
		if c.HostRuntime.RateLimiter != nil {
			limiter = c.HostRuntime.RateLimiter
		}
		if c.HostRuntime.Metrics != nil {
			metrics = c.HostRuntime.Metrics
		}
	}

	if !limiter.Allow(payload.SessionID) {
		metrics.OnDrop(payload.SessionID, kind, "rate_limited")
		return
	}

	c.emit(&Event{
		Type:           kind,
		TransitionKind: NodeKindBash,
		Payload:        payload,
	})
	metrics.OnEmit(payload.SessionID, kind)
}

// PublishHostApprovalSurface emits the A2UI host.approval surface as a
// terminal StreamChunk on the CPN's event bus so the frontend
// HostApprovalCard can render the extended-action buttons. The content
// MUST already carry the A2UIMarker prefix.
//
// The emit is synchronous w.r.t. the sink and non-blocking on the event
// emitter channel, mirroring the invariants enforced by the other
// publish-* helpers on this type.
func (c *CPN) PublishHostApprovalSurface(transitionID, content string) {
	if c == nil {
		return
	}
	c.emit(&Event{
		Type:           EventStreamChunk,
		TransitionID:   transitionID,
		TransitionKind: NodeKindBash,
		Payload: StreamChunk{
			SessionID: c.SessionID,
			CPNID:     c.ID,
			CPNRole:   c.Role,
			Content:   content,
			Done:      true,
		},
	})
}

// PublishHITLRequested emits the typed EventHITLRequested envelope bound
// to transitionID. Used by the gate's SessionHITLRouter so the
// integration layer can suppress any legacy review-card fallback when the
// custom host.approval surface has already been published.
func (c *CPN) PublishHITLRequested(transitionID string, payload HITLRequestedPayload) {
	if c == nil {
		return
	}
	c.emit(&Event{
		Type:           EventHITLRequested,
		TransitionID:   transitionID,
		TransitionKind: NodeKindBash,
		Payload:        payload,
	})
}

// SessionEventSink returns an Event sink bound to this CPN for use with
// PTYRequest.EventSink. It wraps the Event into the payload expected by
// PublishProcessEvent so adapters can emit pre-shaped Event values
// without knowing about the ProcessOutputPayload detail.
//
// The returned closure is safe for concurrent use and never blocks.
func (c *CPN) SessionEventSink() func(Event) {
	return func(e Event) {
		payload, _ := e.Payload.(ProcessOutputPayload)
		c.PublishProcessEvent(e.Type, payload)
	}
}

// selfEventBus lazily creates (on first call) and returns the CPN-local
// event bus used to feed drainObservers. Safe for concurrent use.
func (c *CPN) selfEventBus() chan Event {
	c.selfMu.Lock()
	defer c.selfMu.Unlock()
	if c.selfBus == nil {
		c.selfBus = make(chan Event, selfEventBusCapacity)
	}
	return c.selfBus
}

// isProcessEvent reports whether the event kind is one of the four
// process-lifecycle kinds fan-out to the self-bus.
func isProcessEvent(k EventType) bool {
	switch k {
	case EventProcessStarted, EventProcessStdout, EventProcessStderr, EventProcessExit:
		return true
	}
	return false
}

// switchModeToCentaurian sets Mode to ModeCentaurian and emits EventModeSwitch.
// REQ-013: Called when a human token enters a SpaceComputation place via HITL.
func (c *CPN) switchModeToCentaurian() {
	c.setMode(ModeCentaurian)
}

// setMode is the internal helper that handles mode transitions with event emission.
// Emits EventModeSwitch with map{"from": old, "to": new} payload.
// No-op if the mode is already set to the target value.
func (c *CPN) setMode(newMode Mode) {
	c.mu.Lock()
	old := c.Mode
	if old == newMode {
		c.mu.Unlock()
		return
	}
	c.Mode = newMode
	c.mu.Unlock()

	c.emit(&Event{
		Type:    EventModeSwitch,
		Payload: map[string]Mode{"from": old, "to": newMode},
	})
}

// SetMode explicitly sets the CPN mode. Thread-safe.
// Allows parent CPNs to override mode regardless of token state.
func (c *CPN) SetMode(mode Mode) {
	c.setMode(mode)
}

// getMode returns the CPN mode under read lock.
func (c *CPN) getMode() Mode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Mode
}

// isComputationTransition returns true if any output place is SpaceComputation.
func (c *CPN) isComputationTransition(t *Transition) bool {
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if ok && p.Space == SpaceComputation {
			return true
		}
	}
	return false
}

// effectiveGuard returns the guard to use for a transition considering mode.
// In ModeCentaurian + computation transition: injects centaurianGuard.
// Composes with custom guard if one exists (AND semantics).
// In ModeMAS or for non-computation transitions: returns t.Guard unchanged.
//
// GUD-001: NodeKindHITL is exempt from centaurianGuard — HITL transitions
// are the mechanism for PRODUCING human tokens. Requiring a human token
// to fire would create a deadlock (the token doesn't exist until HITL fires).
// This matches the Validate exemption in validate.go.
func (c *CPN) effectiveGuard(t *Transition) func([]*Token) bool {
	if c.getMode() != ModeCentaurian {
		return t.Guard
	}
	if t.Kind == NodeKindHITL {
		return t.Guard
	}
	if !c.isComputationTransition(t) {
		return t.Guard
	}

	// Computation transition in Centaurian mode: inject centaurianGuard.
	if t.Guard == nil {
		return centaurianGuard
	}

	// Compose: both centaurian AND custom guard must pass.
	custom := t.Guard
	return func(tokens []*Token) bool {
		return centaurianGuard(tokens) && custom(tokens)
	}
}

// effectiveCanFire evaluates whether a transition can fire in the current mode.
// Replaces direct t.CanFire(c.Places) calls in the executor.
func (c *CPN) effectiveCanFire(t *Transition) bool {
	return t.canFireWith(c.Places, c.effectiveGuard(t))
}

// checkModeSwitch evaluates mode transition rules after each firing round.
//
// MAS → Centaurian: when any SpaceComputation place contains a human-origin token.
// Centaurian → MAS: when NO SpaceComputation place contains human-origin tokens
// AND no HITL transition is firable.
func (c *CPN) checkModeSwitch() {
	hasHumanInComputation := false
	for _, p := range c.Places {
		if p.Space != SpaceComputation {
			continue
		}
		tokens, ok := p.Peek()
		if !ok {
			continue
		}
		for _, tok := range tokens {
			if tok.IsHumanOrigin() {
				hasHumanInComputation = true
				break
			}
		}
		if hasHumanInComputation {
			break
		}
	}

	currentMode := c.getMode()

	if currentMode == ModeMAS && hasHumanInComputation {
		c.setMode(ModeCentaurian)
		return
	}

	if currentMode == ModeCentaurian && !hasHumanInComputation {
		// Check if any HITL transition is firable — if so, stay in Centaurian.
		for _, t := range c.Transitions {
			if t.Kind == NodeKindHITL && t.CanFire(c.Places) {
				return
			}
		}
		c.setMode(ModeMAS)
	}
}

// registerSubNetBus registers a child's event bus channel for observer draining.
// Lazy-initializes the subNetBuses map. Thread-safe.
func (c *CPN) registerSubNetBus(childID string, bus <-chan Event) {
	c.subNetMu.Lock()
	defer c.subNetMu.Unlock()
	if c.subNetBuses == nil {
		c.subNetBuses = make(map[string]<-chan Event)
	}
	c.subNetBuses[childID] = bus
}

// Reset clears all places' token buffers and resets state to StateIdle.
// Called before re-running a CPN that previously failed (e.g., after HITL rejection)
// to prevent stale tokens from interfering with the next execution.
// Also clears History since the caller re-populates it from the session.
//
// After clearing, SeedFunc (if set) is invoked to restore the topology's
// initial marking. Without this, seeded counter places (e.g. p-round in
// the unified topology) would stay empty after Reset, silently deadlocking
// any downstream transition whose input arc includes them.
func (c *CPN) Reset() {
	c.mu.Lock()
	c.State = StateIdle
	c.Error = nil
	c.Mode = c.initialMode
	c.History = nil
	c.StreamedOutput = false
	seed := c.SeedFunc
	c.mu.Unlock()

	for _, p := range c.Places {
		p.Clear()
	}
	if seed != nil {
		seed(c)
	}
}

// HasApprovedFlow reports whether the user has already approved the given
// flow_id during this session (GAP-4 REQ-051). Thread-safe.
func (c *CPN) HasApprovedFlow(flowID string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.approvedFlows[flowID]
	return ok
}

// MarkFlowApproved records the given flow_id as approved for this session
// so subsequent NodeKindInstantiate transitions skip HITL (GAP-4 REQ-051).
// Thread-safe; idempotent.
func (c *CPN) MarkFlowApproved(flowID string) {
	if c == nil || flowID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approvedFlows == nil {
		c.approvedFlows = make(map[string]struct{})
	}
	c.approvedFlows[flowID] = struct{}{}
}

// IsComplete returns true when ALL terminal places have at least one token.
// Returns false if there are no terminal places.
func (c *CPN) IsComplete() bool {
	terminals := c.TerminalPlaces()
	if len(terminals) == 0 {
		return false
	}
	for _, p := range terminals {
		if p.Len() == 0 {
			return false
		}
	}
	return true
}
