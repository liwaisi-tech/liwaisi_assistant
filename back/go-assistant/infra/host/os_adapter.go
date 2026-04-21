// Package host is the infrastructure adapter implementing the cpn.HostAdapter,
// cpn.BashSessionManager, and cpn.HostGate ports declared in back/go-assistant/cpn/host.go.
//
// v1 (GAP-1) is deliberately minimal: one-shot command execution via os/exec
// plus PTY sessions via github.com/creack/pty. Policy enforcement beyond the
// belt-and-braces SEC-001/SEC-002 checks is deferred to GAP-6.
package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// DefaultMaxOutputBytes caps stdout/stderr capture at 1 MiB (spec §9 "huge
// stdout"). Callers needing full streaming should use SpawnPTY instead.
const DefaultMaxOutputBytes = 1 << 20

// DefaultDenyList lists command lines that are rejected unconditionally
// regardless of the gate result (SEC-001). Substring matching is
// case-sensitive and intentionally conservative; GAP-6 replaces this with
// a real policy engine.
var DefaultDenyList = []string{
	"rm -rf /",
	":(){ :|:& };:",
}

// pathJailSubdir is the suffix under $HOME that WriteFile/ReadFile are
// allowed to touch (SEC-002). v1 is unconditional; GAP-6 delegates to gate.
const pathJailSubdir = ".local/brae"

// OSHostAdapter is the production implementation of cpn.HostAdapter. It is
// safe for concurrent use.
type OSHostAdapter struct {
	MaxOutputBytes int
	DenyList       []string
	// AllowedRoot is the absolute directory WriteFile/ReadFile may write
	// inside of. Defaults to $HOME/.local/brae/ (SEC-002).
	AllowedRoot string

	Logger *slog.Logger

	// artefactLedger, when non-nil, records every successful WriteFile as
	// an authored-artefact row (GAP-10). Injected via the
	// WithArtefactLedger option so WriteFile stays backward-compatible
	// when no ledger is wired.
	artefactLedger persist.AuthoredArtefactLedger
}

// Option configures an OSHostAdapter at construction time.
type Option func(*OSHostAdapter)

// WithArtefactLedger injects an AuthoredArtefactLedger so every WriteFile
// records an artefact row. Pass nil to disable ledger recording — which is
// also the default when this option is not applied (backward compatibility
// with existing call sites).
func WithArtefactLedger(ledger persist.AuthoredArtefactLedger) Option {
	return func(a *OSHostAdapter) {
		a.artefactLedger = ledger
	}
}

// Compile-time interface check.
var _ cpn.HostAdapter = (*OSHostAdapter)(nil)

