package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolapproval"
)

// DefaultToolApprovalTimeout is how long a single Gate.CheckSynthesized call
// waits for the operator to resolve the HITL prompt before we fail closed.
// The frontend component keeps the card mounted until the backend replies,
// so we want this long enough that an attentive user doesn't time out mid-
// decision, but short enough that a forgotten session doesn't pin a goroutine
// forever. 5 minutes matches the A2UI-level HITL flow ergonomics.
const DefaultToolApprovalTimeout = 5 * time.Minute

// ErrToolApprovalTimeout signals that the operator did not respond before
// the deadline. Callers (the Gate) treat this as DecisionDenied.
var ErrToolApprovalTimeout = errors.New("tool approval timed out")

// ErrUnknownToolApprovalRequest is returned from Resolve when the request_id
// does not correspond to a pending prompt (either never emitted, already
// resolved, or expired).
var ErrUnknownToolApprovalRequest = errors.New("unknown tool-approval request id")

// toolApprovalPending is the server-side state for a single outstanding
// prompt. The channel carries the operator's decision from Resolve back to
// PromptAndAwait.
type toolApprovalPending struct {
	sessionID string
	decision  chan toolapproval.Decision
}

// ToolApprovalBroker bridges toolapproval.Gate's fire-and-wait prompter
// contract to the HTTP+SSE surface. The Gate calls PromptAndAwait; the
// broker emits a `tool_approval_request` SSE event on the session stream
// and blocks the caller until the matching POST /tool-approvals arrives or
// the context / timeout fires.
type ToolApprovalBroker struct {
	sse     *SSEBroker
	logger  *slog.Logger
	timeout time.Duration

	mu      sync.Mutex
	pending map[string]*toolApprovalPending
}

// NewToolApprovalBroker constructs a broker wired to the given SSE publisher.
// A nil logger is tolerated (events are dropped silently) — production callers
// should always pass one.
func NewToolApprovalBroker(sse *SSEBroker, logger *slog.Logger) *ToolApprovalBroker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolApprovalBroker{
		sse:     sse,
		logger:  logger,
		timeout: DefaultToolApprovalTimeout,
		pending: make(map[string]*toolApprovalPending),
	}
}

// PromptAndAwait implements toolapproval.HITLPrompter — the Gate calls this
// when a synthesized tool hits the first-run HITL gate. The broker emits an
// SSE event and blocks until Resolve is called with the matching request_id,
// the context is cancelled, or the broker timeout expires. Fail-closed: any
// non-approve outcome returns DecisionDenied.
//
// sessionID is not on the HITLPrompter signature; it must be captured by the
// adapter per-call before invoking this method.
func (b *ToolApprovalBroker) PromptAndAwait(ctx context.Context, sessionID string, preview awakens.A2UIMessage) (toolapproval.Decision, error) {
	if b == nil {
		return toolapproval.DecisionDenied, errors.New("tool-approval broker not configured")
	}
	if sessionID == "" {
		return toolapproval.DecisionDenied, errors.New("tool-approval prompt: empty session id")
	}

	requestID := generateToolApprovalRequestID()
	ch := make(chan toolapproval.Decision, 1)

	b.mu.Lock()
	b.pending[requestID] = &toolApprovalPending{sessionID: sessionID, decision: ch}
	b.mu.Unlock()

	defer b.cleanup(requestID)

	payload := map[string]any{
		"request_id": requestID,
		"preview":    preview,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return toolapproval.DecisionDenied, err
	}
	if b.sse != nil {
		b.sse.Publish(sessionID, "tool_approval_request", data)
	}
	b.logger.Info("tool-approval prompt emitted",
		slog.String("session_id", sessionID),
		slog.String("request_id", requestID),
	)

	timeout := b.timeout
	if timeout <= 0 {
		timeout = DefaultToolApprovalTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case decision := <-ch:
		return decision, nil
	case <-ctx.Done():
		return toolapproval.DecisionDenied, ctx.Err()
	case <-timer.C:
		return toolapproval.DecisionDenied, ErrToolApprovalTimeout
	}
}

// Resolve delivers an operator decision to the goroutine blocked inside
// PromptAndAwait. Returns ErrUnknownToolApprovalRequest when no pending
// prompt matches — the caller surfaces this as 404.
func (b *ToolApprovalBroker) Resolve(sessionID, requestID string, decision toolapproval.Decision) error {
	if b == nil {
		return errors.New("tool-approval broker not configured")
	}
	if requestID == "" {
		return errors.New("tool-approval resolve: empty request id")
	}

	b.mu.Lock()
	p, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()
	if !ok {
		return ErrUnknownToolApprovalRequest
	}
	if sessionID != "" && p.sessionID != sessionID {
		return ErrUnknownToolApprovalRequest
	}
	select {
	case p.decision <- decision:
	default:
	}
	return nil
}

// cleanup removes a pending entry regardless of whether Resolve or timeout
// fired. Safe to call twice.
func (b *ToolApprovalBroker) cleanup(requestID string) {
	b.mu.Lock()
	delete(b.pending, requestID)
	b.mu.Unlock()
}

func generateToolApprovalRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "tap_" + hex.EncodeToString(buf)
}
