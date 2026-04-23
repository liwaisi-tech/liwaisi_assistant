package cpn

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CPNRoleEphemeral is the sentinel CPNRole value for messages that the
// LLM saw in their producing turn but should not be replayed in the T3
// sliding window of subsequent turns. Used for verbose read-only tool
// outputs (large `ls`, `cat`, query result dumps) whose value evaporates
// after the turn that consumed them. See spec REQ-004.
const CPNRoleEphemeral = "__ephemeral__"

// IsEphemeral reports whether m is a tool result tagged ephemeral. T3
// assembly skips these so they do not consume window slots after the
// turn that produced them.
func IsEphemeral(m *Message) bool {
	return m != nil && m.CPNRole == CPNRoleEphemeral
}

// LedgerSink is the side-channel a tool executor uses to record a
// ledger entry against the running CPN. Tool executors do not have a
// CPN handle, so fireTool installs a sink on context before calling
// the executor; tools retrieve it via LedgerSinkFromContext.
//
// Append is idempotent at the append level (it just adds to history);
// the caller's responsibility is to construct the Message via
// LedgerSuccess / LedgerFailure first.
type LedgerSink interface {
	Append(*Message)
}

// ledgerSinkKey is the context key for LedgerSink. Named (not struct{})
// for log/doc readability; mirrors the WriteIntentKeyType pattern.
type ledgerSinkKeyType struct{}

var ledgerSinkKey = ledgerSinkKeyType{}

// WithLedgerSink returns a child context carrying sink. Returns ctx
// unchanged when sink is nil so callers can pass through optional
// sinks without an extra branch.
func WithLedgerSink(ctx context.Context, sink LedgerSink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, ledgerSinkKey, sink)
}

// cpnLedgerSink adapts a *CPN to LedgerSink. The append takes c.mu
// under write so it is safe to call from a tool executor goroutine
// concurrently with executor-level history mutations.
type cpnLedgerSink struct{ c *CPN }

// Append implements LedgerSink. Safe for concurrent use.
func (s cpnLedgerSink) Append(m *Message) {
	if s.c == nil || m == nil {
		return
	}
	s.c.mu.Lock()
	s.c.History = append(s.c.History, m)
	s.c.mu.Unlock()
}

// LedgerSink returns a sink that appends to c.History under lock.
// Returns nil for a nil CPN so callers can chain through WithLedgerSink
// without an extra branch.
func (c *CPN) LedgerSink() LedgerSink {
	if c == nil {
		return nil
	}
	return cpnLedgerSink{c: c}
}

// LedgerSinkFromContext retrieves the sink installed by WithLedgerSink.
// Returns ok=false when no sink is present — tools MUST treat this as
// "ledger emission disabled in this caller", not as an error.
func LedgerSinkFromContext(ctx context.Context) (LedgerSink, bool) {
	if ctx == nil {
		return nil, false
	}
	v, ok := ctx.Value(ledgerSinkKey).(LedgerSink)
	return v, ok
}

// EmitLedgerSuccess constructs a success ledger entry and pushes it
// through the context-installed sink. No-op when no sink is present.
// Convenience wrapper for tool executors.
func EmitLedgerSuccess(ctx context.Context, verb LedgerVerb, target, size string) {
	sink, ok := LedgerSinkFromContext(ctx)
	if !ok {
		return
	}
	msg, err := LedgerSuccess(verb, target, size)
	if err != nil || msg == nil {
		return
	}
	sink.Append(msg)
}

// EmitLedgerFailure is the failure-side counterpart to EmitLedgerSuccess.
func EmitLedgerFailure(ctx context.Context, verb LedgerVerb, target, cause string) {
	sink, ok := LedgerSinkFromContext(ctx)
	if !ok {
		return
	}
	msg, err := LedgerFailure(verb, target, cause)
	if err != nil || msg == nil {
		return
	}
	sink.Append(msg)
}

// LedgerVerb names the action a state-changing tool performed. Defined
// as a string type rather than an enum to keep new tool authors from
// having to edit a central registry every time they add a verb.
type LedgerVerb string

