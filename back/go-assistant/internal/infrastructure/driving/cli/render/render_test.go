package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
)

func TestNewRenderer(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		wantErr bool
	}{
		{"standard width", 80, false},
		{"narrow width", 40, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRenderer(glamour.WithAutoStyle(), tt.width)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewRenderer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if r.Width() != tt.width {
				t.Errorf("Width() = %d, want %d", r.Width(), tt.width)
			}
		})
	}
}

func TestRenderer_Render(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"plain text", "Hello world", "Hello world"},
		{"bold text", "**bold**", "bold"},
		{"code block", "```go\nfmt.Println(\"hi\")\n```", "Println"},
		{"heading", "# Title", "Title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := r.Render(tt.input)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(out, tt.contains) {
				t.Errorf("Render() output missing %q:\n%s", tt.contains, out)
			}
		})
	}
}

func TestRenderer_SetWidth(t *testing.T) {
	tests := []struct {
		name         string
		initialWidth int
		newWidth     int
		wantChanged  bool
	}{
		{"shrink from 80 to 40", 80, 40, true},
		{"grow from 40 to 120", 40, 120, true},
		{"same width is no-op", 80, 80, false},
		{"narrow terminal", 80, 20, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRenderer(glamour.WithAutoStyle(), tt.initialWidth)
			if err != nil {
				t.Fatalf("NewRenderer: %v", err)
			}
			if r.Width() != tt.initialWidth {
				t.Fatalf("initial Width() = %d, want %d", r.Width(), tt.initialWidth)
			}

			err = r.SetWidth(tt.newWidth)
			if err != nil {
				t.Fatalf("SetWidth(%d) error = %v", tt.newWidth, err)
			}
			if r.Width() != tt.newWidth {
				t.Errorf("Width() after SetWidth = %d, want %d", r.Width(), tt.newWidth)
			}
		})
	}
}

func TestRenderer_SetWidth_RenderRespects(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	wide, err := r.Render("Hello world, this is a test")
	if err != nil {
		t.Fatalf("Render at 80: %v", err)
	}

	if err := r.SetWidth(20); err != nil {
		t.Fatalf("SetWidth(20): %v", err)
	}

	narrow, err := r.Render("Hello world, this is a test")
	if err != nil {
		t.Fatalf("Render at 20: %v", err)
	}

	if wide == narrow {
		t.Error("expected different rendered output at different widths")
	}
}

func TestStreamRenderer_WriteToken(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	cmd := sr.WriteToken("Hello ")
	if cmd != nil {
		t.Error("expected nil cmd for mid-paragraph token")
	}

	cmd = sr.WriteToken("world\n")
	if cmd == nil {
		t.Error("expected non-nil cmd after newline boundary")
	}

	if got := sr.Content(); got != "Hello world\n" {
		t.Errorf("Content() = %q, want %q", got, "Hello world\n")
	}
}

func TestStreamRenderer_ParagraphBoundary(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	sr.WriteToken("First paragraph.")
	cmd := sr.WriteToken("\n\n")
	if cmd == nil {
		t.Error("expected render at paragraph boundary (double newline)")
	}
}

func TestStreamRenderer_Flush(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	sr.WriteToken("leftover content")
	cmd := sr.Flush()
	if cmd == nil {
		t.Error("Flush() should return a cmd for non-empty buffer")
	}
}

func TestStreamRenderer_FlushEmpty(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	cmd := sr.Flush()
	if cmd != nil {
		t.Error("Flush() on empty buffer should return nil")
	}
}

func TestStreamRenderer_Reset(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	sr.WriteToken("some content")
	sr.Reset()

	if got := sr.Content(); got != "" {
		t.Errorf("Content() after Reset() = %q, want empty", got)
	}
}

func TestStreamRenderer_CodeBlockBoundary(t *testing.T) {
	r, err := NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	sr := NewStreamRenderer(r)

	sr.WriteToken("```go\nfmt.Println()")
	cmd := sr.WriteToken("\n```")
	if cmd == nil {
		t.Error("expected render at code block closing boundary")
	}
}
