package cpn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// sleepRecorder captures sleep durations without blocking.
type sleepRecorder struct {
	durations []time.Duration
}

func (s *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	s.durations = append(s.durations, d)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// ── DefaultRetryPolicy ──────────────────────────────────────────────────

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()

	if p.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", p.MaxAttempts)
	}
	if p.InitialWait != 500*time.Millisecond {
		t.Fatalf("InitialWait = %v, want 500ms", p.InitialWait)
	}
	if p.MaxWait != 30*time.Second {
		t.Fatalf("MaxWait = %v, want 30s", p.MaxWait)
	}
	if p.Multiplier != 2.0 {
		t.Fatalf("Multiplier = %f, want 2.0", p.Multiplier)
	}
	if p.RetryOn == nil {
		t.Fatal("RetryOn should not be nil")
	}
	if p.CircuitBreaker != nil {
		t.Fatal("CircuitBreaker should be nil by default")
	}
}

func TestDefaultRetryPolicy_RetryOn(t *testing.T) {
	p := DefaultRetryPolicy()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"context_canceled", context.Canceled, false},
		{"deadline_exceeded", context.DeadlineExceeded, false},
		{"wrapped_canceled", errors.Join(errors.New("wrap"), context.Canceled), false},
		{"generic_error", errors.New("transient"), true},
		{"circuit_open", ErrCircuitOpen, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.RetryOn(tt.err, 1)
			if got != tt.want {
				t.Errorf("RetryOn(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ── CircuitBreakerState ─────────────────────────────────────────────────

func TestNewCircuitBreakerState(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: 10 * time.Second}
	cb := NewCircuitBreakerState(cfg)

	if cb.Config != cfg {
		t.Fatal("Config mismatch")
	}
	if cb.Failures != 0 {
		t.Fatalf("Failures = %d, want 0", cb.Failures)
	}
	if cb.TrippedAt != nil {
		t.Fatal("TrippedAt should be nil")
	}
	if cb.nowFunc == nil {
		t.Fatal("nowFunc should default to non-nil")
	}
}

func TestCircuitBreakerState_Allow_Closed(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 3, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)

	if !cb.Allow() {
		t.Fatal("Allow() = false, want true for closed circuit")
	}
}

func TestCircuitBreakerState_Allow_Open(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 2, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	cb.RecordFailure() // trips

	if cb.Allow() {
		t.Fatal("Allow() = true, want false for open circuit")
	}
}

func TestCircuitBreakerState_Allow_HalfOpen(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 2, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	cb.RecordFailure() // trips

	if cb.Allow() {
		t.Fatal("should be open")
	}

	// Advance past OpenDuration
	now = now.Add(6 * time.Second)

	if !cb.Allow() {
		t.Fatal("Allow() = false, want true for half-open (probe)")
	}
	if cb.TrippedAt != nil {
		t.Fatal("TrippedAt should be reset to nil in half-open")
	}
}

func TestCircuitBreakerState_RecordFailure(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 3, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	if cb.Failures != 1 {
		t.Fatalf("Failures = %d, want 1", cb.Failures)
	}
	if cb.TrippedAt != nil {
		t.Fatal("should not be tripped after 1 failure")
	}

	cb.RecordFailure()
	if cb.Failures != 2 {
		t.Fatalf("Failures = %d, want 2", cb.Failures)
	}
	if cb.TrippedAt != nil {
		t.Fatal("should not be tripped after 2 failures (threshold=3)")
	}

	cb.RecordFailure() // trips at threshold
	if cb.Failures != 3 {
		t.Fatalf("Failures = %d, want 3", cb.Failures)
	}
	if cb.TrippedAt == nil {
		t.Fatal("should be tripped after 3 failures")
	}
}

func TestCircuitBreakerState_RecordSuccess(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 2, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	cb.RecordFailure() // trips

	if cb.TrippedAt == nil {
		t.Fatal("should be tripped")
	}

	cb.RecordSuccess()

	if cb.Failures != 0 {
		t.Fatalf("Failures = %d, want 0 after success", cb.Failures)
	}
	if cb.TrippedAt != nil {
		t.Fatal("TrippedAt should be nil after success")
	}
}

func TestCircuitBreakerState_HalfOpenFailureRetrips(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 2, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	cb.RecordFailure() // trips

	// Advance past OpenDuration -> half-open
	now = now.Add(6 * time.Second)
	if !cb.Allow() {
		t.Fatal("should be half-open")
	}

	// Probe fails -> re-trips
	cb.RecordFailure()
	if cb.TrippedAt == nil {
		t.Fatal("should re-trip after half-open failure")
	}
	if cb.Allow() {
		t.Fatal("should be open again")
	}
}

func TestCircuitBreakerState_Concurrent(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 100, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			cb.RecordFailure()
		}()
		go func() {
			defer wg.Done()
			cb.Allow()
		}()
	}
	wg.Wait()

	// Just verifying no race — exact state depends on scheduling
}

