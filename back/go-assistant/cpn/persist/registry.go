package persist

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// DefaultRegistry is the package-level function registry.
var DefaultRegistry = NewFuncRegistry()

// FuncRegistry maps string names to Go functions for CPN topology serialization.
// Go functions cannot be marshaled; the registry maps string names to func values
// at startup, enabling topology JSON to store func references as string keys.
// Thread-safe via sync.RWMutex.
type FuncRegistry struct {
	guards       map[string]func([]*cpn.Token) bool
	executors    map[string]func(context.Context, cpn.Token) (cpn.Token, error)
	retryOns     map[string]func(error, int) bool
	factories    map[string]func() *cpn.CPN
	eventFilters map[string]func(cpn.Event) bool
	validateFns  map[string]func(any) error
	onSuccessFns map[string]func(any) any
	schemaFns    map[string]func() any

	guardRev       map[uintptr]string
	executorRev    map[uintptr]string
	retryOnRev     map[uintptr]string
	factoryRev     map[uintptr]string
	eventFilterRev map[uintptr]string
	validateFnRev  map[uintptr]string
	onSuccessFnRev map[uintptr]string

	mu sync.RWMutex
}

// NewFuncRegistry creates an empty FuncRegistry.
func NewFuncRegistry() *FuncRegistry {
	return &FuncRegistry{
		guards:         make(map[string]func([]*cpn.Token) bool),
		executors:      make(map[string]func(context.Context, cpn.Token) (cpn.Token, error)),
		retryOns:       make(map[string]func(error, int) bool),
		factories:      make(map[string]func() *cpn.CPN),
		eventFilters:   make(map[string]func(cpn.Event) bool),
		validateFns:    make(map[string]func(any) error),
		onSuccessFns:   make(map[string]func(any) any),
		schemaFns:      make(map[string]func() any),
		guardRev:       make(map[uintptr]string),
		executorRev:    make(map[uintptr]string),
		retryOnRev:     make(map[uintptr]string),
		factoryRev:     make(map[uintptr]string),
		eventFilterRev: make(map[uintptr]string),
		validateFnRev:  make(map[uintptr]string),
		onSuccessFnRev: make(map[uintptr]string),
	}
}

func funcPtr(fn any) uintptr {
	return reflect.ValueOf(fn).Pointer()
}

// RegisterGuard registers a guard function. Panics on duplicate name.
func (r *FuncRegistry) RegisterGuard(name string, fn func([]*cpn.Token) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.guards[name]; ok {
		panic(fmt.Sprintf("persist: duplicate guard registration: %q", name))
	}
	r.guards[name] = fn
	r.guardRev[funcPtr(fn)] = name
}

// LookupGuard returns a guard function by name.
func (r *FuncRegistry) LookupGuard(name string) (func([]*cpn.Token) bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.guards[name]
	return fn, ok
}

// ReverseLookupGuard returns the name for a guard function.
func (r *FuncRegistry) ReverseLookupGuard(fn func([]*cpn.Token) bool) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.guardRev[funcPtr(fn)]
	return name, ok
}

// RegisterExecutor registers an executor function. Panics on duplicate name.
func (r *FuncRegistry) RegisterExecutor(name string, fn func(context.Context, cpn.Token) (cpn.Token, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.executors[name]; ok {
		panic(fmt.Sprintf("persist: duplicate executor registration: %q", name))
	}
	r.executors[name] = fn
	r.executorRev[funcPtr(fn)] = name
}

// LookupExecutor returns an executor function by name.
func (r *FuncRegistry) LookupExecutor(name string) (func(context.Context, cpn.Token) (cpn.Token, error), bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.executors[name]
	return fn, ok
}

// ReverseLookupExecutor returns the name for an executor function.
func (r *FuncRegistry) ReverseLookupExecutor(fn func(context.Context, cpn.Token) (cpn.Token, error)) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.executorRev[funcPtr(fn)]
	return name, ok
}

// RegisterRetryOn registers a retry-on function. Panics on duplicate name.
func (r *FuncRegistry) RegisterRetryOn(name string, fn func(error, int) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.retryOns[name]; ok {
		panic(fmt.Sprintf("persist: duplicate retryOn registration: %q", name))
	}
	r.retryOns[name] = fn
	r.retryOnRev[funcPtr(fn)] = name
}

