package tool

import (
	"context"
	"encoding/json"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Registrar accepts tool registrations. Both Registry and ActiveRegistry
// implement this interface, allowing registration functions (RegisterWhoAmI,
// RegisterFindSkills, etc.) to work with either type.
type Registrar interface {
	Register(def valueobject.ToolDefinition, handler Handler)
}

// Executor provides tool execution capabilities.
// Both Registry and ActiveRegistry implement this interface, allowing the
// agent service to work with either eager or JIT loading transparently.
type Executor interface {
	// Definitions returns all currently registered tool definitions for
	// inclusion in LLM requests.
	Definitions() []valueobject.ToolDefinition

	// Execute runs the named tool with the given JSON arguments.
	Execute(ctx context.Context, name string, args json.RawMessage) (string, error)

	// Has reports whether the executor has any registered tools.
	Has() bool
}

// SessionExecutorProvider allows looking up an executor by session ID.
type SessionExecutorProvider interface {
	GetExecutor(sessionID string) Executor
}

// compile-time checks.
var (
	_ Executor  = (*Registry)(nil)
	_ Executor  = (*ScopedRegistry)(nil)
	_ Registrar = (*Registry)(nil)
	_ Registrar = (*ActiveRegistry)(nil)
)