func TestCircuitBreakerState_ThresholdOne(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 1, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure() // trips immediately
	if cb.TrippedAt == nil {
		t.Fatal("should trip on first failure with threshold=1")
	}
	if cb.Allow() {
		t.Fatal("should be open")
	}
}

func TestCircuitBreakerState_RecordFailureAfterSuccess(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 3, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // resets

	if cb.Failures != 0 {
		t.Fatalf("Failures = %d, want 0", cb.Failures)
	}

	// Need full threshold again to trip
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.TrippedAt != nil {
		t.Fatal("should not be tripped yet (2 < 3)")
	}
	cb.RecordFailure() // trips
	if cb.TrippedAt == nil {
		t.Fatal("should be tripped")
	}
}

// ── fireWithRetry ───────────────────────────────────────────────────────

func TestFireWithRetry_NilRetry(t *testing.T) {
	called := 0
	_, err := doRetry(context.Background(), nil, nil, func() (float64, error) {
		called++
		return 0, nil
	}, (&sleepRecorder{}).sleep)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
}

func TestFireWithRetry_NilRetry_Error(t *testing.T) {
	want := errors.New("boom")
	_, err := doRetry(context.Background(), nil, nil, func() (float64, error) {
		return 0, want
	}, (&sleepRecorder{}).sleep)

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestFireWithRetry_SuccessFirstAttempt(t *testing.T) {
	rec := &sleepRecorder{}
	policy := DefaultRetryPolicy()
	called := 0

	_, err := doRetry(context.Background(), policy, nil, func() (float64, error) {
		called++
		return 0, nil
	}, rec.sleep)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
	if len(rec.durations) != 0 {
		t.Fatalf("sleeps = %d, want 0", len(rec.durations))
	}
}

func TestFireWithRetry_SuccessSecondAttempt(t *testing.T) {
	rec := &sleepRecorder{}
	policy := DefaultRetryPolicy()
	attempt := 0

	_, err := doRetry(context.Background(), policy, nil, func() (float64, error) {
		attempt++
		if attempt == 1 {
			return 0, errors.New("transient")
		}
		return 0, nil
	}, rec.sleep)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if attempt != 2 {
		t.Fatalf("attempts = %d, want 2", attempt)
	}
	if len(rec.durations) != 1 {
		t.Fatalf("sleeps = %d, want 1", len(rec.durations))
	}
	if rec.durations[0] != 500*time.Millisecond {
		t.Fatalf("sleep[0] = %v, want 500ms", rec.durations[0])
	}
}

func TestFireWithRetry_ExhaustedRetries(t *testing.T) {
	rec := &sleepRecorder{}
	policy := DefaultRetryPolicy() // MaxAttempts=3
	lastErr := errors.New("final-error")
	attempt := 0

	_, err := doRetry(context.Background(), policy, nil, func() (float64, error) {
		attempt++
		if attempt == 3 {
			return 0, lastErr
		}
		return 0, errors.New("transient")
	}, rec.sleep)

	if !errors.Is(err, lastErr) {
		t.Fatalf("err = %v, want %v", err, lastErr)
	}
	if attempt != 3 {
		t.Fatalf("attempts = %d, want 3", attempt)
	}
	if len(rec.durations) != 2 {
		t.Fatalf("sleeps = %d, want 2", len(rec.durations))
	}
}

func TestFireWithRetry_RetryOnStopsEarly(t *testing.T) {
	rec := &sleepRecorder{}
	nonRetryable := errors.New("non-retryable")
	policy := &RetryPolicy{
		MaxAttempts: 5,
		InitialWait: 100 * time.Millisecond,
		MaxWait:     1 * time.Second,
		Multiplier:  2.0,
		RetryOn: func(err error, _ int) bool {
			return !errors.Is(err, nonRetryable)
		},
	}
	attempt := 0

	_, err := doRetry(context.Background(), policy, nil, func() (float64, error) {
		attempt++
		return 0, nonRetryable
	}, rec.sleep)

	if !errors.Is(err, nonRetryable) {
		t.Fatalf("err = %v, want %v", err, nonRetryable)
	}
	if attempt != 1 {
		t.Fatalf("attempts = %d, want 1 (should stop on first non-retryable)", attempt)
	}
	if len(rec.durations) != 0 {
		t.Fatalf("sleeps = %d, want 0", len(rec.durations))
	}
}

func TestFireWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempt := 0

	policy := &RetryPolicy{
		MaxAttempts: 5,
		InitialWait: 100 * time.Millisecond,
		MaxWait:     1 * time.Second,
		Multiplier:  2.0,
	}

	sleepFn := func(ctx context.Context, d time.Duration) error {
		cancel() // cancel during sleep
		return ctx.Err()
	}

	_, err := doRetry(ctx, policy, nil, func() (float64, error) {
		attempt++
		return 0, errors.New("transient")
	}, sleepFn)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if attempt != 1 {
		t.Fatalf("attempts = %d, want 1", attempt)
	}
}

