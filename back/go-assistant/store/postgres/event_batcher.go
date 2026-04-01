package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// EventBatcher buffers event records and flushes them in batches using CopyFrom.
// Flushes on batchSize OR flushInterval, whichever comes first.
// Submit blocks when the channel is full (backpressure).
type EventBatcher struct {
	repo          *EventRepository
	batchSize     int
	flushInterval time.Duration
	ch            chan *persist.EventRecord
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewEventBatcher creates a batcher with the given configuration.
// Defaults: batchSize=100, flushInterval=500ms, channel capacity=batchSize*2.
func NewEventBatcher(repo *EventRepository, batchSize int, flushInterval time.Duration) *EventBatcher {
	if batchSize <= 0 {
		batchSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
	}

	b := &EventBatcher{
		repo:          repo,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		ch:            make(chan *persist.EventRecord, batchSize*2),
		done:          make(chan struct{}),
	}

	b.wg.Add(1)
	go b.run()
	return b
}

// Submit enqueues an event for batch insertion. Blocks when the channel is full.
func (b *EventBatcher) Submit(ctx context.Context, event *persist.EventRecord) error {
	select {
	case b.ch <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return persist.ErrEventAppendFailed
	}
}

// Close flushes remaining events and stops the batcher goroutine.
func (b *EventBatcher) Close(ctx context.Context) error {
	close(b.done)
	b.wg.Wait()

	// Drain remaining events from channel
	var remaining []*persist.EventRecord
	for {
		select {
		case e := <-b.ch:
			remaining = append(remaining, e)
		default:
			if len(remaining) > 0 {
				return b.flush(ctx, remaining)
			}
			return nil
		}
	}
}

func (b *EventBatcher) run() {
	defer b.wg.Done()

	buf := make([]*persist.EventRecord, 0, b.batchSize)
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case e := <-b.ch:
			buf = append(buf, e)
			if len(buf) >= b.batchSize {
				_ = b.flush(context.Background(), buf)
				buf = buf[:0]
			}
		case <-ticker.C:
			if len(buf) > 0 {
				_ = b.flush(context.Background(), buf)
				buf = buf[:0]
			}
		case <-b.done:
			// Drain channel before exiting
			for {
				select {
				case e := <-b.ch:
					buf = append(buf, e)
				default:
					if len(buf) > 0 {
						_ = b.flush(context.Background(), buf)
					}
					return
				}
			}
		}
	}
}

func (b *EventBatcher) flush(ctx context.Context, events []*persist.EventRecord) error {
	return b.repo.Append(ctx, events...)
}
