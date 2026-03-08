package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestScopedRegistry_Filtering(t *testing.T) {
	tests := []struct {
		name        string
		registered  []string
		allowed     []string
		denied      []string
		wantVisible []string
	}{
		{
			name:        "no filters inherits all",
			registered:  []string{"read_file", "write_file", "shell_exec"},
			allowed:     nil,
			denied:      nil,
			wantVisible: []string{"read_file", "shell_exec", "write_file"},
		},
		{
			name:        "allow only",
			registered:  []string{"read_file", "write_file", "shell_exec"},
			allowed:     []string{"read_file"},
			denied:      nil,
			wantVisible: []string{"read_file"},
		},
		{
			name:        "deny only",
			registered:  []string{"read_file", "write_file", "shell_exec"},
			allowed:     nil,
			denied:      []string{"shell_exec"},
			wantVisible: []string{"read_file", "write_file"},
		},
		{
			name:        "deny overrides allow",
			registered:  []string{"read_file", "write_file", "shell_exec"},
			allowed:     []string{"read_file", "shell_exec"},
			denied:      []string{"shell_exec"},
			wantVisible: []string{"read_file"},
		},
		{
			name:        "all tools denied",
			registered:  []string{"read_file", "write_file"},
			allowed:     nil,
			denied:      []string{"read_file", "write_file"},
			wantVisible: nil,
		},
		{
			name:        "allow tool not in registry",
			registered:  []string{"read_file"},
			allowed:     []string{"nonexistent"},
			denied:      nil,
			wantVisible: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, name := range tt.registered {
				registry.Register(testDefinition(name), echoHandler)
			}
			scoped := NewScopedRegistry(registry, tt.allowed, tt.denied)

			defs := scoped.Definitions()
			got := make([]string, len(defs))
			for i, d := range defs {
				got[i] = d.Function.Name
			}
			sort.Strings(got)

			want := tt.wantVisible
			if want == nil {
				want = []string{}
			}
			sort.Strings(want)

			if len(got) != len(want) {
				t.Fatalf("Definitions() returned %v, want %v", got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("Definitions() returned %v, want %v", got, want)
				}
			}
		})
	}
}

