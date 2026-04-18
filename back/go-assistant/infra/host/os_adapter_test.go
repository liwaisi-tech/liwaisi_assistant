//go:build linux

package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Basic execution ─────────────────────────────────────────────────────────

func TestOSHostAdapter_Exec_Echo(t *testing.T) {
	a := NewOSHostAdapter(nil)
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "echo",
		Args:    []string{"hello"},
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if !strings.HasPrefix(string(res.Stdout), "hello") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestOSHostAdapter_Exec_Timeout(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "sleep",
		Args:    []string{"10"},
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, cpn.ErrTimeoutHost) {
		t.Fatalf("expected ErrTimeoutHost, got %v", err)
	}
}

func TestOSHostAdapter_Exec_NonZeroExit(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "false",
		Timeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected non-zero error")
	}
	if !errors.Is(err, cpn.ErrNonZeroExit) {
		t.Fatalf("expected ErrNonZeroExit, got %v", err)
	}
}

func TestOSHostAdapter_Exec_AllowNonZero(t *testing.T) {
	a := NewOSHostAdapter(nil)
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command:      "false",
		Timeout:      2 * time.Second,
		AllowNonZero: true,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("exit = %d, want 1", res.ExitCode)
	}
}

func TestOSHostAdapter_Exec_CommandNotFound(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "this-command-does-not-exist-xyz123",
		Timeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected command-not-found")
	}
	if !errors.Is(err, cpn.ErrCommandNotFound) {
		t.Fatalf("expected ErrCommandNotFound, got %v", err)
	}
}

// ── Streaming via Stdin piping ──────────────────────────────────────────────

func TestOSHostAdapter_Exec_StdinPiped(t *testing.T) {
	a := NewOSHostAdapter(nil)
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "cat",
		Stdin:   []byte("hello stdin\n"),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(res.Stdout) != "hello stdin\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

// ── SEC-001: deny list & path traversal ────────────────────────────────────

func TestOSHostAdapter_DenyList(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "bash",
		Args:    []string{"-c", "rm -rf /"},
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected deny")
	}
	if !errors.Is(err, cpn.ErrDenyListed) {
		t.Fatalf("expected ErrDenyListed, got %v", err)
	}
}

func TestOSHostAdapter_PathTraversalArg(t *testing.T) {
	a := NewOSHostAdapter(nil)
	_, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "cat",
		Args:    []string{"../etc/passwd"},
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected path traversal rejection")
	}
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}
}

// ── SEC-002: WriteFile path jail ────────────────────────────────────────────

func TestOSHostAdapter_WriteFile_Jail(t *testing.T) {
	a := NewOSHostAdapter(nil)
	err := a.WriteFile(context.Background(), "/etc/passwd", []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected path denied")
	}
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}
}

func TestOSHostAdapter_WriteFile_InsideJail(t *testing.T) {
	tmpRoot := t.TempDir()
	a := &OSHostAdapter{
		MaxOutputBytes: DefaultMaxOutputBytes,
		AllowedRoot:    tmpRoot,
	}
	target := filepath.Join(tmpRoot, "hello.txt")
	if err := a.WriteFile(context.Background(), target, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("contents = %q", got)
	}
}

// ── Stdout truncation ───────────────────────────────────────────────────────

func TestOSHostAdapter_Exec_Truncation(t *testing.T) {
	a := &OSHostAdapter{MaxOutputBytes: 10}
	res, err := a.Exec(context.Background(), cpn.ExecRequest{
		Command: "bash",
		Args:    []string{"-c", "head -c 500 /dev/zero | tr '\\0' 'a'"},
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("expected Truncated=true")
	}
	if len(res.Stdout) > 10 {
		t.Fatalf("stdout len = %d, want <= 10", len(res.Stdout))
	}
}
