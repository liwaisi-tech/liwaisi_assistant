package render

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// StreamRenderedMsg carries the latest rendered output.
type StreamRenderedMsg struct {
	Rendered string
}

// StreamRenderErrorMsg is sent when markdown rendering fails during streaming.
type StreamRenderErrorMsg struct {
	Err error
}

// StreamRenderer accumulates tokens and renders markdown at paragraph boundaries.
type StreamRenderer struct {
	buffer   strings.Builder
	renderer *Renderer
}

// NewStreamRenderer creates a StreamRenderer backed by the given Renderer.
func NewStreamRenderer(r *Renderer) *StreamRenderer {
	return &StreamRenderer{renderer: r}
}

// WriteToken appends a token to the buffer and triggers a re-render if
// a paragraph boundary (double newline) is detected or the token stream
// ends with a newline.
func (s *StreamRenderer) WriteToken(token string) tea.Cmd {
	s.buffer.WriteString(token)

	content := s.buffer.String()
	if !shouldRender(content, token) {
		return nil
	}

	rendered, err := s.renderer.Render(content)
	if err != nil {
		return func() tea.Msg {
			return StreamRenderErrorMsg{Err: fmt.Errorf("stream render: %w", err)}
		}
	}

	return func() tea.Msg {
		return StreamRenderedMsg{Rendered: rendered}
	}
}

// Flush forces a final render of whatever remains in the buffer.
func (s *StreamRenderer) Flush() tea.Cmd {
	content := s.buffer.String()
	if content == "" {
		return nil
	}

	rendered, err := s.renderer.Render(content)
	if err != nil {
		return func() tea.Msg {
			return StreamRenderErrorMsg{Err: fmt.Errorf("flush render: %w", err)}
		}
	}

	return func() tea.Msg {
		return StreamRenderedMsg{Rendered: rendered}
	}
}

// Reset clears the buffer for a new streaming session.
func (s *StreamRenderer) Reset() {
	s.buffer.Reset()
}

// Content returns the raw accumulated content.
func (s *StreamRenderer) Content() string {
	return s.buffer.String()
}

func shouldRender(content, lastToken string) bool {
	if strings.Contains(lastToken, "\n\n") {
		return true
	}
	if strings.HasSuffix(content, "\n") {
		return true
	}
	if strings.HasSuffix(lastToken, "```") {
		return true
	}
	return false
}
