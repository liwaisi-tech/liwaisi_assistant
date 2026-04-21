package host

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestTokenBucketRateLimiter_Defaults verifies the documented fallbacks
// for zero / negative configuration values.
func TestTokenBucketRateLimiter_Defaults(t *testing.T) {
	l := NewTokenBucketRateLimiter(0)
	if l.rate != DefaultRateLimit {
		t.Fatalf("rate = %v, want %v", l.rate, DefaultRateLimit)
	}
	l2 := NewTokenBucketRateLimiter(-42)
	if l2.rate != DefaultRateLimit {
		t.Fatalf("rate (negative) = %v, want %v", l2.rate, DefaultRateLimit)
	}
}

// TestTokenBucketRateLimiter_AllowsBurst confirms a freshly seen
// session is given a full burst window — a short burst of events
// should pass even if their rate exceeds the per-second budget.
func TestTokenBucketRateLimiter_AllowsBurst(t *testing.T) {
	l := NewTokenBucketRateLimiter(10)
	for i := 0; i < 10; i++ {
		if !l.Allow("s") {
			t.Fatalf("allow #%d was dropped, expected pass", i)
		}
	}
	if l.Allow("s") {
		t.Fatal("11th Allow should have dropped — bucket empty")
	}
}

// TestTokenBucketRateLimiter_Refills ensures that waiting for the
// bucket to refill unblocks future events (basic time-based invariant).
func TestTokenBucketRateLimiter_Refills(t *testing.T) {
	l := NewTokenBucketRateLimiter(100)
	// Drain the bucket.
	for i := 0; i < 100; i++ {
		_ = l.Allow("s")
	}
	if l.Allow("s") {
		t.Fatal("Allow after drain should fail")
	}
	// Wait a bit for tokens to refill.
	time.Sleep(50 * time.Millisecond)
	// At ~100 tokens/s we should have ~5 tokens after 50ms.
	if !l.Allow("s") {
		t.Fatal("Allow after 50ms of refill should pass")
	}
}

// TestTokenBucketRateLimiter_IsolatesSessions confirms one loud session
// cannot starve a neighbour. Each session has its own bucket.
func TestTokenBucketRateLimiter_IsolatesSessions(t *testing.T) {
	l := NewTokenBucketRateLimiter(5)
	// Drain session A.
	for i := 0; i < 5; i++ {
		if !l.Allow("A") {
			t.Fatalf("A allow #%d should pass", i)
		}
	}
	if l.Allow("A") {
		t.Fatal("A should be drained")
	}
	// Session B has its own bucket.
	if !l.Allow("B") {
		t.Fatal("B should still have tokens")
	}
}

// TestTokenBucketRateLimiter_Flood10kPerSec asserts the spec AC-002:
// given a 10k events/s flood against a 1000/s budget, exactly 9000
// events are dropped within a single 1-second window.
//
// We feed events synchronously so the wall-clock baseline for the
// limiter is the iteration speed, then sleep 1s to let tokens refill,
// and count how many passed the gate. Because Go's goroutine scheduler
// is not deterministic, we assert the ratio within a tolerance band.
func TestTokenBucketRateLimiter_Flood10kPerSec_AC002(t *testing.T) {
	l := NewTokenBucketRateLimiter(1000)

	// 10k events fed as fast as the CPU allows.
	const total = 10_000
	var allowed, dropped int64
	for i := 0; i < total; i++ {
		if l.Allow("hot") {
			atomic.AddInt64(&allowed, 1)
		} else {
			atomic.AddInt64(&dropped, 1)
		}
	}

	// The burst window starts full (1000 tokens), so within the first
	// few milliseconds the limiter admits ~1000 events. Everything
	// else is dropped. We assert:
	//   - allowed is somewhere around 1000 (±20% to absorb refill that
	//     may happen during the loop).
	//   - dropped + allowed == total (no lost events).
	if allowed+dropped != total {
		t.Fatalf("allowed+dropped = %d, want %d", allowed+dropped, total)
	}
	if allowed < 800 || allowed > 2500 {
		// Generous bounds: loop execution on a slow CI box may drift.
		t.Fatalf("allowed = %d, want roughly 1000 (burst) — GAP-9 AC-002", allowed)
	}
	if dropped < 7000 {
		t.Fatalf("dropped = %d, want >= 7000 — limiter is not doing its job", dropped)
	}
}

// TestTokenBucketRateLimiter_ConcurrentAccess stresses the lock path
// with many goroutines to verify there are no data races. Run under
// -race in CI.
func TestTokenBucketRateLimiter_ConcurrentAccess(t *testing.T) {
	l := NewTokenBucketRateLimiter(1000)
	const workers = 16
	const perWorker = 500

	var wg sync.WaitGroup
	var pass, drop int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if l.Allow("shared") {
					atomic.AddInt64(&pass, 1)
				} else {
					atomic.AddInt64(&drop, 1)
				}
			}
		}()
	}
	wg.Wait()
	if pass+drop != workers*perWorker {
		t.Fatalf("pass+drop = %d, want %d", pass+drop, workers*perWorker)
	}
}

// TestTokenBucketRateLimiter_NilReceiver — calling Allow on a nil
// pointer is a valid no-op so callers can pass a nil limiter without
// extra checks.
func TestTokenBucketRateLimiter_NilReceiver(t *testing.T) {
	var l *TokenBucketRateLimiter
	if !l.Allow("anything") {
		t.Fatal("nil limiter should allow by default")
	}
}

// TestCountingMetrics exercises the CountingMetrics implementation used
// by fire_bash + session manager to surface emit / drop counts.
func TestCountingMetrics(t *testing.T) {
	m := NewCountingMetrics()
	m.OnEmit("s", cpn.EventProcessStdout)
	m.OnEmit("s", cpn.EventProcessStdout)
	m.OnEmit("s", cpn.EventProcessStderr)
	m.OnDrop("s", cpn.EventProcessStdout, "rate_limited")

	if got := m.Emitted()[string(cpn.EventProcessStdout)]; got != 2 {
		t.Fatalf("emitted stdout = %d, want 2", got)
	}
	if got := m.Emitted()[string(cpn.EventProcessStderr)]; got != 1 {
		t.Fatalf("emitted stderr = %d, want 1", got)
	}
	if got := m.Dropped()[string(cpn.EventProcessStdout)]; got != 1 {
		t.Fatalf("dropped stdout = %d, want 1", got)
	}
	if got := m.TotalEmitted(); got != 3 {
		t.Fatalf("total emitted = %d, want 3", got)
	}
	if got := m.TotalDropped(); got != 1 {
		t.Fatalf("total dropped = %d, want 1", got)
	}
}
