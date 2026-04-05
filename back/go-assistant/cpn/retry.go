package cpn

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// RetryPolicy configures retry behavior for a transition.
// Implements Building Block 6 (Recovery) at the transition level.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts including the first.
	// Default: 1 (no retry). Set to 3 for standard retry.
	MaxAttempts int

	// InitialWait is the duration before the first retry.
	InitialWait time.Duration

	// MaxWait caps the exponential backoff growth.
	MaxWait time.Duration

	// Multiplier scales the wait after each retry. Default: 2.0.
	Multiplier float64

	// RetryOn decides whether an error is retryable.
	// If nil, all errors are retried up to MaxAttempts.
	RetryOn func(err error, attempt int) bool

	// CircuitBreaker configures failure-rate tracking across all firings.
	// If nil, no circuit breaker is used.
	CircuitBreaker *CircuitBreakerConfig
}

// CircuitBreakerConfig defines when a circuit breaker trips.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures before tripping.
	FailureThreshold int

	// OpenDuration is how long the circuit stays open before allowing a probe.
	OpenDuration time.Duration
}

// CircuitBreakerState tracks the runtime state of a circuit breaker.
// All methods are thread-safe (mutex-protected).
//
// State machine:
//
//	closed → (Failures >= FailureThreshold) → open
//	open   → (time since TrippedAt > OpenDuration) → half-open
//	half-open → (success) → closed
//	half-open → (failure) → open
type CircuitBreakerState struct {
	Config    *CircuitBreakerConfig
	Failures  int
	TrippedAt *time.Time
	mu        sync.Mutex
	nowFunc   func() time.Time // testing seam; defaults to time.Now
}

// NewCircuitBreakerState creates a CircuitBreakerState in the closed state.
func NewCircuitBreakerState(config *CircuitBreakerConfig) *CircuitBreakerState {
	return &CircuitBreakerState{
		Config:  config,
		nowFunc: time.Now,
	}
}

// Allow returns true if the circuit is closed or half-open (probe allowed).
// Returns false if the circuit is open (within OpenDuration).
func (cb *CircuitBreakerState) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.TrippedAt == nil {
		return true
	}

	// Open state — check if OpenDuration has elapsed.
	if cb.nowFunc().Sub(*cb.TrippedAt) > cb.Config.OpenDuration {
		// Transition to half-open: reset TrippedAt to allow one probe.
		cb.TrippedAt = nil
		return true
	}

	return false
}

// RecordFailure increments the failure counter and trips the breaker
// when FailureThreshold is reached.
func (cb *CircuitBreakerState) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.Failures++
	if cb.Failures >= cb.Config.FailureThreshold {
		now := cb.nowFunc()
		cb.TrippedAt = &now
	}
}

// RecordSuccess resets failures and closes the circuit.
func (cb *CircuitBreakerState) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.Failures = 0
	cb.TrippedAt = nil
}

// DefaultRetryPolicy returns the standard retry policy:
// MaxAttempts=3, InitialWait=500ms, MaxWait=30s, Multiplier=2.0.
// RetryOn skips context.Canceled and context.DeadlineExceeded.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 3,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     30 * time.Second,
		Multiplier:  2.0,
		RetryOn: func(err error, _ int) bool {
			if errors.Is(err, context.Canceled) {
				return false
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return false
			}
			return true
		},
	}
}

// sleepFunc is the signature for a cancellable sleep used by the retry loop.
type sleepFunc func(ctx context.Context, d time.Duration) error

// defaultSleep blocks for duration d or until ctx is canceled.
func defaultSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fireWithRetry executes fire() with retry and circuit breaker logic.
//
// If retry is nil, fire() is called once with no retry.
// If cb is non-nil and open, returns ErrCircuitOpen without calling fire.
// On success, cb.RecordSuccess() is called. On failure, cb.RecordFailure().
// Backoff is exponential: InitialWait * Multiplier^(attempt-1), capped at MaxWait.
// Context cancellation terminates the wait immediately with ctx.Err().
// The returned []TokenSnapshot are the output snapshots built by the fire function.
// The returned float64 is the cost (USD) incurred by the fire function.
func fireWithRetry(ctx context.Context, retry *RetryPolicy, cb *CircuitBreakerState, fire func() ([]TokenSnapshot, float64, error)) ([]TokenSnapshot, float64, error) {
	return doRetry(ctx, retry, cb, fire, defaultSleep)
}

// doRetry is the internal retry loop with an injectable sleep for testing.
func doRetry(ctx context.Context, retry *RetryPolicy, cb *CircuitBreakerState, fire func() ([]TokenSnapshot, float64, error), sleep sleepFunc) ([]TokenSnapshot, float64, error) {
	// Circuit breaker gate check.
	if cb != nil && !cb.Allow() {
		return nil, 0, ErrCircuitOpen
	}

	// No retry policy — fire once.
	if retry == nil {
		return fireOnce(cb, fire)
	}

	maxAttempts := max(retry.MaxAttempts, 1)

	var lastErr error
	var totalCost float64
	var lastSnaps []TokenSnapshot
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		snaps, cost, err := fire()
		totalCost += cost
		lastErr = err
		if lastErr == nil {
			lastSnaps = snaps
			if cb != nil {
				cb.RecordSuccess()
			}
			return lastSnaps, totalCost, nil
		}

		if cb != nil {
			cb.RecordFailure()
		}

		// Check if we should retry this error.
		if retry.RetryOn != nil && !retry.RetryOn(lastErr, attempt) {
			return nil, totalCost, lastErr
		}

		// Don't sleep after the last attempt.
		if attempt == maxAttempts {
			break
		}

		// Exponential backoff.
		wait := backoffDuration(retry, attempt)
		if err := sleep(ctx, wait); err != nil {
			return nil, totalCost, err
		}
	}

	return nil, totalCost, lastErr
}

// fireOnce calls fire() and records success/failure on the circuit breaker.
func fireOnce(cb *CircuitBreakerState, fire func() ([]TokenSnapshot, float64, error)) ([]TokenSnapshot, float64, error) {
	snaps, cost, err := fire()
	if cb != nil {
		if err == nil {
			cb.RecordSuccess()
		} else {
			cb.RecordFailure()
		}
	}
	return snaps, cost, err
}

// backoffDuration computes the wait time for the given attempt.
// wait = InitialWait * Multiplier^(attempt-1), capped at MaxWait.
func backoffDuration(retry *RetryPolicy, attempt int) time.Duration {
	wait := float64(retry.InitialWait) * math.Pow(retry.Multiplier, float64(attempt-1))
	return min(time.Duration(wait), retry.MaxWait)
}
