package subagent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestWithProgressReporter_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		reporter ProgressReporter
		wantNil  bool
	}{
		{
			name:     "non-nil reporter survives round-trip",
			reporter: NewChannelReporter(make(chan<- valueobject.StreamChunk, 1)),
			wantNil:  false,
		},
		{
			name:     "nil reporter yields nil on extraction",
			reporter: nil,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tt.reporter != nil {
				ctx = WithProgressReporter(ctx, tt.reporter)
			}

			got := ProgressReporterFrom(ctx)
			if tt.wantNil && got != nil {
				t.Errorf("ProgressReporterFrom() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("ProgressReporterFrom() = nil, want non-nil")
			}
		})
	}
}

func TestProgressReporterFrom_EmptyContext(t *testing.T) {
	t.Parallel()

	got := ProgressReporterFrom(context.Background())
	if got != nil {
		t.Errorf("ProgressReporterFrom(background) = %v, want nil", got)
	}
}

func TestChannelReporter_Report(t *testing.T) {
	t.Parallel()

	ch := make(chan valueobject.StreamChunk, 4)
	reporter := NewChannelReporter(ch)

	event := valueobject.ToolEvent{
		Kind:     valueobject.ToolEventSubAgentStarted,
		ToolName: "test-agent",
		Total:    10,
		Elapsed:  2 * time.Second,
	}

	reporter.Report(event)

	select {
	case chunk := <-ch:
		if chunk.ToolEvent == nil {
			t.Fatal("ToolEvent is nil")
		}
		if chunk.ToolEvent.Kind != valueobject.ToolEventSubAgentStarted {
			t.Errorf("Kind = %q, want %q", chunk.ToolEvent.Kind, valueobject.ToolEventSubAgentStarted)
		}
		if chunk.ToolEvent.ToolName != "test-agent" {
			t.Errorf("ToolName = %q, want %q", chunk.ToolEvent.ToolName, "test-agent")
		}
	default:
		t.Fatal("expected a chunk on the channel")
	}
}

func TestChannelReporter_NonBlocking(t *testing.T) {
	t.Parallel()

	// Unbuffered channel: send should not block.
	ch := make(chan valueobject.StreamChunk)
	reporter := NewChannelReporter(ch)

	done := make(chan struct{})
	go func() {
		reporter.Report(valueobject.ToolEvent{
			Kind:     valueobject.ToolEventSubAgentThinking,
			ToolName: "slow-consumer",
		})
		close(done)
	}()

	select {
	case <-done:
		// Report returned without blocking — success.
	case <-time.After(time.Second):
		t.Fatal("Report blocked on full channel")
	}
}

func TestChannelReporter_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	ch := make(chan valueobject.StreamChunk, 100)
	reporter := NewChannelReporter(ch)

	const goroutines = 10
	const eventsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				reporter.Report(valueobject.ToolEvent{
					Kind:      valueobject.ToolEventSubAgentToolCall,
					ToolName:  "concurrent-agent",
					Iteration: id*eventsPerGoroutine + j,
				})
			}
		}(i)
	}

	wg.Wait()

	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			if received != goroutines*eventsPerGoroutine {
				t.Errorf("received %d events, want %d", received, goroutines*eventsPerGoroutine)
			}
			return
		}
	}
}

func TestReportProgress_NilReporter(t *testing.T) {
	t.Parallel()

	// Must not panic when reporter is nil.
	reportProgress(nil, valueobject.ToolEvent{
		Kind:     valueobject.ToolEventSubAgentStarted,
		ToolName: "nil-test",
	})
}