// NewOSHostAdapter constructs an OSHostAdapter with sane defaults. Options
// (e.g. WithArtefactLedger) are applied after the defaults so callers can
// override only the fields they care about.
func NewOSHostAdapter(logger *slog.Logger, opts ...Option) *OSHostAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	home, _ := os.UserHomeDir()
	allowed := filepath.Join(home, pathJailSubdir)
	a := &OSHostAdapter{
		MaxOutputBytes: DefaultMaxOutputBytes,
		DenyList:       append([]string(nil), DefaultDenyList...),
		AllowedRoot:    allowed,
		Logger:         logger,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// Exec runs the requested command and captures stdout/stderr up to
// MaxOutputBytes.
func (a *OSHostAdapter) Exec(ctx context.Context, req cpn.ExecRequest) (cpn.ExecResult, error) {
	if err := a.guard(req.Command, req.Args); err != nil {
		return cpn.ExecResult{}, err
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	//nolint:gosec // G204: command/args are caller-authorised via the host gate and policy layer before reaching this adapter
	cmd := exec.CommandContext(runCtx, req.Command, req.Args...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	if len(req.Env) > 0 {
		cmd.Env = append([]string(nil), req.Env...)
	}
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	stdout := &cappedBuffer{limit: a.MaxOutputBytes}
	stderr := &cappedBuffer{limit: a.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	result := cpn.ExecResult{
		Stdout:     stdout.Bytes(),
		Stderr:     stderr.Bytes(),
		DurationMs: duration.Milliseconds(),
		Truncated:  stdout.Truncated || stderr.Truncated,
	}

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return result, cpn.NewHostError(cpn.HostErrCodeTimeout,
				fmt.Sprintf("%s exceeded timeout %s", req.Command, req.Timeout), runErr)
		}
		// Detect command-not-found via os/exec.ErrNotFound (path resolution)
		// or ENOENT surfaced through an *exec.Error wrapper.
		if isCommandNotFound(runErr) {
			return result, cpn.NewHostError(cpn.HostErrCodeCommandNotFound,
				fmt.Sprintf("command not found: %s", req.Command), runErr)
		}
		if _, ok := runErr.(*exec.ExitError); ok {
			if req.AllowNonZero {
				return result, nil
			}
			return result, cpn.NewHostError(cpn.HostErrCodeNonZeroExit,
				fmt.Sprintf("%s exited with code %d", req.Command, result.ExitCode), runErr)
		}
		return result, runErr
	}

	if result.ExitCode != 0 && !req.AllowNonZero {
		return result, cpn.NewHostError(cpn.HostErrCodeNonZeroExit,
			fmt.Sprintf("%s exited with code %d", req.Command, result.ExitCode), nil)
	}
	return result, nil
}

// SpawnPTY is implemented by the session manager (GAP-1 keeps PTYs behind
// the BashSessionManager so callers do not juggle raw handles). Direct
// callers should go through BashSessionManager.Open instead. We still
// satisfy the interface to preserve the port shape for GAP-2.
func (a *OSHostAdapter) SpawnPTY(_ context.Context, _ cpn.PTYRequest) (cpn.PTYHandle, error) {
	return cpn.PTYHandle{}, errors.New("SpawnPTY: use BashSessionManager.Open (GAP-1 does not expose raw PTYs)")
}

// KillPID delivers sig to pid. The CPN domain signal values map to their
// POSIX equivalents.
func (a *OSHostAdapter) KillPID(_ context.Context, pid int, sig cpn.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(mapSignal(sig))
}

// ReadFile reads path after checking the jail (SEC-002). Reading outside the
// jail returns ErrPathDenied.
func (a *OSHostAdapter) ReadFile(_ context.Context, path string) ([]byte, error) {
	if err := a.checkPathJail(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteFile writes data to path after checking the jail (SEC-002). Writing
// outside the jail returns ErrPathDenied.
//
// When an AuthoredArtefactLedger is injected via WithArtefactLedger the
// write is recorded as an artefact: PreWrite runs before the disk touch so
// a policy-rejection surfaces as a failed write, and PostWrite runs after
// the bytes are persisted to record SHA-256 + size + MIME. PostWrite errors
// are logged but do not fail the write — the file is already on disk and
// retrying would be a duplicate.
func (a *OSHostAdapter) WriteFile(ctx context.Context, path string, data []byte, mode fs.FileMode) error {
	if err := a.checkPathJail(path); err != nil {
		return err
	}

	// Build an intent. When the caller did not attach one via
	// cpn.WithWriteIntent, synthesise a minimal intent so the ledger
	// still captures the classification and the file-level provenance
	// (path, size, hash). This keeps ad-hoc writes (e.g. from tests and
	// standalone CLI tools) observable without forcing every caller to
	// plumb an intent through every layer.
	intent, _ := persist.LoadWriteIntent(ctx, cpn.WriteIntentKey)
	if intent.Classification == "" {
		intent.Classification = persist.Classify(path, mode)
	}

	var artefactID string
	if a.artefactLedger != nil {
		id, err := a.artefactLedger.PreWrite(ctx, intent, path, uint32(mode))
		if err != nil {
			return fmt.Errorf("write_file: pre-write: %w", err)
		}
		artefactID = id
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}

	if a.artefactLedger != nil && artefactID != "" {
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		mime := detectMIME(data)
		if err := a.artefactLedger.PostWrite(ctx, artefactID, sha, int64(len(data)), mime); err != nil {
			a.Logger.Warn("write_file: post-write ledger update failed",
				slog.String("path", path),
				slog.String("artefact_id", artefactID),
				slog.Any("error", err),
			)
		}
	}
	return nil
}

// detectMIME returns the MIME type of the given payload by inspecting the
// first 512 bytes (same heuristic net/http uses for http.ServeContent).
func detectMIME(data []byte) string {
	n := 512
	if len(data) < n {
		n = len(data)
	}
	return http.DetectContentType(data[:n])
}

// Stat returns metadata for path after checking the jail.
func (a *OSHostAdapter) Stat(_ context.Context, path string) (cpn.FileInfo, error) {
	if err := a.checkPathJail(path); err != nil {
		return cpn.FileInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return cpn.FileInfo{}, err
	}
	return cpn.FileInfo{
		Path:    path,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// guard enforces SEC-001: path traversal and the deny list.
//
// Deny matching runs against a canonicalised command line: the command's
// basename plus each arg with whitespace collapsed to a single space. The
// previous form matched strings.Contains(command+" "+strings.Join(args, " "),
// deny) which was bypassable by padding args with extra whitespace
// (e.g. args=[" -rf", " /"] yielded "rm  -rf /" which does not contain the
// literal "rm -rf /"). Canonicalisation closes that seam while keeping the
// fork-bomb-style multi-token rules intact (SEC-FIX-007 / AC-007).
func (a *OSHostAdapter) guard(command string, args []string) error {
	candidates := append([]string{command}, args...)
	for _, c := range candidates {
		if containsDotDot(c) {
			return cpn.NewHostError(cpn.HostErrCodePathDenied,
				"command or arguments contain path traversal (..)", nil)
		}
	}

	canonCmd := filepath.Base(strings.TrimSpace(command))
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, canonCmd)
	for _, a := range args {
		// strings.Fields normalises any internal whitespace run to single
		// spaces; an empty/whitespace-only arg contributes nothing.
		parts = append(parts, strings.Fields(a)...)
	}
	canonLine := strings.Join(parts, " ")

	for _, deny := range a.DenyList {
		if deny == "" {
			continue
		}
		canonDeny := strings.Join(strings.Fields(deny), " ")
		if canonDeny == "" {
			continue
		}
		if strings.Contains(canonLine, canonDeny) {
			return cpn.NewHostError(cpn.HostErrCodeDenyList,
				fmt.Sprintf("command matches deny rule %q", deny), nil)
		}
	}
	return nil
}

// containsDotDot reports whether s contains a ".." path segment (either
// standalone, at the start, at the end, or surrounded by "/").
func containsDotDot(s string) bool {
	if s == ".." {
		return true
	}
	if strings.HasPrefix(s, "../") || strings.HasSuffix(s, "/..") {
		return true
	}
	return strings.Contains(s, "/../")
}

// checkPathJail enforces SEC-002: any path must resolve under AllowedRoot.
//
// SEC-FIX-002: symlinks are evaluated before the containment check so a
// link inside the jail pointing outside cannot exfiltrate/overwrite. For
// paths that do not exist yet (typical on WriteFile creating a new file)
// the parent directory is symlink-resolved instead so the write cannot
// traverse a hostile link that was placed in the parent.
func (a *OSHostAdapter) checkPathJail(path string) error {
	if a.AllowedRoot == "" {
		return cpn.NewHostError(cpn.HostErrCodePathDenied,
			"adapter has no allowed root configured", nil)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cpn.NewHostError(cpn.HostErrCodePathDenied, "invalid path", err)
	}

	// Resolve symlinks. When the leaf does not exist, climb until we find
	// an ancestor that does and resolve that — this covers the "create new
	// file under a symlink'd parent" case. If we walk all the way to "/"
	// without finding an existing ancestor, the path is unresolvable and
	// we deny conservatively.
	resolved, rerr := evalPathWithMissingLeaf(abs)
	if rerr != nil {
		return cpn.NewHostError(cpn.HostErrCodePathDenied,
			fmt.Sprintf("path %q could not be resolved: %v", path, rerr), rerr)
	}

	rootClean, err := filepath.EvalSymlinks(a.AllowedRoot)
	if err != nil {
		// AllowedRoot MUST exist and resolve. If it does not, deny — the
		// adapter is mis-configured and the safest behaviour is to refuse.
		return cpn.NewHostError(cpn.HostErrCodePathDenied,
			fmt.Sprintf("allowed root %q unresolvable: %v", a.AllowedRoot, err), err)
	}
	rootClean = filepath.Clean(rootClean)

	rel, err := filepath.Rel(rootClean, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return cpn.NewHostError(cpn.HostErrCodePathDenied,
			fmt.Sprintf("path %q is outside allowed root %q", path, a.AllowedRoot), nil)
	}
	return nil
}

// evalPathWithMissingLeaf resolves symlinks on the deepest existing prefix
// of path and joins the remaining (non-existent) tail back on. This lets
// WriteFile create new files inside the jail while still rejecting a
// hostile symlink anywhere on the resolved prefix.
func evalPathWithMissingLeaf(abs string) (string, error) {
	abs = filepath.Clean(abs)
	missing := ""
	cur := abs
	for {
		if _, err := filepath.EvalSymlinks(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			if missing == "" {
				return filepath.Clean(resolved), nil
			}
			return filepath.Clean(filepath.Join(resolved, missing)), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached root without finding an existing ancestor.
			return "", fmt.Errorf("no existing ancestor for %q", abs)
		}
		missing = filepath.Join(filepath.Base(cur), missing)
		cur = parent
	}
}

// cappedBuffer is an io.Writer that grows to at most limit bytes; subsequent
// writes are discarded and Truncated is set.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	Truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.limit <= 0 {
		return c.buf.Write(p)
	}
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		c.Truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	// Write what fits, discard the rest.
	if _, err := c.buf.Write(p[:remaining]); err != nil {
		return 0, err
	}
	c.Truncated = true
	return len(p), nil
}

// Bytes returns the captured bytes so far (not a copy — caller must treat it
// as read-only).
func (c *cappedBuffer) Bytes() []byte {
	return c.buf.Bytes()
}

// mapSignal converts a domain cpn.Signal to its os.Signal equivalent.
func mapSignal(s cpn.Signal) os.Signal {
	switch s {
	case cpn.SignalKill:
		return syscall.SIGKILL
	case cpn.SignalInt:
		return syscall.SIGINT
	default:
		return syscall.SIGTERM
	}
}

// isCommandNotFound reports whether err is "executable not found in $PATH"
// or "no such file or directory" when executing an absolute path.
func isCommandNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		if errors.Is(execErr.Err, exec.ErrNotFound) || errors.Is(execErr.Err, os.ErrNotExist) {
			return true
		}
	}
	// ENOENT from the underlying fork/exec shows up as a wrapped *os.PathError.
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errors.Is(pathErr.Err, syscall.ENOENT) {
			return true
		}
	}
	return false
}
