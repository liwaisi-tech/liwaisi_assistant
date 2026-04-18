// Package host — rate_limiter.go implements cpn.ProcessEventRateLimiter.
//
// GAP-9: A verbose command (strace, find /, noisy test runner) can emit
// millions of events per second. Without a per-session cap one bash
// transition can saturate the executor's observer drain and starve every
// other CPN on the host. The limiter enforces a token-bucket budget that
// the fire_bash dispatcher and session manager consult on every emit.
//
// The implementation is deliberately lock-short: each session owns its
// own bucket and the shared map is only held when we lazy-allocate.
package host

import (
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// DefaultRateLimit is the per-session event rate (events per second)
// applied when NewTokenBucketRateLimiter receives rate <= 0. 1000 events
// per second matches the GAP-9 spec (REQ-004) and leaves plenty of
// headroom for typical interactive shells while still capping pathological
// output volumes.
const DefaultRateLimit = 1000

// TokenBucketRateLimiter is a per-session token bucket implementation of
// cpn.ProcessEventRateLimiter. It permits rate events per second with a
// burst equal to rate (so a process that has been silent for one second
// may burst up to rate events without dropping).
//
// Session state is lazily allocated on first Allow call and retained for
// the lifetime of the limiter. Typical deployments instantiate one
// limiter per process and share it across all bash transitions.
type TokenBucketRateLimiter struct {
	rate  float64 // tokens added per second
	burst float64 // bucket capacity

	mu      sync.Mutex
	buckets map[string]*tokenBucket

	// now is injectable for deterministic tests. Defaults to time.Now.
	now func() time.Time
}

// tokenBucket holds the runtime state for a single session.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// NewTokenBucketRateLimiter builds a limiter with the given per-second
// rate. A non-positive rate is replaced by DefaultRateLimit (1000).
func NewTokenBucketRateLimiter(rate int) *TokenBucketRateLimiter {
	if rate <= 0 {
		rate = DefaultRateLimit
	}
	return &TokenBucketRateLimiter{
		rate:    float64(rate),
		burst:   float64(rate),
		buckets: make(map[string]*tokenBucket),
		now:     time.Now,
	}
}

// Compile-time interface check.
var _ cpn.ProcessEventRateLimiter = (*TokenBucketRateLimiter)(nil)

// Allow reports whether a session may emit one more event at this
// instant. It returns true and consumes one token, or returns false if
// the bucket is empty. Safe for concurrent use.
//
// An empty sessionID is valid (one-shot bash transitions have no session
// ID) and shares a single "anonymous" bucket.
func (l *TokenBucketRateLimiter) Allow(sessionID string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[sessionID]
	if !ok {
		// Fresh bucket starts full so short-lived sessions aren't
		// penalised by the very first event.
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[sessionID] = b
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.rate
			if b.tokens > l.burst {
				b.tokens = l.burst
			}
			b.last = now
		}
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// Reset clears the per-session state. Primarily useful in tests.
func (l *TokenBucketRateLimiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets = make(map[string]*tokenBucket)
}

// CountingMetrics is an in-memory cpn.ProcessEventMetrics used by tests
// and light-weight deployments to count emitted vs dropped events
// without wiring a full observability stack.
type CountingMetrics struct {
	mu      sync.Mutex
	emitted map[string]int64
	dropped map[string]int64
}

// NewCountingMetrics builds an empty counter.
func NewCountingMetrics() *CountingMetrics {
	return &CountingMetrics{
		emitted: make(map[string]int64),
		dropped: make(map[string]int64),
	}
}

// Compile-time interface check.
var _ cpn.ProcessEventMetrics = (*CountingMetrics)(nil)

// OnEmit increments the emit counter for the event kind.
func (m *CountingMetrics) OnEmit(_ string, kind cpn.EventType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitted[string(kind)]++
}

// OnDrop increments the drop counter for the event kind (ignoring reason
// for simplicity; callers can tag via a wrapper if they need detail).
func (m *CountingMetrics) OnDrop(_ string, kind cpn.EventType, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropped[string(kind)]++
}

// Emitted returns a snapshot of the per-kind emit counter.
func (m *CountingMetrics) Emitted() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.emitted))
	for k, v := range m.emitted {
		out[k] = v
	}
	return out
}

// Dropped returns a snapshot of the per-kind drop counter.
func (m *CountingMetrics) Dropped() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.dropped))
	for k, v := range m.dropped {
		out[k] = v
	}
	return out
}

// TotalEmitted returns the sum of all emit counters across kinds.
func (m *CountingMetrics) TotalEmitted() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, v := range m.emitted {
		total += v
	}
	return total
}

// TotalDropped returns the sum of all drop counters across kinds.
func (m *CountingMetrics) TotalDropped() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, v := range m.dropped {
		total += v
	}
	return total
}
