package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func echoHandler(_ context.Context, args json.RawMessage) (string, error) {
	return string(args), nil
}

func testDefinition(name string) valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        name,
			Description: "test tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}
}

func TestRegistry_Has(t *testing.T) {
	tests := []struct {
		name     string
		register bool
		wantHas  bool
	}{
		{"empty registry", false, false},
		{"registry with tool", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			if tt.register {
				r.Register(testDefinition("test"), echoHandler)
			}
			if got := r.Has(); got != tt.wantHas {
				t.Errorf("Has() = %v, want %v", got, tt.wantHas)
			}
		})
	}
}

func TestRegistry_Definitions(t *testing.T) {
	tests := []struct {
		name    string
		tools   []string
		wantLen int
	}{
		{"no tools", nil, 0},
		{"one tool", []string{"a"}, 1},
		{"multiple tools", []string{"a", "b", "c"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			for _, name := range tt.tools {
				r.Register(testDefinition(name), echoHandler)
			}
			defs := r.Definitions()
			if len(defs) != tt.wantLen {
				t.Errorf("Definitions() returned %d, want %d", len(defs), tt.wantLen)
			}
		})
	}
}

func TestRegistry_Execute(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    string
		want    string
		wantErr bool
	}{
		{
			name: "known tool returns args",
			tool: "echo",
			args: `{"key":"value"}`,
			want: `{"key":"value"}`,
		},
		{
			name:    "unknown tool returns error",
			tool:    "nonexistent",
			args:    "{}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			r.Register(testDefinition("echo"), echoHandler)

			got, err := r.Execute(context.Background(), tt.tool, json.RawMessage(tt.args))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	r := NewRegistry()
	r.Register(testDefinition("dup"), func(_ context.Context, _ json.RawMessage) (string, error) {
		return "first", nil
	})
	r.Register(testDefinition("dup"), func(_ context.Context, _ json.RawMessage) (string, error) {
		return "second", nil
	})

	got, err := r.Execute(context.Background(), "dup", nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "second" {
		t.Errorf("Execute() = %q, want %q (last registration wins)", got, "second")
	}

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Errorf("Definitions() = %d, want 1 (no duplicates)", len(defs))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	r.Register(testDefinition("base"), echoHandler)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(4)
		idx := i
		go func() {
			defer wg.Done()
			r.Register(testDefinition(fmt.Sprintf("tool_%d", idx)), echoHandler)
		}()
		go func() {
			defer wg.Done()
			_, _ = r.Execute(context.Background(), "base", json.RawMessage(`{}`))
		}()
		go func() {
			defer wg.Done()
			_ = r.Definitions()
		}()
		go func() {
			defer wg.Done()
			_ = r.Has()
		}()
	}
	wg.Wait()
}
