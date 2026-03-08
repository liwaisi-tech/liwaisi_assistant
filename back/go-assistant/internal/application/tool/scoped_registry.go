package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ScopedRegistry wraps an Executor and filters tool access based on
// allow/deny lists. It implements the Executor interface.
//
// When both lists are empty, all tools from the underlying executor are
// visible. When an allowlist is provided, only those tools are visible.
// The denylist always takes precedence — a tool present in both lists is
// denied.
//
// ScopedRegistry is safe for concurrent use because it delegates all
// state access to the underlying Executor, which must be safe for
// concurrent use itself. The allow/deny maps are immutable after
// construction.
type ScopedRegistry struct {
	executor Executor
	allowed  map[string]bool // if non-empty, only these tools are visible
	denied   map[string]bool // these tools are always hidden
}

// NewScopedRegistry creates a scoped view of the given executor.
// allowedTools: if non-empty, only these tools are visible (allowlist).
// deniedTools: these tools are always hidden (denylist, applied after allowlist).
func NewScopedRegistry(executor Executor, allowedTools, deniedTools []string) *ScopedRegistry {
	allowed := make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		allowed[t] = true
	}
	denied := make(map[string]bool, len(deniedTools))
	for _, t := range deniedTools {
		denied[t] = true
	}
	return &ScopedRegistry{
		executor: executor,
		allowed:  allowed,
		denied:   denied,
	}
}

func (s *ScopedRegistry) isAllowed(name string) bool {
	if s.denied[name] {
		return false
	}
	if len(s.allowed) > 0 {
		return s.allowed[name]
	}
	return true
}

// Definitions returns only the tool definitions that pass the scope filter.
func (s *ScopedRegistry) Definitions() []valueobject.ToolDefinition {
	if s.executor == nil {
		return nil
	}
	all := s.executor.Definitions()
	filtered := make([]valueobject.ToolDefinition, 0, len(all))
	for _, def := range all {
		if s.isAllowed(def.Function.Name) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// Execute runs the named tool if it passes the scope filter.
// Returns a descriptive error when the tool is denied or not in the allowlist.
func (s *ScopedRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if !s.isAllowed(name) {
		return "", fmt.Errorf("tool %q is not available in this scope", name)
	}
	if s.executor == nil {
		return "", fmt.Errorf("executor is not initialized")
	}
	return s.executor.Execute(ctx, name, args)
}

// Has reports whether any tools pass the scope filter.
func (s *ScopedRegistry) Has() bool {
	if s.executor == nil {
		return false
	}
	for _, def := range s.executor.Definitions() {
		if s.isAllowed(def.Function.Name) {
			return true
		}
	}
	return false
}
