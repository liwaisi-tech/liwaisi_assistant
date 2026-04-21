package fanout

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
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

func TestDetectSandbox_PrefersBwrap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap preference is Linux-only (REQ-1001)")
	}
	dir := t.TempDir()
	writeFakeBin(t, dir, "bwrap")
	writeFakeBin(t, dir, "firejail")
	stubPATH(t, dir)

	s, err := DetectSandbox()
	if err != nil {
		t.Fatalf("DetectSandbox: %v", err)
	}
	if s.Tool != ToolBwrap {
		t.Fatalf("tool = %q; want bwrap", s.Tool)
	}
}

func TestDetectSandbox_FallsBackToFirejail(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "firejail")
	stubPATH(t, dir)

	s, err := DetectSandbox()
	if err != nil {
		t.Fatalf("DetectSandbox: %v", err)
	}
	if s.Tool != ToolFirejail {
		t.Fatalf("tool = %q; want firejail", s.Tool)
	}
}

func TestDetectSandbox_NoneInstalled(t *testing.T) {
	dir := t.TempDir()
	stubPATH(t, dir)

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

func TestSandbox_IsZero(t *testing.T) {
	if !(Sandbox{}).IsZero() {
		t.Fatal("zero Sandbox must be IsZero")
	}
	if (Sandbox{Tool: "bwrap"}).IsZero() {
		t.Fatal("populated Sandbox must not be IsZero")
	}
}