// Canonical state-changing verbs. The set is intentionally small;
// buildWorkspacePreamble only surfaces lines whose verb is in
// stateChangingVerbs (memory.go).
const (
	LedgerVerbWrite LedgerVerb = "write"
	LedgerVerbEdit  LedgerVerb = "edit"
	LedgerVerbBuild LedgerVerb = "build"
	LedgerVerbChmod LedgerVerb = "chmod"
	LedgerVerbMv    LedgerVerb = "mv"
	LedgerVerbMkdir LedgerVerb = "mkdir"
	LedgerVerbCp    LedgerVerb = "cp"
)

// maxLedgerContent caps the rendered ledger line at 200 bytes
// (CON-010). Anything longer is truncated middle-elided.
const maxLedgerContent = 200

// maxLedgerCause caps the failure-cause snippet at 120 chars
// (REQ-021). Long stack traces or compiler dumps are reduced to
// their first error sentence.
const maxLedgerCause = 120

// LedgerSuccess builds a ledger Message recording a successful
// state-changing tool call. The returned Message MUST be appended to
// CPN.History — emission is the caller's responsibility so this helper
// stays I/O-free and trivially testable.
//
// Shape:
//
//	[ok] <verb> <target> (<size>)        when size != ""
//	[ok] <verb> <target>                 when size == ""
//
// Both shapes are recognised by parseLedgerLine in memory.go.
func LedgerSuccess(verb LedgerVerb, target, size string) (*Message, error) {
	if verb == "" || target == "" {
		return nil, fmt.Errorf("ledger: verb and target are required")
	}
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	target = elideMiddle(strings.TrimSpace(target), 100)
	var content string
	if size = strings.TrimSpace(size); size != "" {
		content = fmt.Sprintf("[ok] %s %s (%s)", verb, target, size)
	} else {
		content = fmt.Sprintf("[ok] %s %s", verb, target)
	}
	if len(content) > maxLedgerContent {
		content = elideMiddle(content, maxLedgerContent)
	}
	return &Message{
		ID:        id,
		Role:      RoleObserver,
		CPNRole:   CPNRoleLedger,
		Content:   sanitizeForLLM(content),
		Timestamp: time.Now(),
	}, nil
}

// LedgerFailure builds a ledger Message recording a failed
// state-changing tool call. The cause is truncated to maxLedgerCause
// chars and quoted so the LLM can recognise it as a verbatim error
// string and not free-form prose.
//
// Shape:
//
//	[fail] <verb> <target> → "<cause, ≤120 chars>"
func LedgerFailure(verb LedgerVerb, target, cause string) (*Message, error) {
	if verb == "" || target == "" {
		return nil, fmt.Errorf("ledger: verb and target are required")
	}
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	target = elideMiddle(strings.TrimSpace(target), 100)
	cause = compressCause(cause)
	content := fmt.Sprintf(`[fail] %s %s → %q`, verb, target, cause)
	if len(content) > maxLedgerContent {
		// Trim the cause, not the verb/target identifier.
		over := len(content) - maxLedgerContent
		if len(cause) > over+3 {
			cause = cause[:len(cause)-over-3] + "..."
			content = fmt.Sprintf(`[fail] %s %s → %q`, verb, target, cause)
		} else {
			content = elideMiddle(content, maxLedgerContent)
		}
	}
	return &Message{
		ID:        id,
		Role:      RoleObserver,
		CPNRole:   CPNRoleLedger,
		Content:   sanitizeForLLM(content),
		Timestamp: time.Now(),
	}, nil
}

// compressCause reduces an arbitrarily large error/stderr blob to a
// single-line root-cause snippet. Strategy:
//  1. Trim whitespace.
//  2. Collapse to first non-empty line (stack traces dump root cause first).
//  3. Truncate to maxLedgerCause chars with a trailing ellipsis.
func compressCause(cause string) string {
	cause = strings.TrimSpace(cause)
	if cause == "" {
		return "unknown error"
	}
	if nl := strings.IndexByte(cause, '\n'); nl >= 0 {
		cause = strings.TrimSpace(cause[:nl])
	}
	// Collapse runs of whitespace.
	cause = strings.Join(strings.Fields(cause), " ")
	if len(cause) > maxLedgerCause {
		cause = cause[:maxLedgerCause-3] + "..."
	}
	return cause
}

// IsLedgerEntry reports whether m is a ledger-tagged observer message.
// Cheap enough to call on every history element.
func IsLedgerEntry(m *Message) bool {
	return m != nil && m.Role == RoleObserver && m.CPNRole == CPNRoleLedger
}
