package statusline

import (
	"testing"
)

func TestPalette_ColorFor_Determinism(t *testing.T) {
	t.Parallel()

	p := NewPalette()

	tests := []struct {
		name      string
		agentName string
	}{
		{"code_reviewer", "code_reviewer"},
		{"security_auditor", "security_auditor"},
		{"data_analyst", "data_analyst"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first := p.ColorFor(tt.agentName).Render("test")
			second := p.ColorFor(tt.agentName).Render("test")

			if first != second {
				t.Errorf("ColorFor(%q) not deterministic: %q != %q", tt.agentName, first, second)
			}
		})
	}
}

func TestPalette_ColorFor_Distribution(t *testing.T) {
	t.Parallel()

	p := NewPalette()
	names := []string{
		"agent_a", "agent_b", "agent_c", "agent_d", "agent_e",
		"reviewer", "writer", "planner", "coder", "tester",
	}

	rendered := make(map[string]struct{})
	for _, name := range names {
		style := p.ColorFor(name)
		output := style.Render("X")
		rendered[output] = struct{}{}
	}

	// With 10 names and 8 colors, we expect at least 3 distinct outputs.
	if len(rendered) < 3 {
		t.Errorf("expected at least 3 distinct colors from 10 names, got %d", len(rendered))
	}
}

func TestPalette_ColorFor_EmptyName(t *testing.T) {
	t.Parallel()

	p := NewPalette()
	style := p.ColorFor("")
	output := style.Render("test")

	if output == "" {
		t.Error("ColorFor(\"\") should still render text")
	}
}

func TestPalette_ColorFor_EmptyPalette(t *testing.T) {
	t.Parallel()

	p := Palette{hexColors: nil}
	style := p.ColorFor("agent")
	output := style.Render("test")

	if output == "" {
		t.Error("ColorFor on empty palette should still render text")
	}
}
