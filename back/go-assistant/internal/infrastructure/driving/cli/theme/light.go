package theme

import (
	"github.com/charmbracelet/glamour"

	"charm.land/lipgloss/v2"
)

func init() { //nolint:gochecknoinits // theme self-registration
	Register(&lightTheme{})
}

type lightTheme struct{}

func (l *lightTheme) Name() string { return "light" }

func (l *lightTheme) UserMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#333333")).
		Bold(true).
		PaddingLeft(1)
}

func (l *lightTheme) AssistantMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A5276")).
		PaddingLeft(1)
}

func (l *lightTheme) HeaderBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#E8E8E8")).
		Foreground(lipgloss.Color("#333333")).
		Bold(true).
		Padding(0, 1)
}

func (l *lightTheme) StatusBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#E8E8E8")).
		Foreground(lipgloss.Color("#666666")).
		Padding(0, 1)
}

func (l *lightTheme) InputArea() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#CCCCCC")).
		Padding(0, 1)
}

func (l *lightTheme) Border() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#CCCCCC"))
}

func (l *lightTheme) SuccessText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#27AE60"))
}

func (l *lightTheme) ErrorText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#E74C3C"))
}

func (l *lightTheme) MutedText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#999999"))
}

func (l *lightTheme) ToolCallText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#B8860B"))
}

func (l *lightTheme) CommandText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#7E57C2"))
}

func (l *lightTheme) GlamourStyle() glamour.TermRendererOption {
	return glamour.WithAutoStyle()
}