func TestFireWithRetry_CircuitOpen(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 1, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	now := time.Now()
	cb.nowFunc = func() time.Time { return now }
	cb.RecordFailure() // trips

	rec := &sleepRecorder{}
	policy := DefaultRetryPolicy()
	called := false

	_, err := doRetry(context.Background(), policy, cb, func() (float64, error) {
		called = true
		return 0, nil
	}, rec.sleep)

	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Fatal("fire() should not be called when circuit is open")
	}
}

func TestFireWithRetry_RecordsSuccessOnCB(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)
	cb.RecordFailure()
	cb.RecordFailure()

	rec := &sleepRecorder{}
	policy := DefaultRetryPolicy()

	_, err := doRetry(context.Background(), policy, cb, func() (float64, error) {
		return 0, nil
	}, rec.sleep)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if cb.Failures != 0 {
		t.Fatalf("Failures = %d, want 0 after success", cb.Failures)
	}
}

func TestFireWithRetry_RecordsFailureOnCB(t *testing.T) {
	cfg := &CircuitBreakerConfig{FailureThreshold: 10, OpenDuration: 5 * time.Second}
	cb := NewCircuitBreakerState(cfg)

	rec := &sleepRecorder{}
	policy := &RetryPolicy{
		MaxAttempts: 3,
		InitialWait: 100 * time.Millisecond,
		MaxWait:     1 * time.Second,
		Multiplier:  2.0,
	}

	_, err := doRetry(context.Background(), policy, cb, func() (float64, error) {
		return 0, errors.New("fail")
	}, rec.sleep)

	if err == nil {
		t.Fatal("err = nil, want error")
	}
	if cb.Failures != 3 {
		t.Fatalf("Failures = %d, want 3", cb.Failures)
	}
}

func TestFireWithRetry_BackoffProgression(t *testing.T) {
	rec := &sleepRecorder{}
	policy := &RetryPolicy{
		MaxAttempts: 6,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     30 * time.Second,
		Multiplier:  2.0,
	}

	_, _ = doRetry(context.Background(), policy, nil, func() (float64, error) {
		return 0, errors.New("fail")
	}, rec.sleep)

	// 6 attempts = 5 sleeps
	if len(rec.durations) != 5 {
		t.Fatalf("sleeps = %d, want 5", len(rec.durations))
	}

	// Expected: 500ms, 1s, 2s, 4s, 8s
	expected := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}
	for i, want := range expected {
		if rec.durations[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, rec.durations[i], want)
		}
	}
}

func TestFireWithRetry_MaxWaitCap(t *testing.T) {
	rec := &sleepRecorder{}
	policy := &RetryPolicy{
		MaxAttempts: 10,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     2 * time.Second,
		Multiplier:  2.0,
	}

	_, _ = doRetry(context.Background(), policy, nil, func() (float64, error) {
		return 0, errors.New("fail")
	}, rec.sleep)

	// 10 attempts = 9 sleeps
	if len(rec.durations) != 9 {
		t.Fatalf("sleeps = %d, want 9", len(rec.durations))
	}

	// After 500ms, 1s the next should be capped at 2s
	for i := 2; i < len(rec.durations); i++ {
		if rec.durations[i] > 2*time.Second {
			t.Errorf("sleep[%d] = %v, exceeds MaxWait 2s", i, rec.durations[i])
		}
	}
}

func TestFireWithRetry_MaxAttemptsZero(t *testing.T) {
	rec := &sleepRecorder{}
	policy := &RetryPolicy{
		MaxAttempts: 0,
		InitialWait: 100 * time.Millisecond,
		MaxWait:     1 * time.Second,
		Multiplier:  2.0,
	}
	called := 0

	_, err := doRetry(context.Background(), policy, nil, func() (float64, error) {
		called++
		return 0, nil
	}, rec.sleep)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1 (MaxAttempts=0 treated as 1)", called)
	}
}

func TestFireWithRetry_MultiplierOne(t *testing.T) {
	rec := &sleepRecorder{}
	policy := &RetryPolicy{
		MaxAttempts: 4,
		InitialWait: 200 * time.Millisecond,
		MaxWait:     10 * time.Second,
		Multiplier:  1.0,
	}

	_, _ = doRetry(context.Background(), policy, nil, func() (float64, error) {
		return 0, errors.New("fail")
	}, rec.sleep)

	// Constant wait: 200ms, 200ms, 200ms
	for i, d := range rec.durations {
		if d != 200*time.Millisecond {
			t.Errorf("sleep[%d] = %v, want 200ms (constant with multiplier=1)", i, d)
		}
	}
}

// ── fireWithRetry exported function ─────────────────────────────────────

func TestFireWithRetry_Exported(t *testing.T) {
	// Smoke test for the exported function — verifies it calls doRetry with defaultSleep.
	// Uses nil retry so fire() is called once with no actual sleep.
	called := 0
	_, err := fireWithRetry(context.Background(), nil, nil, func() (float64, error) {
		called++
		return 0, nil
	})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
}
