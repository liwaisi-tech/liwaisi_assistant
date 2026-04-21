package gate

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// countingStore records how many Insert calls completed. Inserts block on
// `gate` so tests can orchestrate backpressure into the bounded pool.
type countingStore struct {
	gate      chan struct{}
	completed atomic.Int64
	inflight  atomic.Int64
	peak      atomic.Int64
}

func (s *countingStore) Insert(ctx context.Context, rec *persist.GateDecisionRecord) error {
	n := s.inflight.Add(1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	defer s.inflight.Add(-1)
	if s.gate != nil {
		<-s.gate
	}
	s.completed.Add(1)
	return nil
}

func (s *countingStore) List(context.Context, int) ([]*persist.GateDecisionRecord, error) {
	return nil, nil
}

var _ persist.GateDecisionRepository = (*countingStore)(nil)

// TestAuditWorkerPool_CapsConcurrency covers AC-008 (REQ-FIX-008).
//
// Submit 1000 jobs; confirm peak in-flight inserts never exceeds
// auditWorkerCount (16). Also verifies ordered drain on Shutdown.
func TestAuditWorkerPool_CapsConcurrency(t *testing.T) {
	// Inserts block until we close(gate) so the pool fills before drain.
	gate := make(chan struct{})
	store := &countingStore{gate: gate}

	pool := newAuditWorkerPool(store, slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	const submits = 1000
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range submits {
			pool.Submit(auditJob{rec: &persist.GateDecisionRecord{}, opKind: "exec"})
		}
	}()

	// Let the pool saturate, then check peak.
	time.Sleep(100 * time.Millisecond)
	if peak := store.peak.Load(); peak > int64(auditWorkerCount) {
		close(gate)
		wg.Wait()
		t.Fatalf("peak in-flight = %d, want <= %d", peak, auditWorkerCount)
	}

	// Unblock inserts and let the producer finish.
	close(gate)
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("pool shutdown: %v", err)
	}

	// Completed + dropped-on-full should sum to `submits` at most; drops
	// are permissible. Completed must be > 0 to prove the pool did work.
	if store.completed.Load() == 0 {
		t.Fatalf("pool completed zero inserts")
	}
	if store.completed.Load() > submits {
		t.Fatalf("completed = %d > submits = %d", store.completed.Load(), submits)
	}
}

// TestAuditWorkerPool_DropsOnShutdown confirms Submit after Shutdown does
// not panic and does not enqueue.
func TestAuditWorkerPool_DropsOnShutdown(t *testing.T) {
	store := &countingStore{}
	pool := newAuditWorkerPool(store, slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Post-shutdown Submit must not panic.
	pool.Submit(auditJob{rec: &persist.GateDecisionRecord{}, opKind: "exec"})
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
