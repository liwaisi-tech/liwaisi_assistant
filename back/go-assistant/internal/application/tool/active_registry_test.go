package tool_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// testToolDef returns a minimal ToolDefinition for testing.
func testToolDef(name string) valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        name,
			Description: "test tool " + name,
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
}

// echoHandler returns a handler that echoes the tool name.
func echoHandler(name string) tool.Handler {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "executed:" + name, nil
	}
}

// catalogWithFakeTools builds a Catalog where each category registers
// the specified number of fake tools.
func catalogWithFakeTools(cats map[valueobject.ToolCategory]int) *tool.Catalog {
	c := tool.NewCatalog()
	for cat, count := range cats {
		count := count
		cat := cat
		summaries := make([]valueobject.ToolSummary, count)
		for i := range summaries {
			summaries[i] = valueobject.ToolSummary{
				Name:        cat.String() + "_t" + string(rune('0'+i)),
				Description: "test",
			}
		}
		c.RegisterCategory(&tool.CatalogEntry{
			Info: valueobject.ToolCategoryInfo{
				Category:     cat,
				Description:  "test " + cat.String(),
				Tools:        summaries,
				EstTokenCost: count * 250,
			},
			Factory: func(r *tool.Registry) {
				for _, s := range summaries {
					r.Register(testToolDef(s.Name), echoHandler(s.Name))
				}
			},
		})
	}
	return c
}

func TestActiveRegistry_NewActiveRegistry(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	ar := tool.NewActiveRegistry(c)

	if ar.Has() {
		t.Error("NewActiveRegistry should start with no tools")
	}
	if got := len(ar.Definitions()); got != 0 {
		t.Errorf("Definitions() len = %d, want 0", got)
	}
	if got := len(ar.LoadedCategories()); got != 0 {
		t.Errorf("LoadedCategories() len = %d, want 0", got)
	}
}

func TestActiveRegistry_LoadCategory(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 2,
		valueobject.ToolCategoryEnv: 3,
	})

	tests := []struct {
		name          string
		category      valueobject.ToolCategory
		wantErr       bool
		wantToolCount int
	}{
		{name: "load_web", category: valueobject.ToolCategoryWeb, wantToolCount: 2},
		{name: "load_env", category: valueobject.ToolCategoryEnv, wantToolCount: 3},
		{name: "unknown_category", category: valueobject.ToolCategory("bogus"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ar := tool.NewActiveRegistry(catalog)
			info, err := ar.LoadCategory(tt.category)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadCategory(%q) error = %v, wantErr %v", tt.category, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(info.Tools) != tt.wantToolCount {
				t.Errorf("info.Tools len = %d, want %d", len(info.Tools), tt.wantToolCount)
			}
			if got := len(ar.Definitions()); got != tt.wantToolCount {
				t.Errorf("Definitions() len = %d, want %d", got, tt.wantToolCount)
			}
		})
	}
}

func TestActiveRegistry_LoadCategory_Idempotent(t *testing.T) {
	t.Parallel()
	callCount := 0
	c := tool.NewCatalog()
	c.RegisterCategory(&tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:     valueobject.ToolCategoryWeb,
			Description:  "web",
			Tools:        []valueobject.ToolSummary{{Name: "web_fetch", Description: "fetch"}},
			EstTokenCost: 250,
		},
		Factory: func(r *tool.Registry) {
			callCount++
			r.Register(testToolDef("web_fetch"), echoHandler("web_fetch"))
		},
	})

	ar := tool.NewActiveRegistry(c)

	for i := 0; i < 3; i++ {
		if _, err := ar.LoadCategory(valueobject.ToolCategoryWeb); err != nil {
			t.Fatalf("LoadCategory() iteration %d: %v", i, err)
		}
	}
	if callCount != 1 {
		t.Errorf("Factory called %d times, want 1 (idempotent)", callCount)
	}
	if got := len(ar.Definitions()); got != 1 {
		t.Errorf("Definitions() len = %d, want 1", got)
	}
}

func TestActiveRegistry_IsLoaded(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 1,
	})
	ar := tool.NewActiveRegistry(catalog)

	if ar.IsLoaded(valueobject.ToolCategoryWeb) {
		t.Error("IsLoaded() should be false before loading")
	}
	if _, err := ar.LoadCategory(valueobject.ToolCategoryWeb); err != nil {
		t.Fatal(err)
	}
	if !ar.IsLoaded(valueobject.ToolCategoryWeb) {
		t.Error("IsLoaded() should be true after loading")
	}
}

