package shellexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxTimeout = 300 * time.Second
	defaultMaxOutput  = 1 << 20 // 1 MB
	defaultShell      = "sh"
	killGracePeriod   = 500 * time.Millisecond
)

// ExecutorConfig holds configuration for the CommandExecutor.
type ExecutorConfig struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutputSize  int
	Shell          string
}

// CommandExecutor runs OS commands with safety controls including process group
// isolation, timeout enforcement, and output size limits.
type CommandExecutor struct {
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	maxOutputSize  int
	shell          string
}

// ExecutionResult holds the outcome of a command execution.
type ExecutionResult struct {
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	TimedOut bool          `json:"timed_out"`
	Duration time.Duration `json:"duration"`
}

// NewCommandExecutor creates a CommandExecutor with the given configuration.
// Zero-value fields in cfg are replaced with defaults.
func NewCommandExecutor(cfg ExecutorConfig) *CommandExecutor {
	e := &CommandExecutor{
		defaultTimeout: cfg.DefaultTimeout,
		maxTimeout:     cfg.MaxTimeout,
		maxOutputSize:  cfg.MaxOutputSize,
		shell:          cfg.Shell,
	}
	if e.defaultTimeout <= 0 {
		e.defaultTimeout = defaultTimeout
	}
	if e.maxTimeout <= 0 {
		e.maxTimeout = defaultMaxTimeout
	}
	if e.maxOutputSize <= 0 {
		e.maxOutputSize = defaultMaxOutput
	}
	if e.shell == "" {
		e.shell = defaultShell
	}
	return e
}

// DefaultTimeout returns the executor's default timeout.
func (e *CommandExecutor) DefaultTimeout() time.Duration {
	return e.defaultTimeout
}

// MaxTimeout returns the executor's maximum allowed timeout.
func (e *CommandExecutor) MaxTimeout() time.Duration {
	return e.maxTimeout
}

// Execute runs a command string via the system shell with process group
// isolation. It enforces a timeout and caps output size. The caller's context
// is also respected for cancellation.
func (e *CommandExecutor) Execute(ctx context.Context, command, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	if timeout <= 0 {
		timeout = e.defaultTimeout
	}
	if timeout > e.maxTimeout {
		timeout = e.maxTimeout
	}

	cmd := exec.Command(e.shell, "-c", command) //nolint:gosec // command validated by policy before reaching executor
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	start := time.Now()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
	}

	slog.Debug("command started", "command", command, "workDir", workDir, "timeout", timeout)

	// Set up combined deadline from context + explicit timeout.
	deadline := start.Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	var timedOut atomic.Bool
	doneCh := make(chan struct{})

	// Watchdog goroutine: kills the process group on timeout or context cancel.
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-doneCh:
			return
		case <-timer.C:
			timedOut.Store(true)
			killProcessGroup(cmd)
		case <-ctx.Done():
			killProcessGroup(cmd)
		}
	}()

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(&limitWriter{w: &stdoutBuf, n: int64(e.maxOutputSize)}, stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&limitWriter{w: &stderrBuf, n: int64(e.maxOutputSize)}, stderrPipe)
	}()

	wg.Wait()
	waitErr := cmd.Wait()
	close(doneCh)

	duration := time.Since(start)

	wasTimedOut := timedOut.Load()

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok { //nolint:errorlint // ExitError is a concrete type from os/exec
			exitCode = exitErr.ExitCode()
		} else if !wasTimedOut && ctx.Err() == nil {
			return nil, fmt.Errorf("waiting for command: %w", waitErr)
		}
	}

	slog.Debug("command finished", "command", command, "exitCode", exitCode, "timedOut", wasTimedOut, "duration", duration)

	stdout := truncateAndStrip(stdoutBuf.String(), e.maxOutputSize)
	stderr := truncateAndStrip(stderrBuf.String(), e.maxOutputSize)

	if wasTimedOut && exitCode == 0 {
		exitCode = -1
	}

	return &ExecutionResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		TimedOut: wasTimedOut,
		Duration: duration,
	}, nil
}

// killProcessGroup sends SIGTERM to the entire process group, waits briefly,
// then sends SIGKILL if still alive.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return
	}

	slog.Debug("killing process group", "pgid", pgid)
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(killGracePeriod)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// ansiEscapeRe matches ANSI escape sequences (CSI sequences and OSC sequences).
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\|\x1b\][^\x07]*\x07`)

// truncateAndStrip truncates the string to maxLen bytes and removes ANSI
// escape sequences.
func truncateAndStrip(s string, maxLen int) string {
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// limitWriter wraps an io.Writer to stop accepting data after n bytes.
type limitWriter struct {
	w io.Writer
	n int64
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.n <= 0 {
		return len(p), nil
	}
	toWrite := p
	if int64(len(p)) > lw.n {
		toWrite = p[:lw.n]
	}
	n, _ := lw.w.Write(toWrite)
	lw.n -= int64(n)
	// Always report len(p) so io.Copy keeps draining the pipe even after
	// the buffer limit is reached. Returning a short count would cause
	// io.ErrShortWrite, stalling the pipe and deadlocking the process.
	return len(p), nil
}

var _ io.Writer = (*limitWriter)(nil)
