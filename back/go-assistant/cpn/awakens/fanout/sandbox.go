package fanout

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ToolBwrap is the Linux-preferred sandbox CLI (REQ-1001).
const ToolBwrap = "bwrap"

// ToolFirejail is the fallback sandbox CLI (REQ-1002).
const ToolFirejail = "firejail"

// ErrSandboxMissing is returned by DetectSandbox when neither bwrap nor
// firejail is resolvable on PATH (REQ-1003). The awakening flow surfaces
// this as error_class="sandbox_missing".
var ErrSandboxMissing = errors.New("fanout: no sandbox wrapper available (need bwrap or firejail)")

// Sandbox carries the resolved wrapper CLI + its sandbox argv prefix. The
// zero value represents "no wrapper selected" — executors MUST NOT run
// probes with a zero-value Sandbox.
type Sandbox struct {
	Tool    string
	Version string
	Argv    []string
}

// IsZero reports whether s has not been populated by DetectSandbox.
func (s Sandbox) IsZero() bool { return s.Tool == "" }

// Wrap returns the (wrapper command, full argv) pair that should be passed
// to the host adapter. The caller's original (cmd, args) becomes a trailing
// suffix after the wrapper's sandbox flags.
func (s Sandbox) Wrap(cmd string, args []string) (string, []string) {
	out := make([]string, 0, len(s.Argv)+1+len(args))
	out = append(out, s.Argv...)
	out = append(out, cmd)
	out = append(out, args...)
	return s.Tool, out
}

// DetectSandboxFn is the test-hook indirection used by every caller that
// needs a sandbox. Production code never reassigns it; tests swap it in
// place of invoking real exec.LookPath so they don't mutate the process
// environment (PATH) or require bwrap/firejail to exist on the builder.
var DetectSandboxFn = DetectSandbox

// DetectSandbox probes bwrap first (Linux-preferred per REQ-1001) then
// firejail (REQ-1002). Returns ErrSandboxMissing when neither is present.
func DetectSandbox() (Sandbox, error) {
	if runtime.GOOS == "linux" {
		if s, ok := tryDetect(ToolBwrap); ok {
			return s, nil
		}
		if s, ok := tryDetect(ToolFirejail); ok {
			return s, nil
		}
		return Sandbox{}, ErrSandboxMissing
	}
	if s, ok := tryDetect(ToolFirejail); ok {
		return s, nil
	}
	if s, ok := tryDetect(ToolBwrap); ok {
		return s, nil
	}
	return Sandbox{}, ErrSandboxMissing
}

func tryDetect(tool string) (Sandbox, bool) {
	if _, err := exec.LookPath(tool); err != nil {
		return Sandbox{}, false
	}
	return Sandbox{
		Tool:    tool,
		Version: readVersion(tool),
		Argv:    argvFor(tool),
	}, true
}

func argvFor(tool string) []string {
	switch tool {
	case ToolBwrap:
		return []string{"--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/tmp", "--unshare-net"}
	case ToolFirejail:
		return []string{"--net=none", "--private-tmp", "--read-only=/"}
	default:
		return nil
	}
}

// readVersion is best-effort; parse failures yield "unknown" rather than
// failing awakening (spec non-blocker).
func readVersion(tool string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, tool, "--version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	line := bytes.SplitN(out, []byte("\n"), 2)[0]
	v := strings.TrimSpace(string(line))
	if v == "" {
		return "unknown"
	}
	return v
}
