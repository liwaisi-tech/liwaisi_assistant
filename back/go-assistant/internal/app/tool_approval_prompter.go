package app

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolapproval"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

// ToolApprovalPrompterPort is the minimal interface the app layer consumes
// to route synthesized-tool HITL prompts through an external transport
// (HTTP+SSE in production, stub in tests). Implemented by
// httpapi.ToolApprovalBroker.
type ToolApprovalPrompterPort interface {
	PromptAndAwait(ctx context.Context, sessionID string, preview awakens.A2UIMessage) (toolapproval.Decision, error)
}

// sessionScopedPrompter adapts the session-aware ToolApprovalPrompterPort to
// toolapproval.HITLPrompter, whose signature does not carry a session id.
// Instantiate one per Gate.CheckSynthesized call with the active session id
// baked in.
type sessionScopedPrompter struct {
	sessionID string
	port      ToolApprovalPrompterPort
}

// newSessionScopedPrompter captures sessionID + port; returns nil when port
// is nil so Gate wiring can detect the unconfigured case and fail-closed.
func newSessionScopedPrompter(sessionID string, port ToolApprovalPrompterPort) toolapproval.HITLPrompter {
	if port == nil || sessionID == "" {
		return nil
	}
	return &sessionScopedPrompter{sessionID: sessionID, port: port}
}

// PromptAndAwait implements toolapproval.HITLPrompter.
func (p *sessionScopedPrompter) PromptAndAwait(ctx context.Context, preview awakens.A2UIMessage) (toolapproval.Decision, error) {
	return p.port.PromptAndAwait(ctx, p.sessionID, preview)
}

// WithApprovalStore wires the SC-13 approval cache so synthesized-tool first-
// run HITL decisions persist across invocations in the same session. When
// unset, the approvalGate is not constructed and synthesized manifests that
// reach the tool-invocation path are denied (fail-closed).
func WithApprovalStore(store toolapproval.ApprovalStore) SessionServiceOption {
	return func(s *SessionService) { s.approvalStore = store }
}

// WithPendingToolStore overrides the default in-memory pending store with a
// persistent implementation (e.g. Postgres). The store is shared across
// sessions because approvals are keyed by (session_id, tool_name,
// provenance_sha256) at the store level.
func WithPendingToolStore(store toolsynth.PendingToolStore) SessionServiceOption {
	return func(s *SessionService) {
		if store != nil {
			s.pendingToolStore = store
		}
	}
}

// WithToolApprovalPrompter wires the transport that surfaces synthesized-
// tool HITL prompts to the operator. Production passes
// *httpapi.ToolApprovalBroker; tests stub it. Required alongside
// WithApprovalStore for the SC-13 gate to be operational.
func WithToolApprovalPrompter(p ToolApprovalPrompterPort) SessionServiceOption {
	return func(s *SessionService) { s.toolApprovalPrompter = p }
}

// SetToolApprovalPrompter injects the prompter after construction. Needed
// in the composition root because the HTTP-backed broker can only be built
// after the SSE broker exists (which is owned by the HTTP server).
func (s *SessionService) SetToolApprovalPrompter(p ToolApprovalPrompterPort) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolApprovalPrompter = p
}
