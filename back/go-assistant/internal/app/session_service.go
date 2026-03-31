package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
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

// NewSessionService creates a SessionService with the given dependencies.
func NewSessionService(llm cpn.LLMClient, cost cpn.CostProvider, logger *slog.Logger, factory TopologyFactory) *SessionService {
	return &SessionService{
		sessions:        make(map[string]*cpn.Session),
		states:          make(map[string]*sessionState),
		llm:             llm,
		cost:            cost,
		logger:          logger,
		topologyFactory: factory,
	}
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
	}

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

	st := &sessionState{state: cpn.StateIdle}

	s.mu.Lock()
	s.sessions[id] = session
	s.states[id] = st
	s.mu.Unlock()

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

	if st.get() == cpn.StateRunning {
		return fmt.Errorf("%w: %s", ErrSessionBusy, sessionID)
	}

	msg := &cpn.Message{
		ID:        sessionID + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Role:      cpn.RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	}
	session.AppendMessage(msg)

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
			return
		}

		// Check if any LLM transition used streaming (content already delivered via EventSink).
		streamingActive := hasStreamingTransition(session.Root)

		// Collect output from terminal places and append as assistant messages.
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
				// Always persist to session history.
				session.AppendMessage(&cpn.Message{
					ID:        sessionID + "-resp-" + fmt.Sprintf("%d", time.Now().UnixNano()),
					Role:      cpn.RoleAssistant,
					Content:   content,
					CPNID:     session.Root.ID,
					CPNRole:   session.Root.Role,
					CPNDepth:  session.Root.Depth,
					Timestamp: time.Now(),
				})
				// Only send content via Stream if it was NOT already streamed via the broker.
				if !streamingActive {
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
		// Send done sentinel so SSE handler knows this response is complete.
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
		// Set idle instead of completed — session can accept more messages.
		st.set(cpn.StateIdle)
		// Do NOT close the stream — SSE connection stays alive for next message.
	}()

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

// SetEventCallback registers a callback invoked on every CPN event.
func (s *SessionService) SetEventCallback(fn func(string, cpn.Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

// generateSessionID produces a cryptographically random 32-character hex string.
func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hasStreamingTransition returns true if any LLM transition in the CPN has StreamOutput enabled.
func hasStreamingTransition(c *cpn.CPN) bool {
	for _, t := range c.Transitions {
		if t.Kind == cpn.NodeKindLLM && t.LLMConfig != nil && t.LLMConfig.StreamOutput {
			return true
		}
	}
	return false
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
