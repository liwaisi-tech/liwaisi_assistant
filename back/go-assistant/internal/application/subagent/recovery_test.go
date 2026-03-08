package subagent

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- RetryLLMCall tests ---

func TestRetryLLMCall_NoError(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{MaxLLMRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	calls := 0

	result, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
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

func TestRetryLLMCall_TransientThenSuccess(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries:  3,
		BaseDelay:      time.Millisecond,
		MaxDelay:       10 * time.Millisecond,
		RetryableCheck: func(_ error) bool { return true },
	}
	calls := 0

	result, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("transient")
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

func TestRetryLLMCall_AllFail(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries:  2,
		BaseDelay:      time.Millisecond,
		MaxDelay:       10 * time.Millisecond,
		RetryableCheck: func(_ error) bool { return true },
	}
	calls := 0
	sentinel := errors.New("persistent failure")

	_, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", sentinel
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error should wrap sentinel; got: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", calls)
	}
	if !strings.Contains(err.Error(), "3 attempt(s)") {
		t.Errorf("error message should mention attempt count; got: %v", err)
	}
}

func TestRetryLLMCall_NonRetryable(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries:  5,
		BaseDelay:      time.Millisecond,
		MaxDelay:       time.Second,
		RetryableCheck: func(_ error) bool { return false },
	}
	calls := 0

	_, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", errors.New("non-retryable")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (should not retry non-retryable)", calls)
	}
}

func TestRetryLLMCall_ContextCancel(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries: 10,
		BaseDelay:     50 * time.Millisecond,
		MaxDelay:      time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	_, err := RetryLLMCall(ctx, cfg, func(_ context.Context) (string, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return "", errors.New("fail")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should be context.Canceled; got: %v", err)
	}
}

func TestRetryLLMCall_DelayCapped(t *testing.T) {
	t.Parallel()

	maxDelay := 5 * time.Millisecond
	cfg := RecoveryConfig{
		MaxLLMRetries: 10,
		BaseDelay:     time.Millisecond,
		MaxDelay:      maxDelay,
	}
	calls := 0

	start := time.Now()
	_, _ = RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", errors.New("fail")
	})
	elapsed := time.Since(start)

	// With 10 retries capped at 5ms + jitter (up to 2.5ms), total should be
	// well under 10 * (5+2.5)ms * 2 = 150ms with generous margin.
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed %v exceeds expected bound; delay capping may not work", elapsed)
	}
}

func TestRetryLLMCall_NilRetryableCheck(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries:  1,
		BaseDelay:      time.Millisecond,
		MaxDelay:       10 * time.Millisecond,
		RetryableCheck: nil,
	}
	calls := 0

	_, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
		calls++
		return "", errors.New("fail")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (nil RetryableCheck should retry all)", calls)
	}
}

func TestRetryLLMCall_NegativeRetries(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{
		MaxLLMRetries: -1,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
	}
	calls := 0

	result, err := RetryLLMCall(context.Background(), cfg, func(_ context.Context) (string, error) {
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
		t.Errorf("calls = %d, want 1 (should execute at least once)", calls)
	}
}

// --- WithRecovery tests ---

func TestWithRecovery_Success(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{Timeout: time.Second}

	result := WithRecovery(context.Background(), cfg, func(_ context.Context) valueobject.SubAgentResult {
		return valueobject.SubAgentResult{
			AgentName: "test",
			Output:    "done",
			Status:    valueobject.SubAgentStatusCompleted,
		}
	})

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "done" {
		t.Errorf("Output = %q, want %q", result.Output, "done")
	}
}

func TestWithRecovery_Timeout(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{Timeout: 20 * time.Millisecond}

	result := WithRecovery(context.Background(), cfg, func(ctx context.Context) valueobject.SubAgentResult {
		select {
		case <-ctx.Done():
			return valueobject.SubAgentResult{Status: valueobject.SubAgentStatusCanceled}
		case <-time.After(5 * time.Second):
			return valueobject.SubAgentResult{Status: valueobject.SubAgentStatusCompleted}
		}
	})

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil on timeout")
	}
}

func TestWithRecovery_ParentCancel(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{Timeout: 0}

	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result := WithRecovery(parentCtx, cfg, func(ctx context.Context) valueobject.SubAgentResult {
		<-ctx.Done()
		return valueobject.SubAgentResult{Status: valueobject.SubAgentStatusCanceled}
	})

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}
}

func TestWithRecovery_NoGoroutineLeak(t *testing.T) {
	cfg := RecoveryConfig{Timeout: time.Second}

	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		_ = WithRecovery(context.Background(), cfg, func(_ context.Context) valueobject.SubAgentResult {
			return valueobject.SubAgentResult{Status: valueobject.SubAgentStatusCompleted}
		})
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()
	// Allow a small margin for runtime goroutines.
	if after > before+5 {
		t.Errorf("goroutine leak: before=%d, after=%d", before, after)
	}
}

func TestWithRecovery_NoTimeout(t *testing.T) {
	t.Parallel()
	cfg := RecoveryConfig{Timeout: 0}

	result := WithRecovery(context.Background(), cfg, func(_ context.Context) valueobject.SubAgentResult {
		return valueobject.SubAgentResult{
			AgentName: "no-timeout",
			Output:    "completed",
			Status:    valueobject.SubAgentStatusCompleted,
		}
	})

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
}

// --- DefaultRecoveryConfig tests ---

func TestDefaultRecoveryConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultRecoveryConfig()

	if cfg.Timeout != 2*time.Minute {
		t.Errorf("Timeout = %v, want 2m", cfg.Timeout)
	}
	if cfg.MaxLLMRetries != 2 {
		t.Errorf("MaxLLMRetries = %d, want 2", cfg.MaxLLMRetries)
	}
	if cfg.BaseDelay != 500*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 500ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", cfg.MaxDelay)
	}
	if cfg.RetryableCheck == nil {
		t.Fatal("RetryableCheck should not be nil")
	}
	if !cfg.RetryableCheck(errors.New("rate limit exceeded")) {
		t.Error("should retry rate limit errors")
	}
	if !cfg.RetryableCheck(errors.New("status 429")) {
		t.Error("should retry 429 errors")
	}
	if !cfg.RetryableCheck(errors.New("status 502 bad gateway")) {
		t.Error("should retry 502 errors")
	}
	if !cfg.RetryableCheck(errors.New("connection timeout")) {
		t.Error("should retry timeout errors")
	}
	if cfg.RetryableCheck(errors.New("invalid auth token")) {
		t.Error("should not retry auth errors")
	}
	if cfg.RetryableCheck(errors.New("model not found")) {
		t.Error("should not retry model-not-found errors")
	}
}
