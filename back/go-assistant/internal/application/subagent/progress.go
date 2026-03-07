// Package subagent provides progress reporting for sub-agent execution.
package subagent

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ProgressReporter receives real-time lifecycle events from a sub-agent
// execution. Implementations must be safe for concurrent use.
type ProgressReporter interface {
	Report(event valueobject.ToolEvent)
}

type progressKeyType struct{}

var progressKey progressKeyType

// WithProgressReporter returns a child context carrying the given reporter.
func WithProgressReporter(ctx context.Context, r ProgressReporter) context.Context {
	return context.WithValue(ctx, progressKey, r)
}

// ProgressReporterFrom extracts a ProgressReporter from the context.
// Returns nil if no reporter is attached.
func ProgressReporterFrom(ctx context.Context) ProgressReporter {
	r, _ := ctx.Value(progressKey).(ProgressReporter)
	return r
}

// channelReporter adapts a StreamChunk channel into a ProgressReporter.
// Sends are non-blocking: if the channel is full the event is dropped.
type channelReporter struct {
	ch chan<- valueobject.StreamChunk
}

// NewChannelReporter creates a ProgressReporter that writes ToolEvents
// as StreamChunks to ch. Writes use a select with default to avoid
// blocking the sub-agent goroutine when the consumer is slow.
func NewChannelReporter(ch chan<- valueobject.StreamChunk) ProgressReporter {
	return &channelReporter{ch: ch}
}

// Report sends the event as a StreamChunk. If the channel is full the
// event is silently dropped to prevent backpressure on the runner.
func (r *channelReporter) Report(event valueobject.ToolEvent) {
	select {
	case r.ch <- valueobject.StreamChunk{ToolEvent: &event}:
	default:
	}
}
