package subagent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

// reservedNames lists built-in agent names that cannot be overwritten by users.
var reservedNames = map[string]bool{
	"planner": true,
}

// Router maintains a registry of SubAgentSpec definitions and provides
// deterministic name-based routing. It is safe for concurrent use.
type Router struct {
	mu    sync.RWMutex
	specs map[string]entity.SubAgentSpec
}

// NewRouter creates an empty Router.
func NewRouter() *Router {
	return &Router{
		specs: make(map[string]entity.SubAgentSpec),
	}
}

// Register adds a SubAgentSpec to the router. If a spec with the same
// name already exists and is not a reserved built-in, it is overwritten.
// Returns an error if the name is reserved for a built-in agent.
func (r *Router) Register(spec *entity.SubAgentSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reservedNames[spec.Name] {
		return fmt.Errorf("subagent router: %q is a reserved built-in name and cannot be overwritten", spec.Name)
	}
	r.specs[spec.Name] = *spec
	return nil
}

// RegisterBuiltIn unconditionally registers a built-in SubAgentSpec,
// bypassing the reserved-name guard. Should only be called at startup.
func (r *Router) RegisterBuiltIn(spec *entity.SubAgentSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[spec.Name] = *spec
}

// Get returns the SubAgentSpec for the given name. The boolean reports
// whether the name was found.
func (r *Router) Get(name string) (entity.SubAgentSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	return spec, ok
}

// List returns all registered SubAgentSpecs sorted alphabetically by name.
func (r *Router) List() []entity.SubAgentSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]entity.SubAgentSpec, 0, len(r.specs))
	for k := range r.specs {
		result = append(result, r.specs[k])
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
