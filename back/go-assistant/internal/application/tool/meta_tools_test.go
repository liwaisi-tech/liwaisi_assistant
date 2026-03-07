package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func setupFindTools(t *testing.T) (*tool.ActiveRegistry, *tool.Catalog) {
	t.Helper()
	catalog := catalogWithFakeTools(map[valueobject.ToolCategory]int{
		valueobject.ToolCategoryFileManagement: 3,
		valueobject.ToolCategoryShellExec:      1,
	})
	ar := tool.NewActiveRegistry(catalog)
	tool.RegisterFindToolsOnActive(ar, catalog)
	return ar, catalog
}

func TestFindTools_List(t *testing.T) {
	t.Parallel()
	reg, _ := setupFindTools(t)

	args, _ := json.Marshal(map[string]string{"action": "list"})
	result, err := reg.Execute(context.Background(), "find_tools", args)
	if err != nil {
		t.Fatalf("Execute(find_tools, list) error = %v", err)
	}

	var listResult struct {
		Categories []struct {
			Category string `json:"category"`
			Loaded   bool   `json:"loaded"`
		} `json:"categories"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if listResult.Count != 2 {
		t.Errorf("count = %d, want 2", listResult.Count)
	}
	for _, cat := range listResult.Categories {
		if cat.Loaded {
			t.Errorf("category %q should not be loaded initially", cat.Category)
		}
	}
}

func TestFindTools_Load(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		category string
		wantErr  bool
	}{
		{name: "load_filemanagement", category: "filemanagement"},
		{name: "load_shellexec", category: "shellexec"},
		{name: "unknown_category", category: "bogus", wantErr: true},
		{name: "missing_category", category: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, _ := setupFindTools(t)

			args, _ := json.Marshal(map[string]string{
				"action":   "load",
				"category": tt.category,
			})
			result, err := reg.Execute(context.Background(), "find_tools", args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(find_tools, load %q) error = %v, wantErr %v", tt.category, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			var loadResult struct {
				Category    string                    `json:"category"`
				Status      string                    `json:"status"`
				ToolsLoaded []valueobject.ToolSummary `json:"tools_loaded"`
			}
			if err := json.Unmarshal([]byte(result), &loadResult); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			if loadResult.Status != "loaded" {
				t.Errorf("status = %q, want %q", loadResult.Status, "loaded")
			}
			if loadResult.Category != tt.category {
				t.Errorf("category = %q, want %q", loadResult.Category, tt.category)
			}
		})
	}
}

func TestFindTools_Load_ThenList_ShowsLoaded(t *testing.T) {
	t.Parallel()
	reg, _ := setupFindTools(t)

	loadArgs, _ := json.Marshal(map[string]string{"action": "load", "category": "filemanagement"})
	if _, err := reg.Execute(context.Background(), "find_tools", loadArgs); err != nil {
		t.Fatalf("load: %v", err)
	}

	listArgs, _ := json.Marshal(map[string]string{"action": "list"})
	result, err := reg.Execute(context.Background(), "find_tools", listArgs)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var listResult struct {
		Categories []struct {
			Category string `json:"category"`
			Loaded   bool   `json:"loaded"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatal(err)
	}

	for _, cat := range listResult.Categories {
		wantLoaded := cat.Category == "filemanagement"
		if cat.Loaded != wantLoaded {
			t.Errorf("category %q loaded = %v, want %v", cat.Category, cat.Loaded, wantLoaded)
		}
	}
}

func TestFindTools_UnknownAction(t *testing.T) {
	t.Parallel()
	reg, _ := setupFindTools(t)

	args, _ := json.Marshal(map[string]string{"action": "delete"})
	_, err := reg.Execute(context.Background(), "find_tools", args)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
