package tool_test

import (
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newTestEntry(cat valueobject.ToolCategory, desc string, toolCount int) *tool.CatalogEntry {
	tools := make([]valueobject.ToolSummary, toolCount)
	for i := range tools {
		tools[i] = valueobject.ToolSummary{Name: cat.String() + "_tool", Description: "test"}
	}
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:     cat,
			Description:  desc,
			Tools:        tools,
			EstTokenCost: toolCount * 250,
		},
		Factory: func(_ *tool.Registry) {},
	}
}

func TestCatalog_NewCatalog(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	if got := len(c.List()); got != 0 {
		t.Errorf("NewCatalog().List() len = %d, want 0", got)
	}
	if got := len(c.Categories()); got != 0 {
		t.Errorf("NewCatalog().Categories() len = %d, want 0", got)
	}
}

func TestCatalog_RegisterCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		categories []valueobject.ToolCategory
		wantLen    int
	}{
		{
			name:       "single_category",
			categories: []valueobject.ToolCategory{valueobject.ToolCategoryWeb},
			wantLen:    1,
		},
		{
			name: "multiple_categories",
			categories: []valueobject.ToolCategory{
				valueobject.ToolCategoryFileManagement,
				valueobject.ToolCategoryShellExec,
				valueobject.ToolCategoryWeb,
				valueobject.ToolCategoryEnv,
			},
			wantLen: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tool.NewCatalog()
			for _, cat := range tt.categories {
				c.RegisterCategory(newTestEntry(cat, "test", 1))
			}
			if got := len(c.List()); got != tt.wantLen {
				t.Errorf("List() len = %d, want %d", got, tt.wantLen)
			}
		})
	}
}

func TestCatalog_List_Sorted(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryWeb, "web", 1))
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryEnv, "env", 2))
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryFileManagement, "files", 11))

	list := c.List()
	if len(list) != 3 {
		t.Fatalf("List() len = %d, want 3", len(list))
	}
	expected := []valueobject.ToolCategory{
		valueobject.ToolCategoryEnv,
		valueobject.ToolCategoryFileManagement,
		valueobject.ToolCategoryWeb,
	}
	for i, want := range expected {
		if list[i].Category != want {
			t.Errorf("List()[%d].Category = %q, want %q", i, list[i].Category, want)
		}
	}
}

func TestCatalog_Get(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryWeb, "web tools", 1))

	tests := []struct {
		name     string
		category valueobject.ToolCategory
		wantOK   bool
	}{
		{name: "exists", category: valueobject.ToolCategoryWeb, wantOK: true},
		{name: "not_exists", category: valueobject.ToolCategoryEnv, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry, ok := c.Get(tt.category)
			if ok != tt.wantOK {
				t.Errorf("Get(%q) ok = %v, want %v", tt.category, ok, tt.wantOK)
			}
			if tt.wantOK && entry == nil {
				t.Error("Get() returned nil entry for existing category")
			}
			if !tt.wantOK && entry != nil {
				t.Error("Get() returned non-nil entry for missing category")
			}
		})
	}
}

func TestCatalog_Categories_Sorted(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryShellExec, "shell", 1))
	c.RegisterCategory(newTestEntry(valueobject.ToolCategoryEnv, "env", 2))

	cats := c.Categories()
	if len(cats) != 2 {
		t.Fatalf("Categories() len = %d, want 2", len(cats))
	}
	if cats[0] != valueobject.ToolCategoryEnv || cats[1] != valueobject.ToolCategoryShellExec {
		t.Errorf("Categories() = %v, want [env shellexec]", cats)
	}
}

func TestCatalog_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	c := tool.NewCatalog()
	categories := []valueobject.ToolCategory{
		valueobject.ToolCategoryFileManagement,
		valueobject.ToolCategoryShellExec,
		valueobject.ToolCategoryWeb,
		valueobject.ToolCategoryEnv,
	}

	var wg sync.WaitGroup
	for _, cat := range categories {
		wg.Add(1)
		go func(cat valueobject.ToolCategory) {
			defer wg.Done()
			c.RegisterCategory(newTestEntry(cat, "concurrent", 1))
		}(cat)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.List()
			_ = c.Categories()
			_, _ = c.Get(valueobject.ToolCategoryWeb)
		}()
	}
	wg.Wait()

	if got := len(c.Categories()); got != 4 {
		t.Errorf("after concurrent access, Categories() len = %d, want 4", got)
	}
}
