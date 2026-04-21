package fanout

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ToolBwrap is the Linux-preferred sandbox CLI (REQ-1001).
const ToolBwrap = "bwrap"

// ToolFirejail is the fallback sandbox CLI (REQ-1002).
const ToolFirejail = "firejail"

// ToolContainer is the third-tier sandbox: the container itself provides
// SEC-004's network+fs+proc isolation when neither bwrap nor firejail can
// create namespaces inside Docker/Podman/Kubernetes (REQ-1005).
const ToolContainer = "container"

// ErrSandboxMissing is returned by DetectSandbox when no wrapper can create
// namespaces AND no container runtime is detected (REQ-1003). The awakening
// flow surfaces this as error_class="sandbox_missing".
var ErrSandboxMissing = errors.New("fanout: no sandbox wrapper available (need bwrap, firejail, or a container runtime)")

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
// to the host adapter.
//
// For Tool=="container" (or the zero value) Wrap is a no-op: the container
// itself provides SEC-004 isolation; no nested wrapping needed.
func (s Sandbox) Wrap(cmd string, args []string) (string, []string) {
	if s.Tool == "" || s.Tool == ToolContainer {
		return cmd, args
	}
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

// smokeTestFn runs the tool's namespace smoke test and returns nil on
// success. Indirected for tests so the suite does not shell out.
var smokeTestFn = runSmokeTest

// containerDetectFn exposes the container-detection pipeline behind a test
// seam so the suite can inject a fake filesystem + env.
var containerDetectFn = func() (string, bool) {
	return detectContainerRuntime(osFileStat, os.Getenv, osReadFile)
}

// DetectSandbox probes bwrap first (Linux-preferred per REQ-1001) then
// firejail (REQ-1002) then falls back to the container tier (REQ-1005).
// Returns ErrSandboxMissing when no wrapper can create namespaces AND no
// container runtime is detected.
func DetectSandbox() (Sandbox, error) {
	if runtime.GOOS == "linux" {
		if s, ok := tryDetectHealthy(ToolBwrap); ok {
			return s, nil
		}
		if s, ok := tryDetectHealthy(ToolFirejail); ok {
			return s, nil
		}
	} else {
		if s, ok := tryDetectHealthy(ToolFirejail); ok {
			return s, nil
		}
		if s, ok := tryDetectHealthy(ToolBwrap); ok {
			return s, nil
		}
	}
	if rt, ok := containerDetectFn(); ok {
		return Sandbox{Tool: ToolContainer, Version: rt, Argv: nil}, nil
	}
	return Sandbox{}, ErrSandboxMissing
}

func tryDetectHealthy(tool string) (Sandbox, bool) {
	if _, err := exec.LookPath(tool); err != nil {
		return Sandbox{}, false
	}
	if readVersion(tool) == "unknown" {
		return Sandbox{}, false
	}
	if err := smokeTestFn(tool); err != nil {
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

// runSmokeTest shells out with the wrapper's minimum-namespace argv to
// confirm the kernel actually allows it (Docker default seccomp blocks
// CLONE_NEWUSER / CLONE_NEWNET without CAP_SYS_ADMIN).
func runSmokeTest(tool string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var argv []string
	switch tool {
	case ToolBwrap:
		argv = []string{"--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/tmp", "--unshare-net", "/bin/true"}
	case ToolFirejail:
		argv = []string{"--noprofile", "--quiet", "/bin/true"}
	default:
		return errors.New("fanout: no smoke test for tool " + tool)
	}
	return exec.CommandContext(ctx, tool, argv...).Run()
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

// statFunc / envFunc / readFileFunc are test seams for detectContainerRuntime.
type statFunc func(string) (os.FileInfo, error)
type envFunc func(string) string
type readFileFunc func(string) ([]byte, error)

func osFileStat(p string) (os.FileInfo, error) { return os.Stat(p) }
func osReadFile(p string) ([]byte, error)      { return os.ReadFile(p) }

// detectContainerRuntime reports the container runtime the process is
// running inside, if any. The heuristic checks, in order:
//
//  1. /.dockerenv                    -> "docker"
//  2. /run/.containerenv             -> "podman"
//  3. $container env var             -> its value (lxc, systemd-nspawn, ...)
//  4. /proc/1/cgroup substring match -> docker|containerd|kubepods|buildah|crio
//
// Returns ("", false) on bare metal.
func detectContainerRuntime(stat statFunc, getenv envFunc, readFile readFileFunc) (string, bool) {
	if _, err := stat("/.dockerenv"); err == nil {
		return "docker", true
	}
	if _, err := stat("/run/.containerenv"); err == nil {
		return "podman", true
	}
	if v := strings.TrimSpace(getenv("container")); v != "" {
		return v, true
	}
	if b, err := readFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		for _, needle := range []string{"docker", "containerd", "kubepods", "buildah", "crio"} {
			if strings.Contains(s, needle) {
				return needle, true
			}
		}
	}
	return "", false
}
