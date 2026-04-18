package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/prompts"
)

// fallbackDefaultModel is the compile-time safety net used only when the
// model registry is unreachable AND WithDefaultModel was never called. Keep
// it in the domain package so the app layer never imports infra/openrouter.
const fallbackDefaultModel = "google/gemma-4-31b-it"

// rehydrateTimeout bounds the time a single-flight rehydration can block the
// service on a persistence round-trip. Keeps ghost-session lookups bounded so
// a slow Postgres does not stall the handler pool. Tunable if SLOs change.
const rehydrateTimeout = 5 * time.Second

// TopologyFactory builds a CPN topology for a given session ID.
// The returned CPN must have at least one source place (no incoming transitions)
// and one terminal place (no outgoing transitions).
type TopologyFactory func(sessionID string) *cpn.CPN

// SessionInfo is a read-only snapshot of a session's state.
type SessionInfo struct {
	ID        string
	UserID    string
	Channel   cpn.ChannelType
	State     cpn.State
	CreatedAt time.Time
	Messages  []cpn.Message

	// Chat management fields (populated for fork operations).
	Title               string
	ForkedFromSessionID string
	ForkMessageCount    int
}

// SessionService manages session lifecycle and orchestrates CPN execution.
type SessionService struct {
	sessions        map[string]*cpn.Session
	states          map[string]*sessionState
	mu              sync.RWMutex
	llm             cpn.LLMClient
	cost            cpn.CostProvider
	logger          *slog.Logger
	onEvent         func(sessionID string, evt cpn.Event)
	topologyFactory TopologyFactory
	persist         *PersistDeps
	tokenLedger     TokenLedgerReader
	toolRegistry    SessionToolRegistry
	defaultModel    string            // injected by WithDefaultModel; fallback when registry unavailable
	modelRegistry   cpn.ModelRegistry // optional; enables REQ-GATE-001 runtime validation

	// hostRuntime is the GAP-1 host-side collaborator. When non-nil, every
	// created CPN inherits it so NodeKindBash transitions can fire.
	hostRuntime *cpn.HostRuntime

	// hostCapabilityRepo is the GAP-2 snapshot store. When non-nil, every
	// new session seeds its root CPN's p-host-capabilities place from the
	// latest stored snapshot; a cold/stale repo triggers host-discovery-cpn
	// as a sub-CPN before the session proceeds.
	hostCapabilityRepo persist.HostCapabilityRepository

	// hostDiscoveryFactory constructs a host-discovery-cpn when the cache
	// misses. Provided by main.go at wire-up (and overridable in tests).
	hostDiscoveryFactory func(sessionID string) *cpn.CPN

	// hostSnapshotTTL is the freshness window for a cached snapshot before a
	// new discovery run is triggered. Defaults to 24h per spec REQ-021.
	hostSnapshotTTL time.Duration

	// sf serializes concurrent rehydration attempts for the same session id,
	// so N misses on a restart trigger exactly one persistence round-trip
	// (spec REQ-004, AC-004, PAT-001).
	sf singleflight.Group

	// safeRegistry is the GAP-4 sealed catalogue of safe primitives. When
	// non-nil, every root CPN receives it so synthesize / instantiate
	// transitions can resolve references.
	safeRegistry cpn.SafeRegistryPort

	// authoredFlows is the GAP-4 flow repository.
	authoredFlows cpn.AuthoredFlowRepository

	// topologyRouter surfaces first-instantiation HITL approvals.
	topologyRouter cpn.TopologyHITLRouter

	// mutationLog is the GAP-7 audit log for topology mutations. When non-nil,
	// every created CPN inherits it so MutateTopology calls are persisted.
	mutationLog cpn.MutationAuditLog
}

// sessionState tracks the CPN state safely from outside the cpn package.
type sessionState struct {
	mu           sync.RWMutex
	state        cpn.State
	cancel       context.CancelFunc
	streamClosed sync.Once // guards explicit session teardown in CloseStream
}

func (ss *sessionState) set(s cpn.State) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.state = s
}

func (ss *sessionState) get() cpn.State {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.state
}

// SessionServiceOption configures optional SessionService dependencies.
type SessionServiceOption func(*SessionService)

// WithPersistence enables persistence for sessions, events, ledger, flows, and intelligence.
func WithPersistence(deps *PersistDeps) SessionServiceOption {
	return func(s *SessionService) { s.persist = deps }
}

// WithTokenLedger sets the in-memory token ledger reader for cost sync.
func WithTokenLedger(reader TokenLedgerReader) SessionServiceOption {
	return func(s *SessionService) { s.tokenLedger = reader }
}

// WithToolRegistry sets the tool registry for personality injection and tool resolution.
func WithToolRegistry(reg SessionToolRegistry) SessionServiceOption {
	return func(s *SessionService) { s.toolRegistry = reg }
}

// WithDefaultModel sets the fallback model ID used when the ModelRegistry is
// unavailable. Should be called with the product-default from infra/openrouter
// at the composition root (cmd/server/main.go). When not called, the service
// falls back to the compile-time constant fallbackDefaultModel.
func WithDefaultModel(model string) SessionServiceOption {
	return func(s *SessionService) { s.defaultModel = model }
}

// WithModelRegistry wires the DB-backed model registry for runtime validation
// of every resolved model ID. When set, each resolved candidate (from the
// per-role override → preferred → role-default → product-default cascade) is
// checked against REQ-GATE-001 (lifecycle=active AND license approved) and
// falls back to the product default with a WARN log if the gate fails.
func WithModelRegistry(reg cpn.ModelRegistry) SessionServiceOption {
	return func(s *SessionService) { s.modelRegistry = reg }
}

// WithHostRuntime wires the host-side collaborators (HostAdapter, HostGate,
// BashSessionManager) so NodeKindBash transitions can fire (GAP-1). When
// unset, any CPN that contains a bash transition fails at dispatch time
// with a descriptive error.
func WithHostRuntime(rt *cpn.HostRuntime) SessionServiceOption {
	return func(s *SessionService) { s.hostRuntime = rt }
}

// WithHostCapabilityRepo wires the GAP-2 host-discovery snapshot store.
// Callers typically pair this with WithHostDiscoveryFactory so a stale /
// missing snapshot triggers an on-demand discovery run.
func WithHostCapabilityRepo(repo persist.HostCapabilityRepository) SessionServiceOption {
	return func(s *SessionService) { s.hostCapabilityRepo = repo }
}

// WithHostDiscoveryFactory wires the CPN factory used to spawn the
// host-discovery sub-CPN when the snapshot cache misses.
func WithHostDiscoveryFactory(f func(sessionID string) *cpn.CPN) SessionServiceOption {
	return func(s *SessionService) { s.hostDiscoveryFactory = f }
}

// WithHostSnapshotTTL overrides the 24h default from spec REQ-021.
func WithHostSnapshotTTL(d time.Duration) SessionServiceOption {
	return func(s *SessionService) { s.hostSnapshotTTL = d }
}

// WithSafeRegistry wires the GAP-4 safe primitive catalogue onto every
// session CPN so synthesize / instantiate transitions can dispatch.
func WithSafeRegistry(reg cpn.SafeRegistryPort) SessionServiceOption {
	return func(s *SessionService) { s.safeRegistry = reg }
}

// WithAuthoredFlowRepository wires the GAP-4 flow repository.
func WithAuthoredFlowRepository(repo cpn.AuthoredFlowRepository) SessionServiceOption {
	return func(s *SessionService) { s.authoredFlows = repo }
}

// WithTopologyHITLRouter wires the GAP-4 HITL router used by the
// NodeKindInstantiate handler on first-instantiation-per-session.
func WithTopologyHITLRouter(router cpn.TopologyHITLRouter) SessionServiceOption {
	return func(s *SessionService) { s.topologyRouter = router }
}

