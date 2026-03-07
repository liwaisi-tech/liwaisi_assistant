package valueobject_test

import (
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestToolSummary_JSON(t *testing.T) {
	t.Parallel()
	s := valueobject.ToolSummary{
		Name:        "read_file",
		Description: "Read the contents of a single file",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got valueobject.ToolSummary
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Name != s.Name || got.Description != s.Description {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, s)
	}
}

func TestToolCategoryInfo_JSON(t *testing.T) {
	t.Parallel()
	info := valueobject.ToolCategoryInfo{
		Category:    valueobject.ToolCategoryFileManagement,
		Description: "File system operations",
		Tools: []valueobject.ToolSummary{
			{Name: "read_file", Description: "Read a file"},
			{Name: "write_file", Description: "Write a file"},
		},
		EstTokenCost: 3000,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got valueobject.ToolCategoryInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.Category != info.Category {
		t.Errorf("Category = %q, want %q", got.Category, info.Category)
	}
	if got.Description != info.Description {
		t.Errorf("Description = %q, want %q", got.Description, info.Description)
	}
	if len(got.Tools) != len(info.Tools) {
		t.Fatalf("Tools count = %d, want %d", len(got.Tools), len(info.Tools))
	}
	if got.EstTokenCost != info.EstTokenCost {
		t.Errorf("EstTokenCost = %d, want %d", got.EstTokenCost, info.EstTokenCost)
	}
}