func TestActiveRegistry_LoadedCategories(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb:       1,
		valueobject.ToolCategoryShellExec: 1,
		valueobject.ToolCategoryEnv:       1,
	})
	ar := tool.NewActiveRegistry(catalog)

	if _, err := ar.LoadCategory(valueobject.ToolCategoryShellExec); err != nil {
		t.Fatal(err)
	}
	if _, err := ar.LoadCategory(valueobject.ToolCategoryEnv); err != nil {
		t.Fatal(err)
	}

	cats := ar.LoadedCategories()
	if len(cats) != 2 {
		t.Fatalf("LoadedCategories() len = %d, want 2", len(cats))
	}
	if cats[0] != valueobject.ToolCategoryEnv || cats[1] != valueobject.ToolCategoryShellExec {
		t.Errorf("LoadedCategories() = %v, want [env shellexec]", cats)
	}
}

func TestActiveRegistry_LoadAll(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryFileManagement: 3,
		valueobject.ToolCategoryShellExec:      1,
		valueobject.ToolCategoryWeb:            1,
		valueobject.ToolCategoryEnv:            2,
	})
	ar := tool.NewActiveRegistry(catalog)

	if err := ar.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if got := len(ar.Definitions()); got != 7 {
		t.Errorf("Definitions() len = %d, want 7", got)
	}
	if got := len(ar.LoadedCategories()); got != 4 {
		t.Errorf("LoadedCategories() len = %d, want 4", got)
	}
}

func TestActiveRegistry_Execute(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 1,
	})
	ar := tool.NewActiveRegistry(catalog)

	// Execute before loading should fail.
	_, err := ar.Execute(context.Background(), "web_t0", nil)
	if err == nil {
		t.Error("Execute before LoadCategory should return error")
	}

	if _, err := ar.LoadCategory(valueobject.ToolCategoryWeb); err != nil {
		t.Fatal(err)
	}

	result, err := ar.Execute(context.Background(), "web_t0", nil)
	if err != nil {
		t.Fatalf("Execute after load: %v", err)
	}
	if result != "executed:web_t0" {
		t.Errorf("Execute result = %q, want %q", result, "executed:web_t0")
	}
}

func TestActiveRegistry_BootstrapTools(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryWeb: 1,
	})
	ar := tool.NewActiveRegistry(catalog)

	ar.Register(testToolDef("who_am_i"), echoHandler("who_am_i"))
	if !ar.Has() {
		t.Error("Has() should be true after Register")
	}
	if got := len(ar.Definitions()); got != 1 {
		t.Errorf("Definitions() len = %d, want 1", got)
	}

	// Load category adds on top of bootstrap.
	if _, err := ar.LoadCategory(valueobject.ToolCategoryWeb); err != nil {
		t.Fatal(err)
	}
	if got := len(ar.Definitions()); got != 2 {
		t.Errorf("Definitions() len = %d, want 2 (bootstrap + loaded)", got)
	}
}

func TestActiveRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryFileManagement: 2,
		valueobject.ToolCategoryShellExec:      1,
		valueobject.ToolCategoryWeb:            1,
		valueobject.ToolCategoryEnv:            1,
	})
	ar := tool.NewActiveRegistry(catalog)

	var wg sync.WaitGroup
	categories := []valueobject.ToolCategory{
		valueobject.ToolCategoryFileManagement,
		valueobject.ToolCategoryShellExec,
		valueobject.ToolCategoryWeb,
		valueobject.ToolCategoryEnv,
	}

	for _, cat := range categories {
		wg.Add(1)
		go func(c valueobject.ToolCategory) {
			defer wg.Done()
			_, _ = ar.LoadCategory(c)
		}(cat)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ar.Definitions()
			_ = ar.Has()
			_ = ar.IsLoaded(valueobject.ToolCategoryWeb)
			_ = ar.LoadedCategories()
		}()
	}
	wg.Wait()

	if got := len(ar.LoadedCategories()); got != 4 {
		t.Errorf("after concurrent access, LoadedCategories() len = %d, want 4", got)
	}
}
