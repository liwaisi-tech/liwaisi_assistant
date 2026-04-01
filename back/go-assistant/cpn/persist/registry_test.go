package persist

import (
	"context"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestFuncRegistry_Guards(t *testing.T) {
	fn := func(tokens []*cpn.Token) bool { return true }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterGuard("always-true", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterGuard("always-true", fn)
		got, ok := r.LookupGuard("always-true")
		if !ok || got == nil {
			t.Fatal("LookupGuard should find registered func")
		}
	})

	t.Run("LookupMiss", func(t *testing.T) {
		r := NewFuncRegistry()
		_, ok := r.LookupGuard("nonexistent")
		if ok {
			t.Fatal("LookupGuard should miss")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterGuard("always-true", fn)
		name, ok := r.ReverseLookupGuard(fn)
		if !ok || name != "always-true" {
			t.Fatalf("ReverseLookup: got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterGuard("always-true", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on duplicate registration")
			}
		}()
		r.RegisterGuard("always-true", fn)
	})
}

func TestFuncRegistry_Executors(t *testing.T) {
	fn := func(_ context.Context, tok cpn.Token) (cpn.Token, error) { return tok, nil }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterExecutor("echo", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterExecutor("echo", fn)
		got, ok := r.LookupExecutor("echo")
		if !ok || got == nil {
			t.Fatal("LookupExecutor should find registered func")
		}
	})

	t.Run("LookupMiss", func(t *testing.T) {
		r := NewFuncRegistry()
		_, ok := r.LookupExecutor("nonexistent")
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterExecutor("echo", fn)
		name, ok := r.ReverseLookupExecutor(fn)
		if !ok || name != "echo" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterExecutor("echo", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterExecutor("echo", fn)
	})
}

func TestFuncRegistry_RetryOns(t *testing.T) {
	fn := func(_ error, _ int) bool { return true }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterRetryOn("always", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterRetryOn("always", fn)
		got, ok := r.LookupRetryOn("always")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("LookupMiss", func(t *testing.T) {
		r := NewFuncRegistry()
		_, ok := r.LookupRetryOn("nonexistent")
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterRetryOn("always", fn)
		name, ok := r.ReverseLookupRetryOn(fn)
		if !ok || name != "always" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterRetryOn("always", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterRetryOn("always", fn)
	})
}

func TestFuncRegistry_Factories(t *testing.T) {
	fn := func() *cpn.CPN { return &cpn.CPN{ID: "test"} }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSubNetFactory("test-factory", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSubNetFactory("test-factory", fn)
		got, ok := r.LookupSubNetFactory("test-factory")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("LookupMiss", func(t *testing.T) {
		r := NewFuncRegistry()
		_, ok := r.LookupSubNetFactory("nonexistent")
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSubNetFactory("test-factory", fn)
		name, ok := r.ReverseLookupSubNetFactory(fn)
		if !ok || name != "test-factory" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSubNetFactory("test-factory", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterSubNetFactory("test-factory", fn)
	})
}

func TestFuncRegistry_EventFilters(t *testing.T) {
	fn := func(_ cpn.Event) bool { return true }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterEventFilter("all-events", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterEventFilter("all-events", fn)
		got, ok := r.LookupEventFilter("all-events")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterEventFilter("all-events", fn)
		name, ok := r.ReverseLookupEventFilter(fn)
		if !ok || name != "all-events" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterEventFilter("all-events", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterEventFilter("all-events", fn)
	})
}

func TestFuncRegistry_ValidateFuncs(t *testing.T) {
	fn := func(_ any) error { return nil }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterValidateFunc("json-v1", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterValidateFunc("json-v1", fn)
		got, ok := r.LookupValidateFunc("json-v1")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterValidateFunc("json-v1", fn)
		name, ok := r.ReverseLookupValidateFunc(fn)
		if !ok || name != "json-v1" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterValidateFunc("json-v1", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterValidateFunc("json-v1", fn)
	})
}

func TestFuncRegistry_OnSuccessFuncs(t *testing.T) {
	fn := func(v any) any { return v }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterOnSuccess("identity", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterOnSuccess("identity", fn)
		got, ok := r.LookupOnSuccess("identity")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("ReverseLookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterOnSuccess("identity", fn)
		name, ok := r.ReverseLookupOnSuccess(fn)
		if !ok || name != "identity" {
			t.Fatalf("got %q, %v", name, ok)
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterOnSuccess("identity", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterOnSuccess("identity", fn)
	})
}

func TestFuncRegistry_Schemas(t *testing.T) {
	fn := func() any { return struct{ Name string }{} }

	t.Run("Register", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSchema("user-profile", fn)
	})

	t.Run("Lookup", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSchema("user-profile", fn)
		got, ok := r.LookupSchema("user-profile")
		if !ok || got == nil {
			t.Fatal("should find")
		}
	})

	t.Run("DuplicatePanic", func(t *testing.T) {
		r := NewFuncRegistry()
		r.RegisterSchema("user-profile", fn)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		r.RegisterSchema("user-profile", fn)
	})
}

func TestFuncRegistry_Concurrent(t *testing.T) {
	r := NewFuncRegistry()
	const goroutines = 20

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine registers a unique func
			guardFn := func(tokens []*cpn.Token) bool { return len(tokens) > i }
			name := "guard-" + string(rune('a'+i))
			r.RegisterGuard(name, guardFn)
			r.LookupGuard(name)
			r.ReverseLookupGuard(guardFn)
		}(i)
	}
	wg.Wait()
}

func TestFuncRegistry_ReverseLookupMiss(t *testing.T) {
	r := NewFuncRegistry()

	t.Run("Guard", func(t *testing.T) {
		fn := func(_ []*cpn.Token) bool { return false }
		_, ok := r.ReverseLookupGuard(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("Executor", func(t *testing.T) {
		fn := func(_ context.Context, tok cpn.Token) (cpn.Token, error) { return tok, nil }
		_, ok := r.ReverseLookupExecutor(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("RetryOn", func(t *testing.T) {
		fn := func(_ error, _ int) bool { return false }
		_, ok := r.ReverseLookupRetryOn(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("Factory", func(t *testing.T) {
		fn := func() *cpn.CPN { return nil }
		_, ok := r.ReverseLookupSubNetFactory(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("EventFilter", func(t *testing.T) {
		fn := func(_ cpn.Event) bool { return false }
		_, ok := r.ReverseLookupEventFilter(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("ValidateFunc", func(t *testing.T) {
		fn := func(_ any) error { return nil }
		_, ok := r.ReverseLookupValidateFunc(fn)
		if ok {
			t.Fatal("should miss")
		}
	})

	t.Run("OnSuccess", func(t *testing.T) {
		fn := func(v any) any { return v }
		_, ok := r.ReverseLookupOnSuccess(fn)
		if ok {
			t.Fatal("should miss")
		}
	})
}