// LookupRetryOn returns a retry-on function by name.
func (r *FuncRegistry) LookupRetryOn(name string) (func(error, int) bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.retryOns[name]
	return fn, ok
}

// ReverseLookupRetryOn returns the name for a retry-on function.
func (r *FuncRegistry) ReverseLookupRetryOn(fn func(error, int) bool) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.retryOnRev[funcPtr(fn)]
	return name, ok
}

// RegisterSubNetFactory registers a subnet factory function. Panics on duplicate name.
func (r *FuncRegistry) RegisterSubNetFactory(name string, fn func() *cpn.CPN) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[name]; ok {
		panic(fmt.Sprintf("persist: duplicate factory registration: %q", name))
	}
	r.factories[name] = fn
	r.factoryRev[funcPtr(fn)] = name
}

// LookupSubNetFactory returns a subnet factory function by name.
func (r *FuncRegistry) LookupSubNetFactory(name string) (func() *cpn.CPN, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.factories[name]
	return fn, ok
}

// ReverseLookupSubNetFactory returns the name for a subnet factory function.
func (r *FuncRegistry) ReverseLookupSubNetFactory(fn func() *cpn.CPN) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.factoryRev[funcPtr(fn)]
	return name, ok
}

// RegisterEventFilter registers an event filter function. Panics on duplicate name.
func (r *FuncRegistry) RegisterEventFilter(name string, fn func(cpn.Event) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.eventFilters[name]; ok {
		panic(fmt.Sprintf("persist: duplicate eventFilter registration: %q", name))
	}
	r.eventFilters[name] = fn
	r.eventFilterRev[funcPtr(fn)] = name
}

// LookupEventFilter returns an event filter function by name.
func (r *FuncRegistry) LookupEventFilter(name string) (func(cpn.Event) bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.eventFilters[name]
	return fn, ok
}

// ReverseLookupEventFilter returns the name for an event filter function.
func (r *FuncRegistry) ReverseLookupEventFilter(fn func(cpn.Event) bool) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.eventFilterRev[funcPtr(fn)]
	return name, ok
}

// RegisterValidateFunc registers a validate function. Panics on duplicate name.
func (r *FuncRegistry) RegisterValidateFunc(name string, fn func(any) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.validateFns[name]; ok {
		panic(fmt.Sprintf("persist: duplicate validateFunc registration: %q", name))
	}
	r.validateFns[name] = fn
	r.validateFnRev[funcPtr(fn)] = name
}

// LookupValidateFunc returns a validate function by name.
func (r *FuncRegistry) LookupValidateFunc(name string) (func(any) error, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.validateFns[name]
	return fn, ok
}

// ReverseLookupValidateFunc returns the name for a validate function.
func (r *FuncRegistry) ReverseLookupValidateFunc(fn func(any) error) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.validateFnRev[funcPtr(fn)]
	return name, ok
}

// RegisterOnSuccess registers an on-success function. Panics on duplicate name.
func (r *FuncRegistry) RegisterOnSuccess(name string, fn func(any) any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.onSuccessFns[name]; ok {
		panic(fmt.Sprintf("persist: duplicate onSuccess registration: %q", name))
	}
	r.onSuccessFns[name] = fn
	r.onSuccessFnRev[funcPtr(fn)] = name
}

// LookupOnSuccess returns an on-success function by name.
func (r *FuncRegistry) LookupOnSuccess(name string) (func(any) any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.onSuccessFns[name]
	return fn, ok
}

// ReverseLookupOnSuccess returns the name for an on-success function.
func (r *FuncRegistry) ReverseLookupOnSuccess(fn func(any) any) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.onSuccessFnRev[funcPtr(fn)]
	return name, ok
}

// RegisterSchema registers a schema factory function. Panics on duplicate name.
func (r *FuncRegistry) RegisterSchema(name string, factory func() any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schemaFns[name]; ok {
		panic(fmt.Sprintf("persist: duplicate schema registration: %q", name))
	}
	r.schemaFns[name] = factory
}

// LookupSchema returns a schema factory function by name.
func (r *FuncRegistry) LookupSchema(name string) (func() any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.schemaFns[name]
	return fn, ok
}
