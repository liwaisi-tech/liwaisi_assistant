package tool

import (
	"sort"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// CatalogEntry holds a tool category's metadata and its factory for lazy
// registration into a Registry.
type CatalogEntry struct {
	Info    valueobject.ToolCategoryInfo
	Factory func(registry *Registry)
}

// Catalog holds metadata about all available tool categories without
// exposing their full JSON schemas. It is read-heavy and safe for
// concurrent access.
type Catalog struct {
	mu      sync.RWMutex
	entries map[valueobject.ToolCategory]*CatalogEntry
}

// NewCatalog creates an empty tool catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		entries: make(map[valueobject.ToolCategory]*CatalogEntry),
	}
}

// RegisterCategory adds a category with its metadata and factory function.
// If the category is already registered it is silently overwritten.
func (c *Catalog) RegisterCategory(entry *CatalogEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[entry.Info.Category] = entry
}

// List returns metadata for all registered categories, sorted by category name.
func (c *Catalog) List() []valueobject.ToolCategoryInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	infos := make([]valueobject.ToolCategoryInfo, 0, len(c.entries))
	for _, e := range c.entries {
		infos = append(infos, e.Info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Category < infos[j].Category
	})
	return infos
}

// Get returns the catalog entry for a specific category.
func (c *Catalog) Get(cat valueobject.ToolCategory) (*CatalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[cat]
	return e, ok
}

// Categories returns the names of all registered categories, sorted alphabetically.
func (c *Catalog) Categories() []valueobject.ToolCategory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cats := make([]valueobject.ToolCategory, 0, len(c.entries))
	for k := range c.entries {
		cats = append(cats, k)
	}
	sort.Slice(cats, func(i, j int) bool {
		return cats[i] < cats[j]
	})
	return cats
}
