package shellexec

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestExecutor() *CommandExecutor {
	return NewCommandExecutor(ExecutorConfig{
		DefaultTimeout: 10 * time.Second,
		MaxTimeout:     30 * time.Second,
		MaxOutputSize:  1 << 20,
	})
}

func TestCommandExecutor_Execute(t *testing.T) {
	workDir := t.TempDir()

	tests := []struct {
		name    string
		command string
		timeout time.Duration
		check   func(t *testing.T, r *ExecutionResult)
		wantErr bool
	}{
		{
			name:    "successful echo",
			command: "echo hello",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
				if got := strings.TrimSpace(r.Stdout); got != "hello" {
					t.Errorf("stdout = %q, want %q", got, "hello")
				}
				if r.TimedOut {
					t.Error("timed_out should be false")
				}
				if r.Duration <= 0 {
					t.Error("duration should be positive")
				}
			},
		},
		{
			name:    "non-zero exit code",
			command: "exit 42",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if r.ExitCode != 42 {
					t.Errorf("exit_code = %d, want 42", r.ExitCode)
				}
			},
		},
		{
			name:    "stdout and stderr separation",
			command: `echo out; echo err >&2`,
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if got := strings.TrimSpace(r.Stdout); got != "out" {
					t.Errorf("stdout = %q, want %q", got, "out")
				}
				if got := strings.TrimSpace(r.Stderr); got != "err" {
					t.Errorf("stderr = %q, want %q", got, "err")
				}
			},
		},
		{
			name:    "pipes work",
			command: "echo hello world | wc -w",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
				if got := strings.TrimSpace(r.Stdout); got != "2" {
					t.Errorf("stdout = %q, want %q", got, "2")
				}
			},
		},
		{
			name:    "environment variables",
			command: "MY_TEST_VAR=hello sh -c 'echo $MY_TEST_VAR'",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if got := strings.TrimSpace(r.Stdout); got != "hello" {
					t.Errorf("stdout = %q, want %q", got, "hello")
				}
			},
		},
		{
			name:    "redirects work",
			command: `echo out; echo err >&2 2>&1`,
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
			},
		},
		{
			name:    "command chaining with &&",
			command: "echo first && echo second",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				if !strings.Contains(r.Stdout, "first") || !strings.Contains(r.Stdout, "second") {
					t.Errorf("stdout = %q, want both 'first' and 'second'", r.Stdout)
				}
			},
		},
		{
			name:    "working directory is respected",
			command: "pwd",
			check: func(t *testing.T, r *ExecutionResult) {
				t.Helper()
				got := strings.TrimSpace(r.Stdout)
				if got == "" {
					t.Error("stdout is empty, expected working directory path")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := newTestExecutor()
			result, err := exec.Execute(context.Background(), tt.command, workDir, tt.timeout)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestCommandExecutor_Timeout(t *testing.T) {
	exec := NewCommandExecutor(ExecutorConfig{
		DefaultTimeout: 1 * time.Second,
		MaxTimeout:     5 * time.Second,
	})
	workDir := t.TempDir()

	result, err := exec.Execute(context.Background(), "sleep 60", workDir, 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TimedOut {
		t.Error("timed_out should be true")
	}
	if result.ExitCode == 0 {
		t.Error("exit_code should be non-zero on timeout")
	}
}

func TestCommandExecutor_ContextCancellation(t *testing.T) {
	exec := newTestExecutor()
	workDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	result, err := exec.Execute(ctx, "sleep 60", workDir, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("exit_code should be non-zero after cancellation")
	}
}

func TestCommandExecutor_OutputTruncation(t *testing.T) {
	maxSize := 256
	exec := NewCommandExecutor(ExecutorConfig{
		MaxOutputSize: maxSize,
	})
	workDir := t.TempDir()

	result, err := exec.Execute(context.Background(), "head -c 1024 /dev/urandom | base64", workDir, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Stdout) > maxSize {
		t.Errorf("stdout length = %d, want <= %d", len(result.Stdout), maxSize)
	}
}

func TestCommandExecutor_ANSIStripping(t *testing.T) {
	exec := newTestExecutor()
	workDir := t.TempDir()

	result, err := exec.Execute(context.Background(), `printf '\033[31mred\033[0m text'`, workDir, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.Stdout, "\033[") {
		t.Errorf("stdout still contains ANSI escapes: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "red") || !strings.Contains(result.Stdout, "text") {
		t.Errorf("stdout = %q, want to contain 'red' and 'text'", result.Stdout)
	}
}

func TestCommandExecutor_DefaultTimeout(t *testing.T) {
	exec := NewCommandExecutor(ExecutorConfig{
		DefaultTimeout: 5 * time.Second,
		MaxTimeout:     10 * time.Second,
	})

	if exec.DefaultTimeout() != 5*time.Second {
		t.Errorf("DefaultTimeout() = %v, want 5s", exec.DefaultTimeout())
	}
	if exec.MaxTimeout() != 10*time.Second {
		t.Errorf("MaxTimeout() = %v, want 10s", exec.MaxTimeout())
	}
}

func TestCommandExecutor_DefaultConfig(t *testing.T) {
	exec := NewCommandExecutor(ExecutorConfig{})

	if exec.defaultTimeout != defaultTimeout {
		t.Errorf("defaultTimeout = %v, want %v", exec.defaultTimeout, defaultTimeout)
	}
	if exec.maxTimeout != defaultMaxTimeout {
		t.Errorf("maxTimeout = %v, want %v", exec.maxTimeout, defaultMaxTimeout)
	}
	if exec.maxOutputSize != defaultMaxOutput {
		t.Errorf("maxOutputSize = %d, want %d", exec.maxOutputSize, defaultMaxOutput)
	}
	if exec.shell != defaultShell {
		t.Errorf("shell = %q, want %q", exec.shell, defaultShell)
	}
}

func TestCommandExecutor_ConcurrentExecution(t *testing.T) {
	exec := newTestExecutor()
	workDir := t.TempDir()

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			result, err := exec.Execute(context.Background(), "echo concurrent", workDir, 5*time.Second)
			if err != nil {
				errs <- err
				return
			}
			if result.ExitCode != 0 {
				errs <- fmt.Errorf("exit_code = %d, want 0", result.ExitCode)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent execution error: %v", err)
		}
	}
}

func TestTruncateAndStrip(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "no truncation no ANSI",
			input:  "hello world",
			maxLen: 100,
			want:   "hello world",
		},
		{
			name:   "truncation applied",
			input:  "abcdefghij",
			maxLen: 5,
			want:   "abcde",
		},
		{
			name:   "ANSI stripped",
			input:  "\033[31mred\033[0m text",
			maxLen: 100,
			want:   "red text",
		},
		{
			name:   "both truncation and ANSI",
			input:  "\033[31mred\033[0m",
			maxLen: 20,
			want:   "red",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 100,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateAndStrip(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateAndStrip(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestCommandExecutor_TimeoutKillsProcessGroup(t *testing.T) {
	exec := NewCommandExecutor(ExecutorConfig{
		DefaultTimeout: 1 * time.Second,
		MaxTimeout:     5 * time.Second,
	})
	workDir := t.TempDir()

	result, err := exec.Execute(context.Background(), "sleep 60 & sleep 60 & wait", workDir, 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TimedOut {
		t.Error("timed_out should be true")
	}
}

func TestCommandExecutor_LargeOutputDoesNotHang(t *testing.T) {
	maxSize := 256
	exec := NewCommandExecutor(ExecutorConfig{
		MaxOutputSize:  maxSize,
		DefaultTimeout: 10 * time.Second,
		MaxTimeout:     10 * time.Second,
	})
	workDir := t.TempDir()

	// Produce ~2MB of output, well beyond the 256-byte cap.
	// Before the limitWriter fix, this would deadlock because io.Copy
	// returned ErrShortWrite and stopped draining the pipe.
	result, err := exec.Execute(context.Background(), "yes | head -c 2097152", workDir, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TimedOut {
		t.Fatal("command timed out — limitWriter likely stalled the pipe drain")
	}
	if len(result.Stdout) > maxSize {
		t.Errorf("stdout length = %d, want <= %d", len(result.Stdout), maxSize)
	}
}

func TestCommandExecutor_DurationTracking(t *testing.T) {
	exec := newTestExecutor()
	workDir := t.TempDir()

	result, err := exec.Execute(context.Background(), "sleep 0.1", workDir, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duration < 50*time.Millisecond {
		t.Errorf("duration = %v, expected >= 50ms", result.Duration)
	}
	if result.Duration > 5*time.Second {
		t.Errorf("duration = %v, expected < 5s", result.Duration)
	}
}
