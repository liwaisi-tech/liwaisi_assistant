package openrouter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithRetry_SucceedsFirstAttempt(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		BaseDelay:       1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		RetryableStatus: []int{429, 502},
	}

	var calls int
	result, err := WithRetry(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithRetry_RetriesOnRetryableStatus(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		BaseDelay:       1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		RetryableStatus: []int{429, 502},
	}

	var calls int
	result, err := WithRetry(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", &retryableError{statusCode: 429, err: errors.New("rate limited")}
		}
		return "recovered", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("result = %q, want %q", result, "recovered")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestWithRetry_NoRetryOnNonRetryableStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"400 bad request", 400},
		{"401 unauthorized", 401},
		{"403 forbidden", 403},
		{"404 not found", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RetryConfig{
				MaxRetries:      3,
				BaseDelay:       1 * time.Millisecond,
				MaxDelay:        10 * time.Millisecond,
				RetryableStatus: []int{429, 502, 503, 529},
			}

			var calls int
			_, err := WithRetry(context.Background(), cfg, func(_ context.Context) (string, error) {
				calls++
				return "", &retryableError{statusCode: tt.status, err: errors.New("error")}
			})

			if err == nil {
				t.Fatal("expected error")
			}
			if calls != 1 {
				t.Errorf("calls = %d, want 1 (no retry for status %d)", calls, tt.status)
			}
		})
	}
}

func TestWithRetry_NoRetryOnPlainError(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		BaseDelay:       1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		RetryableStatus: []int{429},
	}

	var calls int
	_, err := WithRetry(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", errors.New("plain error")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithRetry_RespectsMaxRetries(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      2,
		BaseDelay:       1 * time.Millisecond,
		MaxDelay:        10 * time.Millisecond,
		RetryableStatus: []int{502},
	}

	var calls int
	_, err := WithRetry(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", &retryableError{statusCode: 502, err: errors.New("bad gateway")}
	})

	if err == nil {
		t.Fatal("expected error after max retries")
	}
	// initial attempt + MaxRetries retries = MaxRetries + 1
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", calls)
	}
}

func TestWithRetry_RespectsContextCancellation(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      10,
		BaseDelay:       100 * time.Millisecond,
		MaxDelay:        1 * time.Second,
		RetryableStatus: []int{502},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := WithRetry(ctx, cfg, func(_ context.Context) (string, error) {
		calls++
		return "", &retryableError{statusCode: 502, err: errors.New("bad gateway")}
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestBackoffDelay(t *testing.T) {
	base := 100 * time.Millisecond
	max := 5 * time.Second

	for attempt := 0; attempt < 10; attempt++ {
		d := backoffDelay(attempt, base, max)
		if d > max {
			t.Errorf("attempt %d: delay %v exceeds max %v", attempt, d, max)
		}
		if d < 0 {
			t.Errorf("attempt %d: delay %v is negative", attempt, d)
		}
	}
}
