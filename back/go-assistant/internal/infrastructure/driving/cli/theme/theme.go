// Package theme defines the styling contract and registry for the liwaisi CLI.
package theme

import (
	"fmt"

	"github.com/charmbracelet/glamour"

	"charm.land/lipgloss/v2"
)

// Theme defines the styling contract for the CLI.
type Theme interface {
	// Name returns the theme identifier.
	Name() string

	// Message styles.
	UserMessage() lipgloss.Style
	AssistantMessage() lipgloss.Style

	// Layout styles.
	HeaderBar() lipgloss.Style
	StatusBar() lipgloss.Style
	InputArea() lipgloss.Style
	Border() lipgloss.Style

	// Semantic colors.
	SuccessText() lipgloss.Style
	ErrorText() lipgloss.Style
	MutedText() lipgloss.Style
	ToolCallText() lipgloss.Style
	CommandText() lipgloss.Style

	// GlamourStyle returns the Glamour renderer option for markdown.
	GlamourStyle() glamour.TermRendererOption
}

var registry = map[string]Theme{} //nolint:gochecknoglobals // package-level theme registry

// Register adds a theme to the global registry.
func Register(t Theme) {
	registry[t.Name()] = t
}

// Get returns a theme by name or an error if the name is not registered.
func Get(name string) (Theme, error) {
	t, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("theme %q not found", name)
	}
	return t, nil
}

// Names returns all registered theme names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
