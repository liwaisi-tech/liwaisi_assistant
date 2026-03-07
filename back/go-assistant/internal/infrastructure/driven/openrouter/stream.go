package openrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const sseDoneMarker = "[DONE]"

// parseSSEStream reads an SSE text/event-stream response and sends
// StreamChunks to the returned channel. It closes the channel when the
// stream ends or an error occurs, and closes the body when done.
func parseSSEStream(ctx context.Context, body io.ReadCloser) <-chan valueobject.StreamChunk {
	ch := make(chan valueobject.StreamChunk)

	go func() {
		defer close(ch)
		defer body.Close()

		slog.DebugContext(ctx, "SSE stream started")

		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				slog.DebugContext(ctx, "SSE stream canceled", "error", err)
				ch <- valueobject.StreamChunk{Err: err}
				return
			}

			line := scanner.Text()

			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimSpace(data)

			if data == sseDoneMarker {
				slog.DebugContext(ctx, "SSE stream received DONE marker")
				ch <- valueobject.StreamChunk{Done: true}
				return
			}

			var chunk chatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				slog.ErrorContext(ctx, "SSE chunk parse error", "error", err)
				ch <- valueobject.StreamChunk{Err: fmt.Errorf("parsing SSE chunk: %w", err)}
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]

			if choice.FinishReason == "error" {
				ch <- valueobject.StreamChunk{Err: fmt.Errorf("LLM returned error finish_reason")}
				return
			}

			// Streaming tool call deltas are not accumulated here. Tool
			// resolution uses Complete (non-streaming) via executeToolLoop;
			// CompleteStream is only used for the final text response.
			if choice.FinishReason == "stop" || choice.FinishReason == "tool_calls" {
				ch <- valueobject.StreamChunk{Done: true}
				return
			}

			content := choice.Delta.Content
			if content != "" {
				ch <- valueobject.StreamChunk{Content: content}
			}
		}

		if err := scanner.Err(); err != nil {
			slog.ErrorContext(ctx, "SSE stream read error", "error", err)
			ch <- valueobject.StreamChunk{Err: fmt.Errorf("reading SSE stream: %w", err)}
		}
	}()

	return ch
}
