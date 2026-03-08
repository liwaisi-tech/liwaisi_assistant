package tool_test

import (
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func fakeBootstrap(ar *tool.ActiveRegistry, _ *tool.Catalog, _ input.SubAgentService) {
	ar.Register(testToolDef("who_am_i"), echoHandler("who_am_i"))
}

func TestSessionRegistryStore_GetOrCreate(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 1,
	})
	store := tool.NewSessionRegistryStore(catalog, fakeBootstrap)

	tests := []struct {
		name      string
		sessionID string
	}{
		{name: "session_a", sessionID: "a"},
		{name: "session_b", sessionID: "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ar1 := store.GetOrCreate(tt.sessionID)
			ar2 := store.GetOrCreate(tt.sessionID)
			if ar1 != ar2 {
				t.Error("GetOrCreate should return the same ActiveRegistry for the same session ID")
			}
			if !ar1.Has() {
				t.Error("new session should have bootstrap tools")
			}
		})
	}
}

func TestSessionRegistryStore_IndependentSessions(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb:       1,
		valueobject.ToolCategoryShellExec: 1,
	})
	store := tool.NewSessionRegistryStore(catalog, fakeBootstrap)

	arA := store.GetOrCreate("a")
	arB := store.GetOrCreate("b")

	if _, err := arA.LoadCategory(valueobject.ToolCategoryWeb); err != nil {
		t.Fatal(err)
	}

	if arA.IsLoaded(valueobject.ToolCategoryWeb) != true {
		t.Error("session A should have web loaded")
	}
	if arB.IsLoaded(valueobject.ToolCategoryWeb) != false {
		t.Error("session B should NOT have web loaded (independent)")
	}
}

func TestSessionRegistryStore_Remove(t *testing.T) {
	t.Parallel()
	catalog := tool.NewCatalog()
	store := tool.NewSessionRegistryStore(catalog, nil)

	_ = store.GetOrCreate("x")
	if !store.Has("x") {
		t.Error("session should exist after GetOrCreate")
	}

	store.Remove("x")
	if store.Has("x") {
		t.Error("session should not exist after Remove")
	}
	if store.Count() != 0 {
		t.Errorf("Count() = %d, want 0", store.Count())
	}
}

func TestSessionRegistryStore_Count(t *testing.T) {
	t.Parallel()
	catalog := tool.NewCatalog()
	store := tool.NewSessionRegistryStore(catalog, nil)

	for i := 0; i < 5; i++ {
		_ = store.GetOrCreate(string(rune('a' + i)))
	}
	if got := store.Count(); got != 5 {
		t.Errorf("Count() = %d, want 5", got)
	}
}

func TestSessionRegistryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 1,
	})
	store := tool.NewSessionRegistryStore(catalog, fakeBootstrap)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			ar := store.GetOrCreate(id)
			_ = ar.Definitions()
			_ = store.Count()
			_ = store.Has(id)
		}(string(rune('a' + (i % 10))))
	}
	wg.Wait()

	if got := store.Count(); got > 10 {
		t.Errorf("Count() = %d, want <= 10", got)
	}
}