// WithMutationLog wires the GAP-7 topology mutation audit log. When set,
// every created CPN inherits it so MutateTopology calls are persisted.
func WithMutationLog(log cpn.MutationAuditLog) SessionServiceOption {
	return func(s *SessionService) { s.mutationLog = log }
}

// NewSessionService creates a SessionService with the given dependencies.
func NewSessionService(llm cpn.LLMClient, cost cpn.CostProvider, logger *slog.Logger, factory TopologyFactory, opts ...SessionServiceOption) *SessionService {
	svc := &SessionService{
		sessions:        make(map[string]*cpn.Session),
		states:          make(map[string]*sessionState),
		llm:             llm,
		cost:            cost,
		logger:          logger,
		topologyFactory: factory,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// CreateSession creates a new session bound to a CPN topology.
func (s *SessionService) CreateSession(ctx context.Context, userID string, channel cpn.ChannelType) (*SessionInfo, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: userID is required", ErrInvalidInput)
	}

	id, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}

	root := s.topologyFactory(id)
	root.LLMClient = s.llm
	root.Cost = s.cost
	root.HostRuntime = s.hostRuntime
	root.SafeRegistry = s.safeRegistry
	root.FlowRepository = s.authoredFlows
	root.TopologyRouter = s.topologyRouter
	root.MutationLog = s.mutationLog
	root.RegionalVariant = s.resolveRegionalVariant(ctx, userID)
	s.applyUserModelPreferences(ctx, root, userID)
	// GAP-2: seed the well-known p-host-capabilities place from the most
	// recent snapshot, running host-discovery-cpn first if the cache is cold
	// or stale. Failures are logged but never block session creation —
	// downstream consumers PEEK and tolerate absence.
	s.ensureHostCapabilitiesSeed(ctx, root)
	root.EventSink = func(e *cpn.Event) {
		s.mu.RLock()
		cb := s.onEvent
		s.mu.RUnlock()
		if cb != nil {
			cb(id, *e)
		}
		s.persistEvent(id, e)
	}

	// Enable execution metrics collection.
	root.Metrics = cpn.NewMetricsRecorder()

	session := cpn.NewSession(id, userID, channel, root)

	// Wire HITL channels: walk transitions, create channels, register with session.
	for _, t := range root.Transitions {
		if t.Kind != cpn.NodeKindHITL {
			continue
		}
		if t.HITLConfig == nil {
			t.HITLConfig = &cpn.HITLConfig{}
		}
		ch := make(chan cpn.Token, 1)
		t.HITLConfig.Channel = ch
		if err := session.RegisterHITL(t.ID, ch); err != nil {
			s.logger.Error("register HITL channel", "transition", t.ID, "err", err)
		}
	}

	// Inject personality into LLM system prompts (best-effort).
	s.injectPersonality(ctx, root, userID)

	// Inject tool metadata (Parameters, Description, Executor) into transitions.
	if s.toolRegistry != nil {
		s.toolRegistry.InjectIntoCPN(root)
		// GAP-3: CPN gets a handle to the registry so
		// NodeKindRegisterTool can publish new tools at runtime.
		root.ToolRegistry = s.toolRegistry
	}

	st := &sessionState{state: cpn.StateIdle}

	s.mu.Lock()
	s.sessions[id] = session
	s.states[id] = st
	s.mu.Unlock()

	// Persist session creation (best-effort).
	if s.persist != nil && s.persist.Sessions != nil {
		rec := &persist.SessionRecord{
			ID: id, UserID: userID, Channel: string(channel),
			State: persist.SessionActive, CreatedAt: session.CreatedAt, LastActivityAt: session.CreatedAt,
		}
		if err := s.persist.Sessions.Create(ctx, rec); err != nil {
			s.logger.Warn("persist session create", "session_id", id, "error", err)
		}
	}

	s.logger.Info("session created", "session_id", id, "user_id", userID, "channel", channel)

	return &SessionInfo{
		ID:        session.ID,
		UserID:    session.UserID,
		Channel:   session.Channel,
		State:     st.get(),
		CreatedAt: session.CreatedAt,
		Messages:  session.Messages(),
	}, nil
}

