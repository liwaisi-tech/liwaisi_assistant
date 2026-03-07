// Package render provides terminal output rendering for the liwaisi CLI.
package render

import (
	"fmt"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

// Renderer wraps Glamour for styled markdown output.
type Renderer struct {
	glamour *glamour.TermRenderer
	width   int
	style   glamour.TermRendererOption
}

// NewRenderer creates a renderer with the given Glamour style option and terminal width.
// The style is resolved once to a concrete dark/light style so that SetWidth can
// safely recreate the Glamour renderer without sending terminal detection queries
// (which would corrupt Bubble Tea's alt screen).
func NewRenderer(style glamour.TermRendererOption, width int) (*Renderer, error) {
	r, err := glamour.NewTermRenderer(
		style,
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("creating glamour renderer: %w", err)
	}

	resolved := glamour.WithStandardStyle(styles.LightStyle)
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		resolved = glamour.WithStandardStyle(styles.DarkStyle)
	}

	return &Renderer{glamour: r, width: width, style: resolved}, nil
}

// SetWidth recreates the internal Glamour renderer with a new word-wrap width.
// Glamour does not support changing wrap width after creation, so the renderer
// must be rebuilt.
func (r *Renderer) SetWidth(width int) error {
	if width == r.width {
		return nil
	}
	g, err := glamour.NewTermRenderer(r.style, glamour.WithWordWrap(width))
	if err != nil {
		return fmt.Errorf("recreating glamour renderer: %w", err)
	}
	r.glamour = g
	r.width = width
	return nil
}

// Render converts markdown text to styled terminal output.
func (r *Renderer) Render(markdown string) (string, error) {
	out, err := r.glamour.Render(markdown)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	return out, nil
}

// Width returns the configured terminal width.
func (r *Renderer) Width() int {
	return r.width
}
