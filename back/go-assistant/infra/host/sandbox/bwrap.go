// Package sandbox wraps ExecRequests with bwrap or firejail arguments
// per the sandbox profile requested by the policy. The wrapper is a pure
// slice transformation; it does NOT invoke the child process itself —
// callers pipe the wrapped ExecRequest through HostAdapter.Exec as usual.
//
// Spec: spec-architecture-host-gate-security-policy.md §3 REQ-020/021.
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ErrSandboxUnavailable is returned when a profile demands a sandbox
// runtime (bwrap or firejail) and neither is present in $PATH.
var ErrSandboxUnavailable = errors.New("sandbox: neither bwrap nor firejail is available")

// Capability is an interface the wrapper consults to learn whether a
// runtime is installed. Production callers pass NewDefaultCapability();
// tests may inject an in-memory fake to exercise the fallback branch.
type Capability interface {
	Available(runtime string) bool
}

// NewDefaultCapability returns a Capability backed by exec.LookPath.
func NewDefaultCapability() Capability { return &defaultCapability{cache: map[string]bool{}} }

type defaultCapability struct {
	cache map[string]bool
}

func (d *defaultCapability) Available(runtime string) bool {
	if v, ok := d.cache[runtime]; ok {
		return v
	}
	_, err := exec.LookPath(runtime)
	ok := err == nil
	d.cache[runtime] = ok
	return ok
}

// Wrapper translates ExecRequests per profile.
type Wrapper struct {
	Cap Capability

	// JailRoot is the bind target for the fsjail profile (REQ-020).
	// Defaults to $HOME/.local/brae/work.
	JailRoot string
}

// New returns a Wrapper with sensible defaults.
func New(capability Capability) *Wrapper {
	if capability == nil {
		capability = NewDefaultCapability()
	}
	home, _ := os.UserHomeDir()
	return &Wrapper{
		Cap:      capability,
		JailRoot: filepath.Join(home, ".local", "brae", "work"),
	}
}

// Wrap returns a new ExecRequest whose Command/Args are prefixed with
// the sandbox runtime's argv. When profile == none or "", req is
// returned unchanged. Returns ErrSandboxUnavailable when profile
// requires a runtime and neither bwrap nor firejail are installed.
func (w *Wrapper) Wrap(req cpn.ExecRequest, profile cpn.SandboxProfile) (cpn.ExecRequest, error) {
	if profile == "" || profile == cpn.SandboxNone {
		return req, nil
	}
	if w.Cap.Available("bwrap") {
		return w.wrapBwrap(req, profile), nil
	}
	if w.Cap.Available("firejail") {
		return w.wrapFirejail(req, profile), nil
	}
	return cpn.ExecRequest{}, ErrSandboxUnavailable
}

// Available returns true when at least one sandbox runtime is installed.
func (w *Wrapper) Available() bool {
	return w.Cap.Available("bwrap") || w.Cap.Available("firejail")
}

// wrapBwrap builds the argv prefix per REQ-020.
func (w *Wrapper) wrapBwrap(req cpn.ExecRequest, profile cpn.SandboxProfile) cpn.ExecRequest {
	base := []string{
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
		"--unshare-all",
	}
	switch profile {
	case cpn.SandboxReadonly:
		base = append(base, "--share-net")
	case cpn.SandboxFSJail:
		base = append(base,
			"--share-net",
			"--bind", w.JailRoot, "/work",
			"--chdir", "/work",
		)
	case cpn.SandboxNetworkOff:
		base = append(base, "--unshare-net")
	}
	// Preserve env forwarding: bwrap inherits parent env by default; we
	// merely ensure PATH propagates even in --unshare-all mode.
	argv := make([]string, 0, len(base)+2+len(req.Args))
	argv = append(argv, base...)
	argv = append(argv, "--", req.Command)
	argv = append(argv, req.Args...)

	out := req
	out.Command = "bwrap"
	out.Args = argv
	return out
}

// wrapFirejail builds the argv prefix for the fallback runtime.
func (w *Wrapper) wrapFirejail(req cpn.ExecRequest, profile cpn.SandboxProfile) cpn.ExecRequest {
	args := []string{"--quiet", "--private-tmp", "--private-dev"}
	switch profile {
	case cpn.SandboxReadonly:
		args = append(args, "--read-only=/")
	case cpn.SandboxFSJail:
		args = append(args,
			"--read-only=/",
			fmt.Sprintf("--private=%s", w.JailRoot),
		)
	case cpn.SandboxNetworkOff:
		args = append(args, "--read-only=/", "--net=none")
	}
	args = append(args, "--", req.Command)
	args = append(args, req.Args...)

	out := req
	out.Command = "firejail"
	out.Args = args
	return out
}
