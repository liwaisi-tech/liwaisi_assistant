package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ToolSchema holds metadata describing a tool available in the CPN system.
type ToolSchema struct {
	Name         string          `json:"name"`
	Namespace    string          `json:"namespace"`
	Description  string          `json:"description"`
	InputColor   cpn.ColorSet    `json:"input_color"`
	OutputColor  cpn.ColorSet    `json:"output_color"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
	RequiresHITL bool            `json:"requires_hitl"`
	Version      string          `json:"version"`
}

// QualifiedName returns "namespace/name".
func (s *ToolSchema) QualifiedName() string {
	return s.Namespace + "/" + s.Name
}

// ToolEntry pairs a schema with its executor function.
type ToolEntry struct {
	Schema   *ToolSchema
	Executor func(ctx context.Context, in cpn.Token) (cpn.Token, error)
}

// Registry is a thread-safe tool registry keyed by qualified name.
// After sealing, only user-namespace tools can be registered.
type Registry struct {
	entries map[string]*ToolEntry
	sealed  bool
	mu      sync.RWMutex
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*ToolEntry),
	}
}

// Register adds a tool to the registry. Returns an error if:
//   - schema is nil or has empty Name/Namespace
//   - a tool with the same qualified name already exists
//   - namespace is "system" and the registry is sealed
func (r *Registry) Register(schema *ToolSchema, exec func(context.Context, cpn.Token) (cpn.Token, error)) error {
	if schema == nil || schema.Name == "" || schema.Namespace == "" {
		return ErrInvalidToolSchema
	}

	qn := schema.QualifiedName()

	r.mu.Lock()
	defer r.mu.Unlock()

	if schema.Namespace == "system" && r.sealed {
		return fmt.Errorf("%w: %s", ErrSystemNamespaceSealed, qn)
	}

	if _, exists := r.entries[qn]; exists {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, qn)
	}

	r.entries[qn] = &ToolEntry{
		Schema:   schema,
		Executor: exec,
	}
	return nil
}

// Seal locks the system namespace. After this call, only "user" tools
// can be registered.
func (r *Registry) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
}

// IsSealed reports whether the system namespace is locked.
func (r *Registry) IsSealed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sealed
}

// Resolve looks up a tool by its qualified name ("namespace/name").
func (r *Registry) Resolve(qualifiedName string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[qualifiedName]
	return entry, ok
}

// List returns all schemas registered under the given namespace.
func (r *Registry) List(namespace string) []*ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*ToolSchema
	for _, e := range r.entries {
		if e.Schema.Namespace == namespace {
			out = append(out, e.Schema)
		}
	}
	return out
}

// ListAll returns every registered tool schema.
func (r *Registry) ListAll() []*ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*ToolSchema, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.Schema)
	}
	return out
}

// AsLLMTools converts registered tools to []*cpn.LLMTool for injection into
// LLM transitions. If namespaces are provided, only tools in those namespaces
// are included. If none are provided, all tools are returned.
func (r *Registry) AsLLMTools(namespaces ...string) []*cpn.LLMTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filter := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		filter[ns] = struct{}{}
	}

	out := make([]*cpn.LLMTool, 0, len(r.entries))
	for _, e := range r.entries {
		if len(filter) > 0 {
			if _, ok := filter[e.Schema.Namespace]; !ok {
				continue
			}
		}
		out = append(out, &cpn.LLMTool{
			Name:        e.Schema.QualifiedName(),
			Description: e.Schema.Description,
			Parameters:  e.Schema.Parameters,
		})
	}
	return out
}

// registrationName converts a qualified name to a FuncRegistry-safe key.
// Pattern: "tool-exec-{namespace}-{name}" with dots replaced by dashes.
func registrationName(qn string) string {
	parts := strings.SplitN(qn, "/", 2)
	if len(parts) != 2 {
		return "tool-exec-" + strings.ReplaceAll(qn, ".", "-")
	}
	ns := strings.ReplaceAll(parts[0], ".", "-")
	name := strings.ReplaceAll(parts[1], ".", "-")
	return "tool-exec-" + ns + "-" + name
}

// InjectIntoCPN populates ToolMeta and Executor on CPN transitions from the registry.
// Call after topology creation and before CPN.Run().
func (r *Registry) InjectIntoCPN(c *cpn.CPN) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, t := range c.Transitions {
		if t.ToolName == "" {
			continue
		}
		entry, ok := r.entries[t.ToolName]
		if !ok {
			continue
		}
		t.ToolMeta = &cpn.ToolMeta{
			Description:  entry.Schema.Description,
			Parameters:   entry.Schema.Parameters,
			RequiresHITL: entry.Schema.RequiresHITL,
			Namespace:    entry.Schema.Namespace,
		}
		if t.Executor == nil {
			t.Executor = entry.Executor
		}
	}
}

// InjectIntoFuncRegistry registers every tool executor into the given
// FuncRegistry so that CPN topologies can serialize tool references as
// string keys. Registration name pattern: "tool-exec-{namespace}-{name}"
// (dots replaced with dashes).
func (r *Registry) InjectIntoFuncRegistry(fr *persist.FuncRegistry) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for qn, e := range r.entries {
		if e.Executor != nil {
			fr.RegisterExecutor(registrationName(qn), e.Executor)
		}
	}
}