// SendMessage appends a user message and starts CPN execution.
// Returns immediately (202 pattern); CPN runs in a background goroutine.
// Returns ErrSessionBusy if the CPN is already running for this session.
func (s *SessionService) SendMessage(ctx context.Context, sessionID, content string) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	st := s.states[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	current := st.get()
	if current == cpn.StateRunning || current == cpn.StateWaiting {
		return fmt.Errorf("%w: %s", ErrSessionBusy, sessionID)
	}

	msg := &cpn.Message{
		ID:        sessionID + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Role:      cpn.RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	}
	session.AppendMessage(msg)

	// Persist user message (best-effort).
	if s.persist != nil && s.persist.Sessions != nil {
		msgRec := persist.MessageToRecord(sessionID, msg)
		if err := s.persist.Sessions.AppendMessage(ctx, sessionID, msgRec); err != nil {
			s.logger.Warn("persist append message", "session_id", sessionID, "error", err)
		}

		// Auto-generate title from first user message if session has no title yet.
		if len(session.Messages()) == 1 {
			rec, err := s.persist.Sessions.Get(ctx, sessionID)
			if err == nil && rec.Title == "" {
				title := autoTitle(content)
				if err := s.persist.Sessions.UpdateTitle(ctx, sessionID, title); err != nil {
					s.logger.Warn("persist auto-title", "session_id", sessionID, "error", err)
				}
			}
		}
	}

	// Reset CPN before each run to clear stale tokens from previous
	// failed runs (e.g., HITL rejection leaving tokens in p-classified/p-plan).
	session.Root.Reset()

	// Sync conversation history to CPN so LLM transitions have context.
	// Must happen AFTER Reset (which clears History).
	msgs := session.Messages()
	history := make([]*cpn.Message, len(msgs))
	for i := range msgs {
		history[i] = &msgs[i]
	}
	session.Root.History = history
	historyLen := len(session.Root.History)

	tok := &cpn.Token{
		Color:     cpn.ColorString,
		Payload:   content,
		Space:     cpn.SpaceSurface,
		SessionID: sessionID,
		Timestamp: time.Now(),
	}

	// REQ-FIX-001: look up p-input directly; findSourcePlace is non-deterministic
	// when SeedHostSnapshot has added p-host-capabilities to the map.
	source := session.Root.Places["p-input"]
	if source == nil {
		source = findSourcePlace(session.Root)
	}
	if source != nil {
		if err := source.Deposit(tok); err != nil {
			return fmt.Errorf("deposit token: %w", err)
		}
	}

	// FIX-HITL-PERSIST: Wire flush callbacks so that messages accumulated
	// during a CPN run are persisted immediately — before blocking on
	// human input (HITL) and after each batch of transitions completes.
	// Without this, a user who disconnects while HITL is pending or during
	// a long-running execution loses all LLM responses, A2UI surfaces,
	// and tool results from the current run (persistAfterRun only fires
	// after Run returns).
	//
	// The callback tracks which messages have already been flushed via
	// lastFlushed to avoid duplicate inserts on successive callbacks
	// within the same run (e.g. t-classify → t-direct → t-clarify → t-review).
	var lastFlushed int
	flushHistory := func(historySnapshot []*cpn.Message, source string) {
		if s.persist == nil || s.persist.Sessions == nil {
			return
		}
		// Only persist messages newer than what we've already flushed.
		startIdx := max(historyLen, lastFlushed)
		if startIdx >= len(historySnapshot) {
			return
		}
		newMsgs := historySnapshot[startIdx:]

		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()

		flushed := 0
		for _, m := range newMsgs {
			if m.Role != cpn.RoleAssistant && m.ParentMessageID == "" {
				continue
			}
			rec := persist.MessageToRecord(sessionID, m)
			if err := s.persist.Sessions.AppendMessage(flushCtx, sessionID, rec); err != nil {
				s.logger.Warn("mid-run flush persist message", "session_id", sessionID, "msg_id", m.ID, "source", source, "error", err)
			} else {
				flushed++
			}
		}
		lastFlushed = len(historySnapshot)

		// Also sync the flushed messages into session.Messages so
		// rehydration from the in-memory session is consistent.
		for _, m := range newMsgs {
			if m.Role != cpn.RoleAssistant && m.ParentMessageID == "" {
				continue
			}
			session.AppendMessage(m)
		}

		if flushed > 0 {
			s.logger.Info("mid-run flush persisted messages",
				"session_id", sessionID,
				"source", source,
				"flushed", flushed,
				"history_len", len(historySnapshot),
			)
		}

		// Touch activity timestamp.
		touchCtx, touchCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer touchCancel()
		_ = s.persist.Sessions.Touch(touchCtx, sessionID)
	}

	// Called before HITL transitions block on human input.
	session.Root.OnHITLWaiting = func(historySnapshot []*cpn.Message) {
		flushHistory(historySnapshot, "hitl-waiting")
	}
	// Called after each batch of transition firings completes in the executor.
	session.Root.OnHistoryChanged = func(historySnapshot []*cpn.Message) {
		flushHistory(historySnapshot, "executor-batch")
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	st.mu.Lock()
	st.cancel = cancel
	st.mu.Unlock()

	st.set(cpn.StateRunning)

	go func() {
		defer cancel()

		runErr := session.Root.Run(bgCtx)

		// Sync new history entries from CPN back to session on ALL exit paths.
		// fireLLM appends assistant responses to c.History during execution;
		// fireHITL appends both an A2UI surface (assistant) and, on
		// approve/submit/revise, a response row (user) with ParentMessageID
		// pointing at the surface — both must propagate so persistence and
		// rehydration can pair them (REQ-001/002/006). Without this sync,
		// those responses are lost when the CPN fails.
		// FIX-HITL-PERSIST: Start syncing from lastFlushed when the HITL
		// callback already promoted some entries to session.Messages + DB.
		syncStart := max(historyLen, lastFlushed)
		if len(session.Root.History) > syncStart {
			var newRows, assistantCount, userCount int
			for _, m := range session.Root.History[syncStart:] {
				// Assistant rows are always CPN-originated and always sync.
				// User rows only sync when they are HITL responses (identified
				// by ParentMessageID linking to an earlier A2UI surface). All
				// other user rows were authored outside the CPN and are
				// already in session.Messages.
				if m.Role != cpn.RoleAssistant && m.ParentMessageID == "" {
					continue
				}
				// Preserve the CPN-assigned ID when present so the response
				// row's ParentMessageID remains resolvable across session.Messages.
				id := m.ID
				if id == "" {
					id = sessionID + "-sync-" + fmt.Sprintf("%d", time.Now().UnixNano())
				}
				session.AppendMessage(&cpn.Message{
					ID:              id,
					Role:            m.Role,
					Content:         m.Content,
					CPNID:           m.CPNID,
					CPNRole:         m.CPNRole,
					CPNDepth:        m.CPNDepth,
					Timestamp:       m.Timestamp,
					ParentMessageID: m.ParentMessageID,
				})
				newRows++
				switch m.Role {
				case cpn.RoleAssistant:
					assistantCount++
				case cpn.RoleUser:
					userCount++
				}
			}
			// REQ-OBS-005 (spec-process-bugfix-treview-surface-and-locked-parser.md):
			// emit a structured delta log so operators can correlate a CPN
			// run's History growth with the Messages table. HITL cycles
			// should produce assistant_count >= 1 (surface) and user_count
			// in {0, 1} (0 on reject, 1 on approve/revise/submit).
			slog.DebugContext(bgCtx, "history sync delta",
				"session_id", sessionID,
				"new_rows", newRows,
				"assistant_count", assistantCount,
				"user_count", userCount,
			)
		}

		if runErr != nil {
			s.logger.Error("CPN run failed", "session_id", sessionID, "error", runErr)
			// Send done sentinel so the frontend knows the response is complete.
			select {
			case session.Stream <- cpn.StreamChunk{
				SessionID: sessionID,
				CPNID:     session.Root.ID,
				CPNRole:   session.Root.Role,
				Content:   "",
				Done:      true,
			}:
			default:
			}
			// Set idle — HITL rejection is a normal conversational event,
			// not a terminal failure. The session remains usable.
			st.set(cpn.StateIdle)
			newMsgs := session.Messages()[historyLen:]
			go s.persistAfterRun(sessionID, newMsgs, session.Root)
			return
		}

		// Check if streaming actually happened at runtime (content already delivered via EventSink).
		streamingActive := session.Root.StreamedOutput

		// Collect output from terminal places.
		// When streaming was active, assistant messages were already synced from c.History above.
		// We still consume tokens to clear the places, but skip message creation to avoid duplicates.
		for _, p := range session.Root.TerminalPlaces() {
			for {
				tok, err := p.Consume()
				if err != nil {
					break // no more tokens
				}
				content, ok := tok.Payload.(string)
				if !ok {
					continue
				}
				// When streaming was active, history sync already captured this content.
				// Skip AppendMessage to avoid duplicate messages in session history.
				if !streamingActive {
					session.AppendMessage(&cpn.Message{
						ID:        sessionID + "-resp-" + fmt.Sprintf("%d", time.Now().UnixNano()),
						Role:      cpn.RoleAssistant,
						Content:   content,
						CPNID:     session.Root.ID,
						CPNRole:   session.Root.Role,
						CPNDepth:  session.Root.Depth,
						Timestamp: time.Now(),
					})
					select {
					case session.Stream <- cpn.StreamChunk{
						SessionID: sessionID,
						CPNID:     session.Root.ID,
						CPNRole:   session.Root.Role,
						Content:   content,
						Done:      false,
					}:
					default:
						s.logger.Warn("stream buffer full, chunk dropped", "session_id", sessionID)
					}
				}
			}
		}
		// Send done sentinel ONLY when streaming was NOT active.
		// When streaming was active, fireLLM already emitted Done via the broker;
		// emitting a second Done here creates a race condition where this Done
		// (on Session.Stream) can arrive at the SSE handler before the broker
		// has finished delivering all content chunks from client.events.
		if !streamingActive {
			select {
			case session.Stream <- cpn.StreamChunk{
				SessionID: sessionID,
				CPNID:     session.Root.ID,
				CPNRole:   session.Root.Role,
				Content:   "",
				Done:      true,
			}:
			default:
			}
		}
		// Set idle instead of completed — session can accept more messages.
		st.set(cpn.StateIdle)
		// Do NOT close the stream — SSE connection stays alive for next message.

		// Persist activity, ledger, assistant messages, flow, and execution records (async).
		newMsgs := session.Messages()[historyLen:]
		go s.persistAfterRun(sessionID, newMsgs, session.Root)
	}()

	return nil
}

// DeleteSession removes a session and releases its resources.
// If the CPN is running, it is canceled via its context. The session is
// removed from the service maps so no new operations can target it.
// The stream channel is NOT closed here — the background goroutine from
// SendMessage owns the channel lifecycle and will exit on context cancellation.
func (s *SessionService) DeleteSession(sessionID string) error {
	s.mu.Lock()
	_, ok := s.sessions[sessionID]
	st := s.states[sessionID]
	delete(s.sessions, sessionID)
	delete(s.states, sessionID)
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	// Cancel running CPN if active.
	if st != nil {
		st.mu.RLock()
		cancel := st.cancel
		st.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
	}

	// Persist session closure (best-effort).
	if s.persist != nil && s.persist.Sessions != nil {
		if err := s.persist.Sessions.Close(context.Background(), sessionID); err != nil {
			s.logger.Warn("persist session close", "session_id", sessionID, "error", err)
		}
	}

	s.logger.Info("session deleted", "session_id", sessionID)
	return nil
}

// CancelSession cancels a running CPN session.
func (s *SessionService) CancelSession(sessionID string) {
	s.mu.RLock()
	st, ok := s.states[sessionID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	st.mu.RLock()
	cancel := st.cancel
	st.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// ResolveHITL forwards a human response to a waiting HITL transition.
//
// Rehydrates the session from persistence on in-memory miss (REQ-003a). If the
// session has no in-flight execution to accept the response, returns
// ErrSessionInactive so the caller can distinguish "ghost" from "idle" and
// prompt the user for a new run rather than silently failing.
func (s *SessionService) ResolveHITL(ctx context.Context, sessionID, transitionID string, resp cpn.HITLResponse) error {
	session, _, err := s.getOrRehydrate(ctx, sessionID)
	if err != nil {
		return err
	}

	// HITL resolution requires an in-flight execution to be waiting on a
	// human response. If no CPN run is live (Running/Waiting), any rehydrated
	// topology is idle and there is no HITL channel registered for a real
	// transition. Signal this explicitly so the frontend can prompt a new run.
	s.mu.RLock()
	st := s.states[sessionID]
	s.mu.RUnlock()
	if st == nil || !isExecutionLive(st.get()) {
		return fmt.Errorf("%w: %s", ErrSessionInactive, sessionID)
	}

	return session.ResolveHITL(ctx, transitionID, resp)
}

// isExecutionLive reports whether a session state indicates an in-flight CPN
// execution that can accept stream chunks or HITL responses.
func isExecutionLive(state cpn.State) bool {
	return state == cpn.StateRunning || state == cpn.StateWaiting
}

// GetSession returns a read-only snapshot of the session.
//
// On in-memory miss, attempts rehydration from persistence (REQ-001). The
// signature is preserved per spec §4.1; rehydration uses a bounded background
// context internally because callers (HTTP handlers) do not currently thread
// a request context into this entrypoint.
func (s *SessionService) GetSession(sessionID string) (*SessionInfo, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	st := s.states[sessionID]
	s.mu.RUnlock()
	if ok {
		return buildSessionInfo(session, st), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rehydrateTimeout)
	defer cancel()

	session, err := s.rehydrate(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	st = s.states[sessionID]
	s.mu.RUnlock()
	return buildSessionInfo(session, st), nil
}

// StreamChannel returns the session's streaming channel for SSE delivery.
//
// Rehydrates on miss (REQ-002). After rehydration, the returned channel is the
// live stream channel of the rehydrated *cpn.Session.
func (s *SessionService) StreamChannel(sessionID string) (<-chan cpn.StreamChunk, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if ok {
		return session.Stream, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rehydrateTimeout)
	defer cancel()

	session, err := s.rehydrate(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return session.Stream, nil
}

// SendStreamChunk sends a stream chunk to the session's streaming channel.
// Used by the CPN execution engine to deliver LLM streaming output.
//
// Rehydrates on miss (REQ-003a). A freshly rehydrated session has no producer
// owning its stream buffer, so this path returns ErrSessionInactive to prevent
// dropping chunks into a stale topology. Pre-existing in-memory sessions keep
// their legacy behavior (direct channel send) so injecting chunks from tests
// or future in-process callers is unaffected.
func (s *SessionService) SendStreamChunk(sessionID string, chunk cpn.StreamChunk) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), rehydrateTimeout)
		defer cancel()

		if _, err := s.rehydrate(ctx, sessionID); err != nil {
			return err
		}
		// Freshly rehydrated sessions have no active producer/consumer. Spec
		// §4.2 row 2 requires ErrSessionInactive on this specific path.
		return fmt.Errorf("%w: %s", ErrSessionInactive, sessionID)
	}

	select {
	case session.Stream <- chunk:
		return nil
	default:
		return fmt.Errorf("stream buffer full for session %s", sessionID)
	}
}

// CloseStream closes the session's streaming channel, signaling end of output.
// Safe to call multiple times; the channel is closed only once.
func (s *SessionService) CloseStream(sessionID string) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	st := s.states[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	st.streamClosed.Do(func() { close(session.Stream) })
	return nil
}

// ListSessions returns a paginated list of sessions for the given user.
// Requires persistence to be configured; returns ErrNoPersistence otherwise.
func (s *SessionService) ListSessions(ctx context.Context, userID string, opts *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error) {
	if s.persist == nil || s.persist.Sessions == nil {
		return nil, ErrNoPersistence
	}
	return s.persist.Sessions.ListByUserID(ctx, userID, opts)
}

// UpdateSession updates a session's title and/or soft-deletes it.
// If title is non-nil, the session title is updated.
// If softDelete is true, the session is soft-deleted and removed from in-memory maps.
func (s *SessionService) UpdateSession(ctx context.Context, sessionID string, title *string, softDelete bool) error {
	if s.persist == nil || s.persist.Sessions == nil {
		return ErrNoPersistence
	}

	// Verify session exists in persistence.
	if _, err := s.persist.Sessions.Get(ctx, sessionID); err != nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	if title != nil {
		if err := s.persist.Sessions.UpdateTitle(ctx, sessionID, *title); err != nil {
			return fmt.Errorf("update title: %w", err)
		}
	}

	if softDelete {
		if err := s.persist.Sessions.SoftDelete(ctx, sessionID); err != nil {
			return fmt.Errorf("soft delete: %w", err)
		}
		// Remove from in-memory maps so no new operations target it.
		s.mu.Lock()
		st := s.states[sessionID]
		delete(s.sessions, sessionID)
		delete(s.states, sessionID)
		s.mu.Unlock()

		// Cancel running CPN if active.
		if st != nil {
			st.mu.RLock()
			cancel := st.cancel
			st.mu.RUnlock()
			if cancel != nil {
				cancel()
			}
		}
	}

	return nil
}

// ForkSession creates a new session by forking from an existing one at a given message index.
// The new session copies messages up to messageIndex from the source and creates
// a fresh in-memory CPN session (no token deposited).
func (s *SessionService) ForkSession(ctx context.Context, sourceSessionID, userID string, channel cpn.ChannelType, messageIndex int) (*SessionInfo, error) {
	if s.persist == nil || s.persist.Sessions == nil {
		return nil, ErrNoPersistence
	}

	newID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}

	// Fork in persistence (copies messages, creates new session record).
	rec, err := s.persist.Sessions.ForkSession(ctx, newID, sourceSessionID, messageIndex, userID, string(channel))
	if err != nil {
		return nil, fmt.Errorf("fork session: %w", err)
	}

	// Create in-memory CPN session (same as CreateSession but no token deposit).
	root := s.topologyFactory(newID)
	root.LLMClient = s.llm
	root.Cost = s.cost
	root.SafeRegistry = s.safeRegistry
	root.FlowRepository = s.authoredFlows
	root.TopologyRouter = s.topologyRouter
	root.RegionalVariant = s.resolveRegionalVariant(ctx, userID)
	s.applyUserModelPreferences(ctx, root, userID)
	root.EventSink = func(e *cpn.Event) {
		s.mu.RLock()
		cb := s.onEvent
		s.mu.RUnlock()
		if cb != nil {
			cb(newID, *e)
		}
		s.persistEvent(newID, e)
	}
	root.Metrics = cpn.NewMetricsRecorder()

	session := cpn.NewSession(newID, userID, channel, root)

	// Wire HITL channels.
	for _, t := range root.Transitions {
		if t.Kind != cpn.NodeKindHITL {
			continue
		}
		if t.HITLConfig == nil {
			t.HITLConfig = &cpn.HITLConfig{}
		}
		ch := make(chan cpn.Token, 1)
		t.HITLConfig.Channel = ch
		if err := session.RegisterHITL(t.ID, ch); err != nil {
			s.logger.Error("register HITL channel", "transition", t.ID, "err", err)
		}
	}

	// Inject personality into LLM system prompts (best-effort).
	s.injectPersonality(ctx, root, userID)

	// Inject tool metadata (Parameters, Description, Executor) into transitions.
	if s.toolRegistry != nil {
		s.toolRegistry.InjectIntoCPN(root)
		// GAP-3: CPN gets a handle to the registry so
		// NodeKindRegisterTool can publish new tools at runtime.
		root.ToolRegistry = s.toolRegistry
	}

	// Load forked messages into in-memory session history.
	if rec.Messages != nil {
		for _, mr := range rec.Messages {
			session.AppendMessage(&cpn.Message{
				ID:        mr.ID,
				Role:      cpn.MessageRole(mr.Role),
				Content:   mr.Content,
				CPNID:     mr.CPNID,
				CPNRole:   mr.CPNRole,
				CPNDepth:  mr.CPNDepth,
				Timestamp: mr.Timestamp,
			})
		}
	}

	st := &sessionState{state: cpn.StateIdle}

	s.mu.Lock()
	s.sessions[newID] = session
	s.states[newID] = st
	s.mu.Unlock()

	s.logger.Info("session forked", "new_session_id", newID, "source_session_id", sourceSessionID, "message_index", messageIndex)

	return &SessionInfo{
		ID:                  session.ID,
		UserID:              session.UserID,
		Channel:             session.Channel,
		State:               st.get(),
		CreatedAt:           session.CreatedAt,
		Messages:            session.Messages(),
		Title:               rec.Title,
		ForkedFromSessionID: rec.ForkedFromSessionID,
		ForkMessageCount:    rec.ForkMessageCount,
	}, nil
}

