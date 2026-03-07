package statusline

import (
	"hash/fnv"

	"charm.land/lipgloss/v2"
)

// Palette maps sub-agent names to deterministic colors for visual
// identification. The same name always produces the same color.
type Palette struct {
	hexColors []string
}

// NewPalette returns a Palette with 8 visually distinct colors
// chosen for WCAG contrast on dark terminal backgrounds.
func NewPalette() Palette {
	return Palette{
		hexColors: []string{
			"#FF6B6B", // coral red
			"#4ECDC4", // teal
			"#FFD93D", // amber
			"#C084FC", // violet
			"#38BDF8", // sky blue
			"#6EE7B7", // green
			"#F9A8D4", // pink
			"#FB923C", // orange
		},
	}
}

// ColorFor returns a lipgloss.Style with the foreground color
// deterministically assigned to name. The mapping is stable: the same
// name always returns the same color across calls and process restarts.
func (p Palette) ColorFor(name string) lipgloss.Style {
	if name == "" || len(p.hexColors) == 0 {
		return lipgloss.NewStyle()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := h.Sum32() % uint32(len(p.hexColors)) //nolint:gosec // palette length is a small constant
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.hexColors[idx])).Bold(true)
}
