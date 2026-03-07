package openrouter

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"
)

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries      int
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	RetryableStatus []int
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		BaseDelay:       1 * time.Second,
		MaxDelay:        30 * time.Second,
		RetryableStatus: []int{429, 502, 503, 529},
	}
}

// retryableError wraps an error with the HTTP status code for retry decisions.
type retryableError struct {
	statusCode int
	err        error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// WithRetry executes fn with exponential backoff + jitter on retryable errors.
func WithRetry[T any](ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T

	for attempt := 0; ; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		if attempt >= cfg.MaxRetries {
			return zero, err
		}

		re, ok := err.(*retryableError) //nolint:errorlint // intentional: only direct retryableError triggers retry
		if !ok || !isRetryable(re.statusCode, cfg.RetryableStatus) {
			return zero, err
		}

		delay := backoffDelay(attempt, cfg.BaseDelay, cfg.MaxDelay)
		slog.WarnContext(ctx, "retrying request",
			"attempt", attempt+1,
			"max_retries", cfg.MaxRetries,
			"status_code", re.statusCode,
			"delay", delay.String(),
		)

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func isRetryable(status int, retryable []int) bool {
	for _, s := range retryable {
		if status == s {
			return true
		}
	}
	return false
}

func backoffDelay(attempt int, base, maxDelay time.Duration) time.Duration {
	delay := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	if delay > maxDelay {
		delay = maxDelay
	}
	jitter := time.Duration(rand.Int64N(int64(delay)/2 + 1)) //nolint:gosec // jitter does not need crypto rand
	return delay/2 + jitter
}
