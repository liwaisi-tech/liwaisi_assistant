package fanout

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// stubPATH points PATH at dir for the duration of the test so exec.LookPath
// returns the fake binaries we drop there. Restored via t.Cleanup.
func stubPATH(t *testing.T, dir string) {
	t.Helper()
	prev := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
}

func writeFakeBin(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\necho " + name + " 1.2.3\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func stubSmokeTest(t *testing.T, healthy map[string]bool) {
	t.Helper()
	prev := smokeTestFn
	smokeTestFn = func(tool string) error {
		if healthy[tool] {
			return nil
		}
		return errors.New("namespace smoke test failed: " + tool)
	}
	t.Cleanup(func() { smokeTestFn = prev })
}

func stubContainerDetect(t *testing.T, rt string, ok bool) {
	t.Helper()
	prev := containerDetectFn
	containerDetectFn = func() (string, bool) { return rt, ok }
	t.Cleanup(func() { containerDetectFn = prev })
}

func TestDetectSandbox_PrefersBwrap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap preference is Linux-only (REQ-1001)")
	}
	dir := t.TempDir()
	writeFakeBin(t, dir, "bwrap")
	writeFakeBin(t, dir, "firejail")
	stubPATH(t, dir)
	stubSmokeTest(t, map[string]bool{ToolBwrap: true, ToolFirejail: true})
	stubContainerDetect(t, "", false)

	s, err := DetectSandbox()
	if err != nil {
		t.Fatalf("DetectSandbox: %v", err)
	}
	if s.Tool != ToolBwrap {
		t.Fatalf("tool = %q; want bwrap", s.Tool)
	}
}

func TestDetectSandbox_FallsBackToFirejail_WhenBwrapCantCreateNamespaces(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only order")
	}
	dir := t.TempDir()
	writeFakeBin(t, dir, "bwrap")
	writeFakeBin(t, dir, "firejail")
	stubPATH(t, dir)
	stubSmokeTest(t, map[string]bool{ToolBwrap: false, ToolFirejail: true})
	stubContainerDetect(t, "", false)

	s, err := DetectSandbox()
	if err != nil {
		t.Fatalf("DetectSandbox: %v", err)
	}
	if s.Tool != ToolFirejail {
		t.Fatalf("tool = %q; want firejail", s.Tool)
	}
}

func TestDetectSandbox_FallsBackToContainer_WhenNoWrapperWorks(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "bwrap")
	writeFakeBin(t, dir, "firejail")
	stubPATH(t, dir)
	stubSmokeTest(t, map[string]bool{})
	stubContainerDetect(t, "docker", true)

	s, err := DetectSandbox()
	if err != nil {
		t.Fatalf("DetectSandbox: %v", err)
	}
	if s.Tool != ToolContainer {
		t.Fatalf("tool = %q; want container", s.Tool)
	}
	if s.Version != "docker" {
		t.Fatalf("version = %q; want docker", s.Version)
	}
	if s.Argv != nil {
		t.Fatalf("argv = %#v; want nil", s.Argv)
	}
}

func TestDetectSandbox_ErrSandboxMissing_NoContainer_NoWrappers(t *testing.T) {
	dir := t.TempDir()
	stubPATH(t, dir)
	stubSmokeTest(t, map[string]bool{})
	stubContainerDetect(t, "", false)

	_, err := DetectSandbox()
	if !errors.Is(err, ErrSandboxMissing) {
		t.Fatalf("err = %v; want ErrSandboxMissing", err)
	}
}

func TestSandbox_Wrap_Bwrap(t *testing.T) {
	s := Sandbox{Tool: ToolBwrap, Argv: argvFor(ToolBwrap)}
	tool, argv := s.Wrap("sh", []string{"-c", "uname -r"})
	if tool != "bwrap" {
		t.Fatalf("tool = %q", tool)
	}
	want := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/tmp", "--unshare-net", "sh", "-c", "uname -r"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv =\n  %#v\nwant\n  %#v", argv, want)
	}
}