// SetEventCallback registers a callback invoked on every CPN event.
func (s *SessionService) SetEventCallback(fn func(string, cpn.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

// resolveRegionalVariant loads the user's BCP-47 regional variant from the
// repository (cached on the root CPN by the caller). Falls back to the
// language-default variant when the user is unknown, the field is empty, or
// no user repository is wired (anonymous / pre-onboarding flows). The
// returned value is always a supported BCP-47 tag, never empty (GUD-003).
func (s *SessionService) resolveRegionalVariant(ctx context.Context, userID string) string {
	if s.persist == nil || s.persist.Users == nil || userID == "" {
		return prompts.DefaultVariant("")
	}
	rec, err := s.persist.Users.GetByID(ctx, userID)
	if err != nil {
		// Anonymous / pre-onboarding users are expected here; only log at debug.
		return prompts.DefaultVariant("")
	}
	if prompts.IsSupported(rec.RegionalVariant) {
		return rec.RegionalVariant
	}
	return prompts.DefaultVariant(rec.PreferredLanguage)
}

// applyUserModelPreferences stamps LLMConfig.Model on every transition in
// root with the concrete OpenRouter model id the user's preferences dictate.
//
// Precedence (highest wins), per REQ-CFG-005:
//  1. UserRecord.ModelOverrides[LLMConfig.Role] when Role is non-empty and
//     the override is non-empty.
//  2. UserRecord.PreferredModel when non-empty.
//  3. openrouter.PRODUCT_DEFAULT_MODEL.
//
// This function is the ONLY legitimate mutator of LLMConfig.Model — all
// topology authors leave it empty and express intent through Role. No ENV
// lookup occurs here (REQ-CFG-002).
//
// Error handling is best-effort: if the user cannot be fetched, every LLM
// transition is stamped with PRODUCT_DEFAULT_MODEL so the session still
// runs with a well-defined model. Callers MUST NOT attempt to re-resolve
// the model elsewhere (GUD-001).
func (s *SessionService) applyUserModelPreferences(ctx context.Context, root *cpn.CPN, userID string) {
	var rec *persist.UserRecord
	if s.persist != nil && s.persist.Users != nil && userID != "" {
		// Anonymous / pre-onboarding users are expected to yield an error
		// here; we treat that as "no preference" and fall through.
		rec, _ = s.persist.Users.GetByID(ctx, userID)
	}
	for _, t := range root.Transitions {
		if t.Kind != cpn.NodeKindLLM || t.LLMConfig == nil {
			continue
		}
		t.LLMConfig.Model = s.resolveModelWithGate(ctx, rec, t.LLMConfig.Role)
	}
}

// resolveModelWithGate runs the REQ-CFG-005 precedence cascade AND, if a
// ModelRegistry is wired, validates each candidate against REQ-GATE-001.
//
// Cascade (highest wins):
//  1. UserRecord.ModelOverrides[role]          (per-role override)
//  2. UserRecord.PreferredModel                (global user preference)
//  3. ModelRegistry role default (when role != "" and registry is wired)
//  4. ModelRegistry product default / openrouter.PRODUCT_DEFAULT_MODEL
//
// When the registry is wired, steps 1-3 each get validated with GetInvokable
// and fall through on ErrModelNotInvokable / ErrModelNotFound. A WARN log is
// emitted for each fallback so REQ-OBS-004 has a trail to read.
//
// When the registry is NOT wired (tests, legacy bootstrap), behavior exactly
// matches the pre-registry resolveModelForUser helper.
func (s *SessionService) resolveModelWithGate(ctx context.Context, rec *persist.UserRecord, role string) string {
	if s.modelRegistry == nil {
		// Legacy path. Parity with pre-registry behavior is what the existing
		// session_service_model_prefs_test.go asserts — don't touch.
		return resolveModelForUser(rec, role)
	}

	// Step 1 — per-role override
	if rec != nil && role != "" {
		if m, ok := rec.ModelOverrides[role]; ok && m != "" {
			if s.validateInvokable(ctx, m, "user override", role) {
				return m
			}
		}
	}

	// Step 2 — global preferred
	if rec != nil && rec.PreferredModel != "" {
		if s.validateInvokable(ctx, rec.PreferredModel, "user preferred", role) {
			return rec.PreferredModel
		}
	}

	// Step 3 — role default from registry
	if role != "" {
		if roleID, err := s.modelRegistry.GetRoleDefault(ctx, role); err == nil && roleID != "" {
			if s.validateInvokable(ctx, roleID, "role default", role) {
				return roleID
			}
		}
	}

	// Step 4 — product default
	def, err := s.modelRegistry.GetProductDefault(ctx)
	if err != nil {
		// Infrastructure failure — keep the session running on the hardcoded
		// constant. Frontier case; the schema guarantees the row exists.
		dm := s.defaultModel
		if dm == "" {
			dm = fallbackDefaultModel
		}
		s.logger.Error("model registry product default unreachable; using compile-time constant",
			"role", role,
			"err", err,
			"fallback_model", dm,
		)
		return dm
	}
	return def.RegistryID
}

// validateInvokable returns true when the candidate passes REQ-GATE-001.
// A false return means the caller should try the next level of the cascade;
// a WARN has already been logged.
func (s *SessionService) validateInvokable(ctx context.Context, candidate, source, role string) bool {
	if s.modelRegistry == nil {
		return true
	}
	_, err := s.modelRegistry.GetInvokable(ctx, candidate)
	if err == nil {
		return true
	}
	var reason string
	switch {
	case errors.Is(err, cpn.ErrModelNotInvokable):
		reason = "lifecycle or license gate failed"
	case errors.Is(err, cpn.ErrModelNotFound):
		reason = "model not registered"
	default:
		// Infrastructure failure — don't silently advance. Prefer the registered
		// product default over a possibly-invokable-but-unverified candidate.
		s.logger.Error("model registry GetInvokable failed; falling back",
			"source", source,
			"role", role,
			"candidate", candidate,
			"err", err,
		)
		return false
	}
	s.logger.Warn("model not invokable; falling back",
		"source", source,
		"role", role,
		"candidate", candidate,
		"reason", reason,
	)
	return false
}

// resolveModelForUser applies the three-level precedence cascade. Exported
// as a package-visible helper to keep applyUserModelPreferences readable and
// to give tests a tiny pure function to exercise precedence without building
// a full CPN.
func resolveModelForUser(rec *persist.UserRecord, role string) string {
	if rec != nil {
		if role != "" {
			if m, ok := rec.ModelOverrides[role]; ok && m != "" {
				return m
			}
		}
		if rec.PreferredModel != "" {
			return rec.PreferredModel
		}
	}
	return fallbackDefaultModel
}

// injectPersonality loads the user's personality and prefixes all LLM system prompts.
// Called after topology creation but before CPN.Run().
// If the tool registry is nil or the identity tool is not registered, this is a no-op.
func (s *SessionService) injectPersonality(ctx context.Context, c *cpn.CPN, userID string) {
	if s.toolRegistry == nil {
		return
	}

	entry, ok := s.toolRegistry.Resolve("system/personality.get_identity")
	if !ok {
		return
	}

	// Call the identity tool executor directly (outside CPN execution).
	inToken := cpn.Token{
		Color:   cpn.ColorIdentity,
		Payload: userID,
		Space:   cpn.SpaceComputation,
	}

	outToken, err := entry.Executor(ctx, inToken)
	if err != nil {
		s.logger.Warn("personality injection failed, using defaults", "error", err)
		return
	}

	personality, ok := outToken.Payload.(*cpn.Personality)
	if !ok {
		return
	}

	// Prefix all LLM transitions' system prompts with the personality.
	promptPrefix := personality.AsSystemPrompt()
	for _, t := range c.Transitions {
		if t.Kind != cpn.NodeKindLLM {
			continue
		}
		if t.SystemPrompt != "" {
			t.SystemPrompt = promptPrefix + "\n\n" + t.SystemPrompt
		} else {
			t.SystemPrompt = promptPrefix
		}
	}

	// Emit personality loaded event for observability.
	if c.EventSink != nil {
		source := "default"
		if personality.UserID != "" {
			source = "database"
		}

		// Build principle snapshots (up to 3).
		snapshots := make([]cpn.PrincipleSnapshot, 0, len(personality.Principles))
		for _, p := range personality.Principles {
			snapshots = append(snapshots, cpn.PrincipleSnapshot{
				Kind:  string(p.Kind),
				Title: p.Title,
			})
		}

		c.EventSink(&cpn.Event{
			Type:      cpn.EventPersonalityLoaded,
			SessionID: c.SessionID,
			CPNID:     c.ID,
			CPNDepth:  c.Depth,
			CPNRole:   c.Role,
			Payload: cpn.PersonalityLoadedPayload{
				UserID:     userID,
				Source:     source,
				Principles: snapshots,
			},
			Timestamp: time.Now(),
		})
	}
}

// persistEvent converts a CPN event to an EventRecord and appends it.
func (s *SessionService) persistEvent(sessionID string, e *cpn.Event) {
	if s.persist == nil || s.persist.Events == nil {
		return
	}
	rec, err := persist.EventToRecord(sessionID, e)
	if err != nil {
		s.logger.Warn("persist event convert", "session_id", sessionID, "error", err)
		return
	}
	if rec.ID == "" {
		id, err := generateSessionID()
		if err != nil {
			return
		}
		rec.ID = "evt-" + id
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.persist.Events.Append(ctx, rec); err != nil {
		s.logger.Warn("persist event append", "session_id", sessionID, "error", err)
	}
}

// persistAfterRun syncs activity, ledger, assistant messages, flow topology,
// and execution records after a CPN run. Runs in its own goroutine.
func (s *SessionService) persistAfterRun(sessionID string, newMessages []cpn.Message, root *cpn.CPN) {
	if s.persist == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Touch session's LastActivityAt.
	if s.persist.Sessions != nil {
		if err := s.persist.Sessions.Touch(ctx, sessionID); err != nil {
			s.logger.Warn("persist session touch", "session_id", sessionID, "error", err)
		}
	}

	// 2. Persist new assistant messages and HITL user-response rows.
	// HITL response rows carry ParentMessageID linking to the A2UI surface
	// — they must be persisted so rehydration can lock the questionnaire
	// (REQ-001/002/006/007). Other user rows were already persisted at
	// submission time in SendMessage.
	if s.persist.Sessions != nil {
		for i := range newMessages {
			m := &newMessages[i]
			if m.Role != cpn.RoleAssistant && m.ParentMessageID == "" {
				continue
			}
			rec := persist.MessageToRecord(sessionID, m)
			if err := s.persist.Sessions.AppendMessage(ctx, sessionID, rec); err != nil {
				s.logger.Warn("persist cpn-sourced message", "session_id", sessionID, "error", err)
			}
		}
	}

	// 3. Crystallize flow topology and get the hash.
	flowHash := s.persistFlow(ctx, root)

	// 4. Persist execution records and update flow stats.
	if s.persist.Intelligence != nil && root != nil && root.Metrics != nil {
		records := root.Metrics.Records()
		var totalCost float64
		var totalDur int64
		var successCount int
		for i := range records {
			rec := &records[i]
			pRec := &persist.ExecutionRecord{
				CPNID: rec.CPNID, CPNRole: rec.CPNRole, CPNDepth: rec.CPNDepth,
				SessionID: rec.SessionID, TransitionsFired: rec.TransitionsFired,
				LLMCalls: rec.LLMCallCount, ToolCalls: rec.ToolCallCount,
				TokensProduced: rec.TokensProduced, TotalCostUSD: rec.TotalCostUSD,
				DurationMs: rec.Duration.Milliseconds(), Success: rec.Success,
				StartedAt: rec.StartedAt, CompletedAt: rec.CompletedAt,
			}
			id, err := generateSessionID()
			if err == nil {
				pRec.ID = "exec-" + id
			}
			if err := s.persist.Intelligence.RecordExecution(ctx, pRec); err != nil {
				s.logger.Warn("persist execution record", "session_id", sessionID, "error", err)
			}
			totalCost += rec.TotalCostUSD
			totalDur += rec.Duration.Milliseconds()
			if rec.Success {
				successCount++
			}
		}

		// Update flow stats from this execution.
		if flowHash != "" && s.persist.Flows != nil && len(records) > 0 {
			// Read current stats to accumulate.
			existing, err := s.persist.Flows.GetByHash(ctx, flowHash)
			if err == nil {
				prevCount := existing.Stats.ExecutionCount
				newCount := prevCount + int64(len(records))
				// Weighted running average.
				avgCost := (existing.Stats.AvgCostUSD*float64(prevCount) + totalCost) / float64(newCount)
				avgDur := int64(0)
				if newCount > 0 {
					avgDur = (existing.Stats.AvgDurationMs*prevCount + totalDur) / newCount
				}
				successRate := (existing.Stats.SuccessRate*float64(prevCount) + float64(successCount)) / float64(newCount)

				stats := &persist.FlowStats{
					ExecutionCount: newCount,
					SuccessRate:    successRate,
					AvgCostUSD:     avgCost,
					AvgDurationMs:  avgDur,
				}
				if err := s.persist.Flows.UpdateStats(ctx, flowHash, stats); err != nil {
					s.logger.Warn("persist flow updateStats", "hash", flowHash, "error", err)
				}
			}
		}
	}

	// 5. Sync token usage from in-memory ledger.
	if s.persist.Ledger != nil && s.tokenLedger != nil {
		usage := s.tokenLedger.Get(sessionID)
		if usage != nil {
			rec := &persist.LedgerRecord{
				SessionID: sessionID, InputTokens: int64(usage.InputTokens),
				OutputTokens: int64(usage.OutputTokens), Calls: int64(usage.Calls),
				TotalCostUSD: usage.TotalCostUSD, LastUpdated: time.Now(),
			}
			if err := s.persist.Ledger.Record(ctx, rec); err != nil {
				s.logger.Warn("persist ledger record", "session_id", sessionID, "error", err)
			}
		}
	}
}

// persistFlow serializes a CPN topology and saves it to the flow repository.
// Returns the topology hash (empty string on failure).
func (s *SessionService) persistFlow(ctx context.Context, root *cpn.CPN) string {
	if s.persist == nil || s.persist.Flows == nil || s.persist.FuncRegistry == nil || root == nil {
		return ""
	}
	topo, err := persist.MarshalCPN(root, s.persist.FuncRegistry)
	if err != nil {
		s.logger.Warn("persist flow marshal", "cpn_id", root.ID, "error", err)
		return ""
	}
	hash := persist.TopologyHash(topo)
	topoJSON, err := json.Marshal(topo)
	if err != nil {
		s.logger.Warn("persist flow json", "cpn_id", root.ID, "error", err)
		return ""
	}
	now := time.Now()
	rec := &persist.FlowRecord{
		Hash: hash, Role: root.Role, TopologyJSON: topoJSON,
		FunctionMapping: json.RawMessage(`{}`),
		CreatedAt:       now, UpdatedAt: now,
	}
	if err := s.persist.Flows.Save(ctx, rec); err != nil {
		s.logger.Warn("persist flow save", "cpn_id", root.ID, "hash", hash, "error", err)
		return ""
	}
	return hash
}

// buildSessionInfo constructs a SessionInfo snapshot from an in-memory
// *cpn.Session and its matching sessionState. Centralized to keep the hot
// path and the post-rehydration path identical in shape (spec REQ-001).
func buildSessionInfo(session *cpn.Session, st *sessionState) *SessionInfo {
	state := cpn.StateIdle
	if st != nil {
		state = st.get()
	}
	return &SessionInfo{
		ID:        session.ID,
		UserID:    session.UserID,
		Channel:   session.Channel,
		State:     state,
		CreatedAt: session.CreatedAt,
		Messages:  session.Messages(),
	}
}

// getOrRehydrate returns the live *cpn.Session for sessionID, rehydrating it
// from persistence if it is not in memory. The rehydrated flag reports whether
// the returned session was freshly reconstructed (true) or already live (false).
//
// Callers that only tolerate live execution state (HITL resolution, stream
// producers) use the flag to convert a fresh rehydration into ErrSessionInactive
// (spec REQ-003).
func (s *SessionService) getOrRehydrate(ctx context.Context, sessionID string) (*cpn.Session, bool, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if ok {
		return session, false, nil
	}

	session, err := s.rehydrate(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	return session, true, nil
}

// rehydrate reconstructs an in-memory *cpn.Session from persistence when the
// in-memory map has no entry for sessionID. It is protected by singleflight so
// concurrent misses for the same id trigger exactly one persistence round-trip
// (REQ-004, AC-004).
//
// Locking discipline (REQ-009, PAT-001):
//   - Read the map under RLock, release before touching persistence.
//   - On success, take Lock, re-check for a concurrently inserted entry
//     (double-checked locking), and only then insert.
//
// Error contract:
//   - Session absent or soft-deleted → ErrSessionNotFound (REQ-005).
//   - No persistence wired → ErrSessionNotFound (REQ-007, preserves backwards
//     compatibility with ephemeral deployments).
//   - Persistence I/O failure → ErrPersistenceUnavailable (spec §4.2 / §9.6).
//   - All errors are wrapped with fmt.Errorf("...: %w", ...) and are
//     errors.Is-checkable (GUD-001).
//
// TODO(metrics): when a metrics library is adopted, emit
// session_rehydration_total{result} and session_rehydration_duration_seconds
// per REQ-011. For now the operation is recorded via structured logs (REQ-010).
func (s *SessionService) rehydrate(ctx context.Context, sessionID string) (*cpn.Session, error) {
	// REQ-007: no persistence → act like the pre-change code path.
	if s.persist == nil || s.persist.Sessions == nil {
		s.logger.Warn("session rehydrate skipped",
			"session_id", sessionID,
			"reason", "no_persistence",
		)
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	// singleflight collapses concurrent callers for the same id into one load.
	v, err, _ := s.sf.Do(sessionID, func() (any, error) {
		return s.loadFromPersist(ctx, sessionID)
	})
	if err != nil {
		return nil, err
	}
	loaded, ok := v.(*cpn.Session)
	if !ok || loaded == nil {
		// Defensive: loadFromPersist should either return *cpn.Session or error.
		return nil, fmt.Errorf("rehydrate session %s: unexpected nil result", sessionID)
	}

	// Double-checked locking: another goroutine may have inserted between the
	// singleflight call returning and us acquiring the write lock (e.g. a
	// concurrent CreateSession with a colliding id — pathological, but cheap
	// to guard). Never hold s.mu across persistence I/O (REQ-009).
	s.mu.Lock()
	if existing, ok := s.sessions[sessionID]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.sessions[sessionID] = loaded
	s.states[sessionID] = &sessionState{state: cpn.StateIdle}
	s.mu.Unlock()

	return loaded, nil
}

// loadFromPersist performs the actual persistence read and reconstructs a
// live *cpn.Session from the stored record. Called from inside singleflight,
// so exactly one goroutine executes this per (sessionID, in-flight) tuple.
func (s *SessionService) loadFromPersist(ctx context.Context, sessionID string) (*cpn.Session, error) {
	start := time.Now()

	rec, err := s.persist.Sessions.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, persist.ErrSessionNotFound) {
			s.logger.Warn("session rehydrate miss",
				"session_id", sessionID,
				"reason", "not_in_persistence",
				"duration_ms", time.Since(start).Milliseconds(),
			)
			// TODO(metrics): session_rehydration_total{result="miss"}++
			return nil, fmt.Errorf("rehydrate session %s: %w", sessionID, ErrSessionNotFound)
		}
		s.logger.Error("session rehydrate persistence error",
			"session_id", sessionID,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		// TODO(metrics): session_rehydration_total{result="error"}++
		return nil, fmt.Errorf("rehydrate session %s: %w", sessionID, ErrPersistenceUnavailable)
	}

	// REQ-005: soft-deleted sessions must not be rehydrated.
	if rec.DeletedAt != nil {
		s.logger.Warn("session rehydrate soft-deleted",
			"session_id", sessionID,
			"reason", "soft_deleted",
			"duration_ms", time.Since(start).Milliseconds(),
		)
		// TODO(metrics): session_rehydration_total{result="soft_deleted"}++
		return nil, fmt.Errorf("rehydrate session %s: %w", sessionID, ErrSessionNotFound)
	}

	// Reconstruct topology and session. The channel field is authoritative.
	channel := cpn.ChannelType(rec.Channel)
	root := s.topologyFactory(sessionID)
	root.LLMClient = s.llm
	root.Cost = s.cost
	root.SafeRegistry = s.safeRegistry
	root.FlowRepository = s.authoredFlows
	root.TopologyRouter = s.topologyRouter
	root.RegionalVariant = s.resolveRegionalVariant(ctx, rec.UserID)
	s.applyUserModelPreferences(ctx, root, rec.UserID)
	root.EventSink = func(e *cpn.Event) {
		s.mu.RLock()
		cb := s.onEvent
		s.mu.RUnlock()
		if cb != nil {
			cb(sessionID, *e)
		}
		s.persistEvent(sessionID, e)
	}
	root.Metrics = cpn.NewMetricsRecorder()

	session := cpn.NewSession(sessionID, rec.UserID, channel, root)
	// Preserve the original CreatedAt from persistence so SessionInfo snapshots
	// match what the client saw before the restart.
	session.CreatedAt = rec.CreatedAt

	// Wire HITL channels (topology may declare HITL transitions even if no
	// execution is currently waiting).
	for _, t := range root.Transitions {
		if t.Kind != cpn.NodeKindHITL {
			continue
		}
		if t.HITLConfig == nil {
			t.HITLConfig = &cpn.HITLConfig{}
		}
		ch := make(chan cpn.Token, 1)
		t.HITLConfig.Channel = ch
		if err := session.RegisterHITL(t.ID, ch); err != nil {
			s.logger.Error("rehydrate register HITL channel",
				"session_id", sessionID,
				"transition", t.ID,
				"error", err,
			)
		}
	}

	// Best-effort personality and tool injection (same as CreateSession).
	s.injectPersonality(ctx, root, rec.UserID)
	if s.toolRegistry != nil {
		s.toolRegistry.InjectIntoCPN(root)
	}

	// REQ-006: replay message history so SessionInfo.Messages is not silently
	// empty after a restart. Messages are already on rec.Messages from Get().
	for _, mr := range rec.Messages {
		if mr == nil {
			continue
		}
		session.AppendMessage(&cpn.Message{
			ID:              mr.ID,
			Role:            cpn.MessageRole(mr.Role),
			Content:         mr.Content,
			CPNID:           mr.CPNID,
			CPNRole:         mr.CPNRole,
			CPNDepth:        mr.CPNDepth,
			Timestamp:       mr.Timestamp,
			ParentMessageID: mr.ParentMessageID,
		})
	}

	s.logger.Info("session rehydrated",
		"session_id", sessionID,
		"user_id", rec.UserID,
		"source", "postgres",
		"duration_ms", time.Since(start).Milliseconds(),
		"messages", len(rec.Messages),
	)
	// TODO(metrics): session_rehydration_total{result="hit"}++ and
	// session_rehydration_duration_seconds.observe(duration).

	return session, nil
}

// generateSessionID produces a cryptographically random 32-character hex string.
func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// autoTitle generates a session title from the first user message.
// Truncates to 80 characters at a word boundary.
func autoTitle(content string) string {
	const maxLen = 80
	if len(content) <= maxLen {
		return content
	}
	// Find last space before maxLen to truncate at word boundary.
	truncated := content[:maxLen]
	lastSpace := -1
	for i := len(truncated) - 1; i >= 0; i-- {
		if truncated[i] == ' ' {
			lastSpace = i
			break
		}
	}
	if lastSpace > 0 {
		return truncated[:lastSpace] + "..."
	}
	return truncated + "..."
}

// findSourcePlace returns the first place with no incoming transitions (source).
func findSourcePlace(c *cpn.CPN) *cpn.Place {
	outputRefs := make(map[string]bool)
	for _, t := range c.Transitions {
		for _, pid := range t.OutputPlaces {
			outputRefs[pid] = true
		}
	}
	for id, p := range c.Places {
		if !outputRefs[id] {
			return p
		}
	}
	return nil
}

// defaultHostSnapshotTTL is the REQ-021 24h freshness window.
const defaultHostSnapshotTTL = 24 * time.Hour

// ensureHostCapabilitiesSeed seeds root's p-host-capabilities place with the
// latest snapshot from the repository. If the snapshot is missing or older
// than hostSnapshotTTL (default 24h), it first runs host-discovery-cpn
// synchronously, stores its result via the repo, and seeds the fresh
// snapshot. Failures are logged and swallowed — the session proceeds without
// the seed and downstream consumers PEEK for (snap, false).
func (s *SessionService) ensureHostCapabilitiesSeed(ctx context.Context, root *cpn.CPN) {
	if root == nil || s.hostCapabilityRepo == nil {
		return
	}
	ttl := s.hostSnapshotTTL
	if ttl <= 0 {
		ttl = defaultHostSnapshotTTL
	}

	hostID := s.resolveHostID(ctx)
	snap, err := s.hostCapabilityRepo.LatestForHost(ctx, hostID)
	fresh := err == nil && !snap.CapturedAt.IsZero() && time.Since(snap.CapturedAt) <= ttl

	if fresh {
		cpn.SeedHostSnapshot(root, snap)
		return
	}

	// Cache miss or stale — run discovery inline (one-shot). The factory
	// may be nil (e.g. persistence disabled in tests); in that case we
	// still tolerate the session start without a seed.
	if s.hostDiscoveryFactory == nil {
		if err != nil && !errorsIsHostSnapshotNotFound(err) {
			s.logger.Warn("host capability lookup failed",
				slog.String("host_id", hostID),
				slog.Any("error", err),
			)
		}
		return
	}

	if runSnap, runErr := s.runHostDiscovery(ctx); runErr == nil {
		cpn.SeedHostSnapshot(root, runSnap)
	} else {
		s.logger.Warn("host discovery run failed; session starts without host-capabilities seed",
			slog.Any("error", runErr),
		)
	}
}

// runHostDiscovery spawns host-discovery-cpn as a sub-CPN at depth 1, waits
// for it to complete, and returns the deposited HostCapabilitySnapshot (if
// any). The caller owns the decision about what to do with it.
func (s *SessionService) runHostDiscovery(ctx context.Context) (persist.HostCapabilitySnapshot, error) {
	if s.hostDiscoveryFactory == nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("no host discovery factory")
	}
	sessionID := "host-discovery-bootstrap"
	subCPN := s.hostDiscoveryFactory(sessionID)
	if subCPN == nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("host discovery factory returned nil")
	}
	subCPN.HostRuntime = s.hostRuntime
	subCPN.Depth = 1

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := subCPN.Run(runCtx); err != nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("run host discovery: %w", err)
	}
	// Scan terminal place(s) for a snapshot token.
	for _, p := range subCPN.Places {
		if p.ID != "p-snapshot" {
			continue
		}
		if tokens, ok := p.Peek(); ok && len(tokens) > 0 {
			if snap, ok := tokens[0].Payload.(persist.HostCapabilitySnapshot); ok {
				return snap, nil
			}
		}
	}
	return persist.HostCapabilitySnapshot{}, fmt.Errorf("host-discovery-cpn completed without snapshot")
}

// resolveHostID reads /etc/machine-id (via the host adapter if available,
// else direct os.ReadFile as a fallback). On failure it returns a
// deterministic hash of hostname so the repo's host_id column is never
// empty.
func (s *SessionService) resolveHostID(ctx context.Context) string {
	// Preferred: HostAdapter (hexagonal). The default ReadFile jail rejects
	// /etc/machine-id, so we fall back to the direct read below.
	if s.hostRuntime != nil && s.hostRuntime.Adapter != nil {
		if data, err := s.hostRuntime.Adapter.ReadFile(ctx, "/etc/machine-id"); err == nil {
			id := trimMachineID(data)
			if id != "" {
				return id
			}
		}
	}
	if data, err := osReadFileHostID("/etc/machine-id"); err == nil {
		id := trimMachineID(data)
		if id != "" {
			return id
		}
	}
	// Fallback: hash of hostname.
	host, _ := osHostname()
	return fallbackMachineIDHash(host)
}

// trimMachineID strips whitespace/newlines from /etc/machine-id contents.
func trimMachineID(data []byte) string {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		if b == '\n' || b == '\r' || b == ' ' || b == '\t' {
			continue
		}
		out = append(out, b)
	}
	return string(out)
}

// errorsIsHostSnapshotNotFound isolates the import of persist for the
// bootstrap helper so the rest of the file doesn't pick up a dependency on
// the sentinel.
func errorsIsHostSnapshotNotFound(err error) bool {
	return errors.Is(err, persist.ErrHostSnapshotNotFound)
}
