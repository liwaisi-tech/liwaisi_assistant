package host

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestReadFile_RejectsSymlinkEscape covers AC-002 / SEC-FIX-002.
//
// Given a symlink inside AllowedRoot pointing at a file outside,
// ReadFile must refuse with cpn.ErrPathDenied rather than follow the
// link and exfiltrate the target.
func TestReadFile_RejectsSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "jail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	victim := filepath.Join(tmp, "secret")
	if err := os.WriteFile(victim, []byte("shh"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	evil := filepath.Join(root, "evil")
	if err := os.Symlink(victim, evil); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
	_, err := a.ReadFile(context.Background(), evil)
	if err == nil {
		t.Fatalf("expected ErrPathDenied, got nil")
	}
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}
}

// TestWriteFile_RejectsSymlinkEscape covers the write side of SEC-FIX-002.
func TestWriteFile_RejectsSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "jail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	outside := filepath.Join(tmp, "outside")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	evil := filepath.Join(root, "evil")
	if err := os.Symlink(outside, evil); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
	err := a.WriteFile(context.Background(), evil, []byte("pwned"), 0o600)
	if err == nil {
		t.Fatalf("expected ErrPathDenied, got nil")
	}
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}

	// Victim file must be untouched.
	got, rerr := os.ReadFile(outside)
	if rerr != nil {
		t.Fatalf("read victim back: %v", rerr)
	}
	if string(got) != "original" {
		t.Fatalf("victim was overwritten: %q", string(got))
	}
}

// TestStat_RejectsSymlinkEscape covers Stat for SEC-FIX-002.
func TestStat_RejectsSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "jail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	victim := filepath.Join(tmp, "victim")
	if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	evil := filepath.Join(root, "evil")
	if err := os.Symlink(victim, evil); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
	_, err := a.Stat(context.Background(), evil)
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied, got %v", err)
	}
}

// TestWriteFile_CreateNewFileInJail confirms the symlink-resolution logic
// does not regress the happy path: writing a brand-new file under the jail
// succeeds even though the leaf does not yet exist.
func TestWriteFile_CreateNewFileInJail(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "jail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
	target := filepath.Join(root, "nested", "deeper", "hello.txt")
	if err := a.WriteFile(context.Background(), target, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

// TestWriteFile_RejectsSymlinkInParent covers the more-subtle case where
// a middle-of-path component is a symlink escaping the jail.
func TestWriteFile_RejectsSymlinkInParent(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "jail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	outsideDir := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	// jail/escape -> ../outside
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outsideDir, escape); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
	target := filepath.Join(escape, "newfile.txt")
	err := a.WriteFile(context.Background(), target, []byte("x"), 0o600)
	if !errors.Is(err, cpn.ErrPathDenied) {
		t.Fatalf("expected ErrPathDenied (parent-symlink escape), got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(outsideDir, "newfile.txt")); serr == nil {
		t.Fatalf("file was written outside the jail")
	}
}