func TestScopedRegistry_Execute(t *testing.T) {
	tests := []struct {
		name       string
		registered []string
		allowed    []string
		denied     []string
		execTool   string
		execArgs   string
		want       string
		wantErr    bool
		errContain string
	}{
		{
			name:       "allowed tool delegates to registry",
			registered: []string{"read_file", "write_file"},
			allowed:    []string{"read_file"},
			denied:     nil,
			execTool:   "read_file",
			execArgs:   `{"path":"/tmp/test"}`,
			want:       `{"path":"/tmp/test"}`,
		},
		{
			name:       "denied tool returns error",
			registered: []string{"read_file", "shell_exec"},
			allowed:    nil,
			denied:     []string{"shell_exec"},
			execTool:   "shell_exec",
			wantErr:    true,
			errContain: "shell_exec",
		},
		{
			name:       "tool not in allowlist returns error",
			registered: []string{"read_file", "write_file"},
			allowed:    []string{"read_file"},
			denied:     nil,
			execTool:   "write_file",
			wantErr:    true,
			errContain: "write_file",
		},
		{
			name:       "no filters allows execution",
			registered: []string{"read_file"},
			allowed:    nil,
			denied:     nil,
			execTool:   "read_file",
			execArgs:   `{}`,
			want:       `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, name := range tt.registered {
				registry.Register(testDefinition(name), echoHandler)
			}
			scoped := NewScopedRegistry(registry, tt.allowed, tt.denied)

			got, err := scoped.Execute(context.Background(), tt.execTool, json.RawMessage(tt.execArgs))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("Execute() error = %q, want it to contain %q", err.Error(), tt.errContain)
				}
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScopedRegistry_Has(t *testing.T) {
	tests := []struct {
		name       string
		registered []string
		allowed    []string
		denied     []string
		wantHas    bool
	}{
		{
			name:       "has tools after filtering",
			registered: []string{"read_file", "write_file"},
			allowed:    []string{"read_file"},
			denied:     nil,
			wantHas:    true,
		},
		{
			name:       "all tools filtered out",
			registered: []string{"read_file"},
			allowed:    nil,
			denied:     []string{"read_file"},
			wantHas:    false,
		},
		{
			name:       "empty registry",
			registered: nil,
			allowed:    nil,
			denied:     nil,
			wantHas:    false,
		},
		{
			name:       "no filters with tools",
			registered: []string{"read_file"},
			allowed:    nil,
			denied:     nil,
			wantHas:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, name := range tt.registered {
				registry.Register(testDefinition(name), echoHandler)
			}
			scoped := NewScopedRegistry(registry, tt.allowed, tt.denied)
			if got := scoped.Has(); got != tt.wantHas {
				t.Errorf("Has() = %v, want %v", got, tt.wantHas)
			}
		})
	}
}

func TestScopedRegistry_JITCompat(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testDefinition("read_file"), echoHandler)

	scoped := NewScopedRegistry(registry, []string{"read_file", "grep"}, nil)

	if defs := scoped.Definitions(); len(defs) != 1 {
		t.Fatalf("before JIT: Definitions() = %d, want 1", len(defs))
	}

	registry.Register(testDefinition("grep"), echoHandler)

	defs := scoped.Definitions()
	if len(defs) != 2 {
		t.Fatalf("after JIT: Definitions() = %d, want 2", len(defs))
	}

	got := make([]string, len(defs))
	for i, d := range defs {
		got[i] = d.Function.Name
	}
	sort.Strings(got)
	if got[0] != "grep" || got[1] != "read_file" {
		t.Errorf("after JIT: Definitions() names = %v, want [grep read_file]", got)
	}

	result, err := scoped.Execute(context.Background(), "grep", json.RawMessage(`{"pattern":"foo"}`))
	if err != nil {
		t.Fatalf("Execute(grep) after JIT: unexpected error: %v", err)
	}
	if result != `{"pattern":"foo"}` {
		t.Errorf("Execute(grep) = %q, want %q", result, `{"pattern":"foo"}`)
	}
}

func TestScopedRegistry_Concurrent(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testDefinition("read_file"), echoHandler)
	registry.Register(testDefinition("write_file"), echoHandler)
	registry.Register(testDefinition("shell_exec"), echoHandler)

	scoped := NewScopedRegistry(registry, []string{"read_file", "write_file"}, []string{"write_file"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = scoped.Definitions()
		}()
		go func() {
			defer wg.Done()
			_, _ = scoped.Execute(context.Background(), "read_file", json.RawMessage(`{}`))
		}()
		go func() {
			defer wg.Done()
			_ = scoped.Has()
		}()
	}
	wg.Wait()
}

func TestScopedRegistry_ConcurrentWithRegistration(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testDefinition("base"), echoHandler)

	scoped := NewScopedRegistry(registry, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		idx := i
		wg.Add(3)
		go func() {
			defer wg.Done()
			registry.Register(testDefinition(fmt.Sprintf("tool_%d", idx)), echoHandler)
		}()
		go func() {
			defer wg.Done()
			_ = scoped.Definitions()
		}()
		go func() {
			defer wg.Done()
			_, _ = scoped.Execute(context.Background(), "base", json.RawMessage(`{}`))
		}()
	}
	wg.Wait()
}

func TestScopedRegistry_NilExecutor(t *testing.T) {
	t.Parallel()

	// A ScopedRegistry with a nil executor should not panic.
	s := NewScopedRegistry(nil, nil, nil)

	if s.Has() {
		t.Error("Has() should return false for nil executor")
	}

	defs := s.Definitions()
	if len(defs) != 0 {
		t.Errorf("Definitions() returned %d items, want 0", len(defs))
	}

	_, err := s.Execute(context.Background(), "any", nil)
	if err == nil {
		t.Error("Execute() should return an error for nil executor")
	}
}
