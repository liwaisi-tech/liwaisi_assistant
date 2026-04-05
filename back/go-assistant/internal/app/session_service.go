package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

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
	toolRegistry    *tools.Registry
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
func WithToolRegistry(reg *tools.Registry) SessionServiceOption {
	return func(s *SessionService) { s.toolRegistry = reg }
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

	source := findSourcePlace(session.Root)
	if source != nil {
		if err := source.Deposit(tok); err != nil {
			return fmt.Errorf("deposit token: %w", err)
		}
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
		// without this sync, those responses are lost when the CPN fails.
		if len(session.Root.History) > historyLen {
			for _, m := range session.Root.History[historyLen:] {
				if m.Role != cpn.RoleAssistant {
					continue // user messages already in session history
				}
				session.AppendMessage(&cpn.Message{
					ID:        sessionID + "-sync-" + fmt.Sprintf("%d", time.Now().UnixNano()),
					Role:      m.Role,
					Content:   m.Content,
					CPNID:     m.CPNID,
					CPNRole:   m.CPNRole,
					CPNDepth:  m.CPNDepth,
					Timestamp: m.Timestamp,
				})
			}
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
func (s *SessionService) ResolveHITL(ctx context.Context, sessionID, transitionID string, resp cpn.HITLResponse) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	return session.ResolveHITL(ctx, transitionID, resp)
}

// GetSession returns a read-only snapshot of the session.
func (s *SessionService) GetSession(sessionID string) (*SessionInfo, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	st := s.states[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	return &SessionInfo{
		ID:        session.ID,
		UserID:    session.UserID,
		Channel:   session.Channel,
		State:     st.get(),
		CreatedAt: session.CreatedAt,
		Messages:  session.Messages(),
	}, nil
}

// StreamChannel returns the session's streaming channel for SSE delivery.
func (s *SessionService) StreamChannel(sessionID string) (<-chan cpn.StreamChunk, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	return session.Stream, nil
}

// SendStreamChunk sends a stream chunk to the session's streaming channel.
// Used by the CPN execution engine to deliver LLM streaming output.
func (s *SessionService) SendStreamChunk(sessionID string, chunk cpn.StreamChunk) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
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

	// 2. Persist new assistant messages.
	if s.persist.Sessions != nil {
		for i := range newMessages {
			m := &newMessages[i]
			if m.Role != cpn.RoleAssistant {
				continue
			}
			rec := persist.MessageToRecord(sessionID, m)
			if err := s.persist.Sessions.AppendMessage(ctx, sessionID, rec); err != nil {
				s.logger.Warn("persist assistant message", "session_id", sessionID, "error", err)
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
