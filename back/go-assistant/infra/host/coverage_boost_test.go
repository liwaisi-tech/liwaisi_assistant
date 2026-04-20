//go:build linux

package host

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ──────────────────────────────────────────────────────────────────────────
// gate.go: AllowAllHostGate
// ──────────────────────────────────────────────────────────────────────────

func TestAllowAllHostGate_AlwaysAllows(t *testing.T) {
	g := NewAllowAllHostGate()
	if err := g.Check(context.Background(), cpn.GateOp{Kind: "exec", Command: "rm -rf /"}); err != nil {
		t.Fatalf("allow-all must not error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// os_adapter.go: SpawnPTY, KillPID, ReadFile, Stat, mapSignal,
// isCommandNotFound, cappedBuffer, containsDotDot, checkPathJail
// ──────────────────────────────────────────────────────────────────────────

func TestOSHostAdapter_SpawnPTY_ReturnsStubError(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.SpawnPTY(context.Background(), cpn.PTYRequest{Command: "bash"})
	if err == nil {
		t.Fatal("expected stub error")
	}
}

func TestOSHostAdapter_KillPID(t *testing.T) {
	// Spawn a long-running child via exec so we have a known PID.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	a := NewOSHostAdapter(nil)
	if err := a.KillPID(context.Background(), cmd.Process.Pid, cpn.SignalKill); err != nil {
		t.Fatalf("KillPID: %v", err)
	}
}

func TestOSHostAdapter_KillPID_InvalidPid(t *testing.T) {
	a := NewOSHostAdapter(nil)
	// os.FindProcess on Linux always succeeds, but Signal on a non-existent
	// PID returns ESRCH. Call and assert err != nil without hard-coding.
	err := a.KillPID(context.Background(), 0x7fffffff, cpn.SignalInt)
	if err == nil {
		t.Fatal("expected error for bogus pid")
	}
}

func TestMapSignal(t *testing.T) {
	cases := []struct {
		in   cpn.Signal
		want os.Signal
	}{
		{cpn.SignalKill, syscall.SIGKILL},
		{cpn.SignalInt, syscall.SIGINT},
		{cpn.Signal("garbage"), syscall.SIGTERM},
	}
	for _, tc := range cases {
		if got := mapSignal(tc.in); got != tc.want {
			t.Errorf("mapSignal(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOSHostAdapter_ReadFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = dir
	fp := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(fp, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := a.ReadFile(context.Background(), fp)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("got %q", data)
	}
}

func TestOSHostAdapter_ReadFile_OutsideJail(t *testing.T) {
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = t.TempDir()
	_, err := a.ReadFile(context.Background(), "/etc/passwd")
	if err == nil {
		t.Fatal("expected path_denied")
	}
}

func TestOSHostAdapter_Stat(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = dir
	fp := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(fp, []byte("xyz"), 0o600)
	info, err := a.Stat(context.Background(), fp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 3 {
		t.Fatalf("size = %d", info.Size)
	}
	if info.IsDir {
		t.Fatal("should not be dir")
	}

	// jail miss
	if _, err := a.Stat(context.Background(), "/etc/passwd"); err == nil {
		t.Fatal("expected jail error")
	}

	// path inside jail but missing file → os.Stat err
	if _, err := a.Stat(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected stat err")
	}
}

func TestOSHostAdapter_ReadFile_EmptyAllowedRoot(t *testing.T) {
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = ""
	if _, err := a.ReadFile(context.Background(), "/tmp/x"); err == nil {
		t.Fatal("expected error for empty AllowedRoot")
	}
}

func TestCheckPathJail_InvalidAbs(t *testing.T) {
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = t.TempDir()
	// An empty path's Abs does not error on Linux but resolves to cwd.
	// Force the outside-root branch via a relative-like traversal.
	if err := a.checkPathJail("/definitely/outside/jail/abs"); err == nil {
		t.Fatal("expected denied")
	}
}

func TestContainsDotDot(t *testing.T) {
	cases := map[string]bool{
		"..":        true,
		"../etc":    true,
		"foo/..":    true,
		"foo/../x":  true,
		"foo/bar":   false,
		"...":       false,
		"foo..bar":  false,
	}
	for in, want := range cases {
		if got := containsDotDot(in); got != want {
			t.Errorf("containsDotDot(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOSHostAdapter_Exec_WithEnvAndStdin(t *testing.T) {
	a := NewOSHostAdapter(nil)
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "cat",
		Env:     []string{"FOO=bar"},
		Stdin:   []byte("piped"),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(res.Stdout) != "piped" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestOSHostAdapter_Exec_WithCwd(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil)
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "pwd",
		Cwd:     dir,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// On macOS /tmp resolves through /private, so just check non-empty.
	if len(res.Stdout) == 0 {
		t.Fatal("no stdout")
	}
}

func TestIsCommandNotFound(t *testing.T) {
	if isCommandNotFound(nil) {
		t.Fatal("nil should return false")
	}
	if !isCommandNotFound(exec.ErrNotFound) {
		t.Fatal("ErrNotFound should match")
	}
	if !isCommandNotFound(os.ErrNotExist) {
		t.Fatal("ErrNotExist should match")
	}
	execErr := &exec.Error{Name: "x", Err: exec.ErrNotFound}
	if !isCommandNotFound(execErr) {
		t.Fatal("exec.Error wrap should match")
	}
	pErr := &os.PathError{Op: "open", Path: "/no", Err: syscall.ENOENT}
	if !isCommandNotFound(pErr) {
		t.Fatal("PathError ENOENT should match")
	}
	if isCommandNotFound(errors.New("random")) {
		t.Fatal("random error should not match")
	}
}

func TestCappedBuffer_NoLimit(t *testing.T) {
	c := &cappedBuffer{limit: 0}
	n, err := c.Write([]byte("unbounded"))
	if err != nil || n != len("unbounded") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if !bytes.Equal(c.Bytes(), []byte("unbounded")) {
		t.Fatalf("got %q", c.Bytes())
	}
	if c.Truncated {
		t.Fatal("should not be truncated")
	}
}

func TestCappedBuffer_ExactFit(t *testing.T) {
	c := &cappedBuffer{limit: 5}
	_, _ = c.Write([]byte("abcde"))
	if c.Truncated {
		t.Fatal("exact fit must not truncate")
	}
}

func TestCappedBuffer_OverflowInSingleWrite(t *testing.T) {
	c := &cappedBuffer{limit: 5}
	n, err := c.Write([]byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("n=%d", n)
	}
	if !c.Truncated {
		t.Fatal("expected truncation")
	}
	if string(c.Bytes()) != "01234" {
		t.Fatalf("buf = %q", c.Bytes())
	}
}

func TestCappedBuffer_OverflowAcrossTwoWrites(t *testing.T) {
	c := &cappedBuffer{limit: 5}
	_, _ = c.Write([]byte("0123"))
	// Now remaining is 1; next write is fully discarded past 1 byte.
	n, _ := c.Write([]byte("45678"))
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
	if !c.Truncated {
		t.Fatal("expected truncation")
	}
	// Additional write when remaining==0
	n, _ = c.Write([]byte("more"))
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
}

func TestOSHostAdapter_Guard_DenyList(t *testing.T) {
	a := NewOSHostAdapter(nil)
	err := a.guard("rm", []string{"-rf", "/"})
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestOSHostAdapter_Guard_PathTraversalInArgs(t *testing.T) {
	a := NewOSHostAdapter(nil)
	err := a.guard("ls", []string{"../etc"})
	if err == nil {
		t.Fatal("expected path-denied")
	}
}

func TestOSHostAdapter_Guard_EmptyDenyEntrySkipped(t *testing.T) {
	a := NewOSHostAdapter(nil)
	a.DenyList = []string{"", "impossible-needle"}
	if err := a.guard("echo", []string{"ok"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// WriteFile: jail deny, success, MkdirAll nested path
// ──────────────────────────────────────────────────────────────────────────

func TestOSHostAdapter_WriteFile_JailDeny(t *testing.T) {
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = t.TempDir()
	err := a.WriteFile(context.Background(), "/etc/bogus.txt", []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected jail deny")
	}
}

func TestOSHostAdapter_WriteFile_CreatesParent(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = dir
	fp := filepath.Join(dir, "sub1", "sub2", "file.txt")
	if err := a.WriteFile(context.Background(), fp, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(fp)
	if err != nil || string(data) != "hi" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// rate_limiter.go: Reset (0% → covered)
// ──────────────────────────────────────────────────────────────────────────

func TestTokenBucketRateLimiter_Reset(t *testing.T) {
	rl := NewTokenBucketRateLimiter(1)
	if !rl.Allow("a") {
		t.Fatal("first should allow")
	}
	if rl.Allow("a") {
		t.Fatal("second immediately should deny")
	}
	rl.Reset()
	if !rl.Allow("a") {
		t.Fatal("after reset should allow again")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// session_manager.go: Close timeout branch, Write/Kill/Chunks/Status for
// missing sessions, List shape
// ──────────────────────────────────────────────────────────────────────────

func TestBashSessionManager_Write_NotFound(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	if err := m.Write(context.Background(), "ghost", []byte("x")); !errors.Is(err, cpn.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBashSessionManager_Kill_NotFound(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	if err := m.Kill(context.Background(), "ghost"); !errors.Is(err, cpn.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBashSessionManager_Close_NotFound(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	if err := m.Close(context.Background(), "ghost"); !errors.Is(err, cpn.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBashSessionManager_Chunks_NotFound(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	if _, err := m.Chunks("ghost"); !errors.Is(err, cpn.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBashSessionManager_Status_NotFound(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	if _, err := m.Status("ghost"); !errors.Is(err, cpn.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBashSessionManager_List_AfterOpen(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	id, err := m.Open(context.Background(), cpn.PTYRequest{Command: "sleep", Args: []string{"2"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = m.Kill(context.Background(), id) }()

	infos := m.List(context.Background())
	if len(infos) != 1 {
		t.Fatalf("len=%d", len(infos))
	}
	if infos[0].ID != id || infos[0].Command != "sleep" {
		t.Fatalf("info = %+v", infos[0])
	}
	if infos[0].PID == 0 {
		t.Fatal("pid should be set")
	}
}

func TestBashSessionManager_Close_ContextCancel(t *testing.T) {
	m := NewInMemoryBashSessionManager(nil)
	id, err := m.Open(context.Background(), cpn.PTYRequest{Command: "sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Close(ctx, id); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Helper to avoid unused-import warnings if refactored.
var _ = strings.Contains
