// Package tool provides a registry for agent tool definitions and handlers.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Handler is a function that executes a tool and returns a string result.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Registry maps tool names to their definitions and handlers.
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]valueobject.ToolDefinition
	handlers    map[string]Handler
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		definitions: make(map[string]valueobject.ToolDefinition),
		handlers:    make(map[string]Handler),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(def valueobject.ToolDefinition, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions[def.Function.Name] = def
	r.handlers[def.Function.Name] = handler
}

// Definitions returns all registered tool definitions for the LLM request.
func (r *Registry) Definitions() []valueobject.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]valueobject.ToolDefinition, 0, len(r.definitions))
	for _, d := range r.definitions {
		defs = append(defs, d)
	}
	return defs
}

// Execute runs the named tool with the given arguments.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return handler(ctx, args)
}

// Has reports whether the registry contains any tools.
func (r *Registry) Has() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.definitions) > 0
}

// GetExecutor implements SessionExecutorProvider by returning the registry
// itself, ignoring the session ID.
func (r *Registry) GetExecutor(_ string) Executor {
	return r
}
