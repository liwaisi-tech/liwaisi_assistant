package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCommandRegistry_RegisterAndExecute(t *testing.T) {
	tests := []struct {
		name      string
		register  []CommandEntry
		execName  string
		execArgs  string
		wantFound bool
	}{
		{
			name: "registered command is found",
			register: []CommandEntry{
				{Name: "clear", Description: "Clear conversation", Handler: func(_ string) tea.Cmd { return nil }},
			},
			execName:  "clear",
			wantFound: true,
		},
		{
			name:      "unknown command is not found",
			register:  nil,
			execName:  "nonexistent",
			wantFound: false,
		},
		{
			name: "multiple commands",
			register: []CommandEntry{
				{Name: "clear", Description: "Clear", Handler: func(_ string) tea.Cmd { return nil }},
				{Name: "help", Description: "Help", Handler: func(_ string) tea.Cmd { return nil }},
			},
			execName:  "help",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewCommandRegistry()
			for _, entry := range tt.register {
				reg.Register(entry)
			}

			_, found := reg.Execute(tt.execName, tt.execArgs)
			if found != tt.wantFound {
				t.Errorf("Execute(%q) found = %v, want %v", tt.execName, found, tt.wantFound)
			}
		})
	}
}

func TestCommandRegistry_Entries(t *testing.T) {
	reg := NewCommandRegistry()
	reg.Register(CommandEntry{Name: "rewind", Description: "Rewind", Handler: func(_ string) tea.Cmd { return nil }})
	reg.Register(CommandEntry{Name: "clear", Description: "Clear", Handler: func(_ string) tea.Cmd { return nil }})
	reg.Register(CommandEntry{Name: "help", Description: "Help", Handler: func(_ string) tea.Cmd { return nil }})

	entries := reg.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() returned %d entries, want 3", len(entries))
	}

	want := []string{"clear", "help", "rewind"}
	for i, e := range entries {
		if e.Name != want[i] {
			t.Errorf("entries[%d].Name = %q, want %q", i, e.Name, want[i])
		}
	}
}

func TestCommandRegistry_ExecutePassesArgs(t *testing.T) {
	var capturedArgs string
	reg := NewCommandRegistry()
	reg.Register(CommandEntry{
		Name: "test",
		Handler: func(args string) tea.Cmd {
			capturedArgs = args
			return nil
		},
	})

	reg.Execute("test", "arg1 arg2")
	if capturedArgs != "arg1 arg2" {
		t.Errorf("handler received args = %q, want %q", capturedArgs, "arg1 arg2")
	}
}
