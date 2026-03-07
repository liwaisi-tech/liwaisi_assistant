package valueobject_test

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestToolCategory_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		category valueobject.ToolCategory
		want     string
	}{
		{name: "filemanagement", category: valueobject.ToolCategoryFileManagement, want: "filemanagement"},
		{name: "shellexec", category: valueobject.ToolCategoryShellExec, want: "shellexec"},
		{name: "web", category: valueobject.ToolCategoryWeb, want: "web"},
		{name: "env", category: valueobject.ToolCategoryEnv, want: "env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.category.String(); got != tt.want {
				t.Errorf("ToolCategory.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
