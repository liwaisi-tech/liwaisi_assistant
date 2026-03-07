package subagent

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RecoveryConfig controls failure handling for a subagent execution.
type RecoveryConfig struct {
	// Timeout is the overall execution timeout. Zero means no timeout
	// (parent context deadline still applies).
	Timeout time.Duration

	// MaxLLMRetries is the number of retry attempts per LLM call.
	// The total number of attempts is MaxLLMRetries + 1.
	MaxLLMRetries int

	// BaseDelay is the initial delay before the first retry.
	BaseDelay time.Duration

	// MaxDelay caps the exponential backoff delay.
	MaxDelay time.Duration

	// RetryableCheck determines whether an error is eligible for retry.
	// When nil, all errors are retried.
	RetryableCheck func(error) bool
}

// DefaultRecoveryConfig returns sensible defaults for subagent recovery.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		Timeout:        2 * time.Minute,
		MaxLLMRetries:  2,
		BaseDelay:      500 * time.Millisecond,
		MaxDelay:       30 * time.Second,
		RetryableCheck: defaultRetryableCheck,
	}
}

// isRetryable reports whether err should be retried according to the config.
func (c *RecoveryConfig) isRetryable(err error) bool {
	if c.RetryableCheck == nil {
		return true
	}
	return c.RetryableCheck(err)
}

// defaultRetryableCheck classifies errors as retryable based on known
// transient signal strings (rate limits, server errors, timeouts).
// Non-transient errors like auth failures are not retried.
func defaultRetryableCheck(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, signal := range []string{
		"rate limit", "429", "500", "502", "503", "504",
		"timeout", "temporary", "connection reset",
		"connection refused", "eof",
	} {
		if strings.Contains(msg, signal) {
			return true
		}
	}
	return false
}

// WithRecovery wraps a subagent execution with timeout and cancellation
// support. The function fn runs in a separate goroutine so that context
// cancellation or timeout is detected via select even if fn blocks.
//
// The result channel is buffered(1) so the goroutine can complete its
// send even if WithRecovery has already returned via ctx.Done(). The
// goroutine itself terminates when fn observes the canceled context.
func WithRecovery(
	parentCtx context.Context,
	cfg RecoveryConfig,
	fn func(ctx context.Context) valueobject.SubAgentResult,
) valueobject.SubAgentResult {
	var ctx context.Context
	var cancel context.CancelFunc

	if cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parentCtx, cfg.Timeout)
	} else {
		ctx, cancel = context.WithCancel(parentCtx)
	}
	defer cancel()

	resultCh := make(chan valueobject.SubAgentResult, 1)
	go func() {
		resultCh <- fn(ctx)
	}()

	select {
	case result := <-resultCh:
		return result
	case <-ctx.Done():
		return valueobject.SubAgentResult{
			Status: valueobject.SubAgentStatusCanceled,
			Err:    fmt.Errorf("subagent execution canceled: %w", ctx.Err()),
		}
	}
}

// RetryLLMCall retries a single LLM call with exponential backoff and
// jitter. It respects context cancellation between retry attempts and
// returns immediately for non-retryable errors.
func RetryLLMCall[T any](
	ctx context.Context,
	cfg RecoveryConfig,
	fn func(ctx context.Context) (T, error),
) (T, error) {
	var zero T

	maxAttempts := cfg.MaxLLMRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		if attempt == maxAttempts-1 || !cfg.isRetryable(err) {
			return zero, fmt.Errorf("failed after %d attempt(s): %w", attempt+1, err)
		}

		delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt)))
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		maxJitter := int64(delay/2) + 1
		n, randErr := rand.Int(rand.Reader, big.NewInt(maxJitter))
		if randErr != nil {
			n = big.NewInt(0)
		}
		jitter := time.Duration(n.Int64())

		timer := time.NewTimer(delay + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}

	return zero, fmt.Errorf("retry: unreachable")
}
