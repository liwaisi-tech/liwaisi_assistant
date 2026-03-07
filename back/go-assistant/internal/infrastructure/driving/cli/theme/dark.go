package theme

import (
	"github.com/charmbracelet/glamour"

	"charm.land/lipgloss/v2"
)

func init() { //nolint:gochecknoinits // theme self-registration
	Register(&darkTheme{})
}

type darkTheme struct{}

func (d *darkTheme) Name() string { return "dark" }

func (d *darkTheme) UserMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0E0E0")).
		Bold(true).
		PaddingLeft(1)
}

func (d *darkTheme) AssistantMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A8D8EA")).
		PaddingLeft(1)
}

func (d *darkTheme) HeaderBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#333333")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1)
}

func (d *darkTheme) StatusBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#333333")).
		Foreground(lipgloss.Color("#AAAAAA")).
		Padding(0, 1)
}

func (d *darkTheme) InputArea() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555555")).
		Padding(0, 1)
}

func (d *darkTheme) Border() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555555"))
}

func (d *darkTheme) SuccessText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#A8E6CF"))
}

func (d *darkTheme) ErrorText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8B94"))
}

func (d *darkTheme) MutedText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
}

func (d *darkTheme) ToolCallText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
}

func (d *darkTheme) CommandText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB"))
}

func (d *darkTheme) GlamourStyle() glamour.TermRendererOption {
	return glamour.WithAutoStyle()
}
