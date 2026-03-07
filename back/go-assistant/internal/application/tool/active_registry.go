package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ActiveRegistry wraps a Registry with session-scoped JIT loading capabilities.
// It starts with only bootstrap tools and loads categories on demand from the
// catalog. All operations are safe for concurrent use.
type ActiveRegistry struct {
	mu       sync.RWMutex
	registry *Registry
	catalog  *Catalog
	loaded   map[valueobject.ToolCategory]bool
}

// NewActiveRegistry creates a new ActiveRegistry backed by the given catalog.
// The underlying Registry starts empty; bootstrap tools and on-demand
// categories are registered separately.
func NewActiveRegistry(catalog *Catalog) *ActiveRegistry {
	return &ActiveRegistry{
		registry: NewRegistry(),
		catalog:  catalog,
		loaded:   make(map[valueobject.ToolCategory]bool),
	}
}

// LoadCategory loads all tools from the named category into the active
// registry. Returns the category info. Idempotent: loading an
// already-loaded category is a no-op that returns the info.
func (ar *ActiveRegistry) LoadCategory(cat valueobject.ToolCategory) (*valueobject.ToolCategoryInfo, error) {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	entry, ok := ar.catalog.Get(cat)
	if !ok {
		return nil, fmt.Errorf("unknown tool category: %q", cat)
	}

	if ar.loaded[cat] {
		return &entry.Info, nil
	}

	entry.Factory(ar.registry)
	ar.loaded[cat] = true
	return &entry.Info, nil
}

// IsLoaded reports whether a category has been loaded into this registry.
func (ar *ActiveRegistry) IsLoaded(cat valueobject.ToolCategory) bool {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	return ar.loaded[cat]
}

// LoadedCategories returns the list of currently loaded categories, sorted
// alphabetically.
func (ar *ActiveRegistry) LoadedCategories() []valueobject.ToolCategory {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	cats := make([]valueobject.ToolCategory, 0, len(ar.loaded))
	for k := range ar.loaded {
		cats = append(cats, k)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	return cats
}

// LoadAll loads every category registered in the catalog. Used for eager
// mode backward compatibility.
func (ar *ActiveRegistry) LoadAll() error {
	for _, cat := range ar.catalog.Categories() {
		if _, err := ar.LoadCategory(cat); err != nil {
			return fmt.Errorf("loading category %q: %w", cat, err)
		}
	}
	return nil
}

// Register adds a single tool definition and handler to the underlying
// registry. Used for bootstrap tools that don't belong to a category.
func (ar *ActiveRegistry) Register(def valueobject.ToolDefinition, handler Handler) {
	ar.registry.Register(def, handler)
}

// Definitions returns all registered tool definitions (bootstrap + loaded).
func (ar *ActiveRegistry) Definitions() []valueobject.ToolDefinition {
	return ar.registry.Definitions()
}

// Execute runs the named tool with the given arguments.
func (ar *ActiveRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return ar.registry.Execute(ctx, name, args)
}

// Has reports whether the registry contains any tools.
func (ar *ActiveRegistry) Has() bool {
	return ar.registry.Has()
}

// compile-time check: ActiveRegistry implements Executor.
var _ Executor = (*ActiveRegistry)(nil)
