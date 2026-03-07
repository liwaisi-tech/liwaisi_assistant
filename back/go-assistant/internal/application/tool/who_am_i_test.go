package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegisterWhoAmI(t *testing.T) {
	info := &AgentInfo{
		Name:         "liwaisi",
		Version:      "0.1.0",
		Description:  "Test agent",
		Model:        "test-model",
		Capabilities: []string{"chat", "tools"},
		CreatedBy:    "test",
	}

	r := NewRegistry()
	RegisterWhoAmI(r, info)

	if !r.Has() {
		t.Fatal("registry should have tools after RegisterWhoAmI")
	}

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Function.Name != "who_am_i" {
		t.Errorf("tool name = %q, want %q", defs[0].Function.Name, "who_am_i")
	}
}

func TestWhoAmI_Execute(t *testing.T) {
	tests := []struct {
		name string
		args json.RawMessage
	}{
		{"nil args", nil},
		{"empty object args", json.RawMessage(`{}`)},
		{"empty raw message", json.RawMessage(``)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &AgentInfo{
				Name:         "liwaisi",
				Version:      "1.0.0",
				Description:  "AI assistant",
				Model:        "test-model",
				Capabilities: []string{"chat"},
				CreatedBy:    "test",
			}

			r := NewRegistry()
			RegisterWhoAmI(r, info)

			result, err := r.Execute(context.Background(), "who_am_i", tt.args)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			var got AgentInfo
			if err := json.Unmarshal([]byte(result), &got); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if got.Name != info.Name {
				t.Errorf("Name = %q, want %q", got.Name, info.Name)
			}
			if got.Version != info.Version {
				t.Errorf("Version = %q, want %q", got.Version, info.Version)
			}
			if got.Model != info.Model {
				t.Errorf("Model = %q, want %q", got.Model, info.Model)
			}
			if len(got.Capabilities) != len(info.Capabilities) {
				t.Errorf("Capabilities length = %d, want %d", len(got.Capabilities), len(info.Capabilities))
			}
		})
	}
}