func TestSandbox_Wrap_Firejail(t *testing.T) {
	s := Sandbox{Tool: ToolFirejail, Argv: argvFor(ToolFirejail)}
	tool, argv := s.Wrap("sh", []string{"-c", "uname -r"})
	if tool != "firejail" {
		t.Fatalf("tool = %q", tool)
	}
	want := []string{"--net=none", "--private-tmp", "--read-only=/", "sh", "-c", "uname -r"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv =\n  %#v\nwant\n  %#v", argv, want)
	}
}

func TestSandbox_Wrap_Container_IsNoOp(t *testing.T) {
	s := Sandbox{Tool: ToolContainer, Version: "docker"}
	tool, argv := s.Wrap("uname", []string{"-r"})
	if tool != "uname" {
		t.Fatalf("tool = %q; want uname (no wrapping)", tool)
	}
	if !reflect.DeepEqual(argv, []string{"-r"}) {
		t.Fatalf("argv = %#v; want [-r]", argv)
	}
}

func TestSandbox_IsZero(t *testing.T) {
	if !(Sandbox{}).IsZero() {
		t.Fatal("zero Sandbox must be IsZero")
	}
	if (Sandbox{Tool: "bwrap"}).IsZero() {
		t.Fatal("populated Sandbox must not be IsZero")
	}
}

// ---- detectContainerRuntime -----------------------------------------------

type fakeInfo struct{ name string }

func (f fakeInfo) Name() string       { return f.name }
func (fakeInfo) Size() int64          { return 0 }
func (fakeInfo) Mode() fs.FileMode    { return 0 }
func (fakeInfo) ModTime() (t time.Time) { return }
func (fakeInfo) IsDir() bool          { return false }
func (fakeInfo) Sys() any             { return nil }

func statWith(present map[string]bool) statFunc {
	return func(p string) (os.FileInfo, error) {
		if present[p] {
			return fakeInfo{name: filepath.Base(p)}, nil
		}
		return nil, fs.ErrNotExist
	}
}

func envWith(m map[string]string) envFunc {
	return func(k string) string { return m[k] }
}

func readWith(m map[string]string) readFileFunc {
	return func(p string) ([]byte, error) {
		if v, ok := m[p]; ok {
			return []byte(v), nil
		}
		return nil, fs.ErrNotExist
	}
}

func TestDetectContainerRuntime_Docker(t *testing.T) {
	rt, ok := detectContainerRuntime(
		statWith(map[string]bool{"/.dockerenv": true}),
		envWith(nil),
		readWith(nil),
	)
	if !ok || rt != "docker" {
		t.Fatalf("got (%q,%v); want (docker,true)", rt, ok)
	}
}

func TestDetectContainerRuntime_Podman(t *testing.T) {
	rt, ok := detectContainerRuntime(
		statWith(map[string]bool{"/run/.containerenv": true}),
		envWith(nil),
		readWith(nil),
	)
	if !ok || rt != "podman" {
		t.Fatalf("got (%q,%v); want (podman,true)", rt, ok)
	}
}

func TestDetectContainerRuntime_CgroupMatches(t *testing.T) {
	rt, ok := detectContainerRuntime(
		statWith(nil),
		envWith(nil),
		readWith(map[string]string{
			"/proc/1/cgroup": "12:pids:/kubepods/besteffort/pod-abc/containerd-xyz\n",
		}),
	)
	if !ok {
		t.Fatalf("got (%q,%v); want ok=true", rt, ok)
	}
	if rt != "containerd" {
		t.Fatalf("got rt=%q; want containerd (first needle hit)", rt)
	}
}

func TestDetectContainerRuntime_ContainerEnvVar(t *testing.T) {
	rt, ok := detectContainerRuntime(
		statWith(nil),
		envWith(map[string]string{"container": "lxc"}),
		readWith(nil),
	)
	if !ok || rt != "lxc" {
		t.Fatalf("got (%q,%v); want (lxc,true)", rt, ok)
	}
}

func TestDetectContainerRuntime_BareMetal_ReturnsFalse(t *testing.T) {
	rt, ok := detectContainerRuntime(
		statWith(nil),
		envWith(nil),
		readWith(map[string]string{"/proc/1/cgroup": "0::/user.slice/user-1000.slice\n"}),
	)
	if ok {
		t.Fatalf("got (%q,true); want bare metal (false)", rt)
	}
}
