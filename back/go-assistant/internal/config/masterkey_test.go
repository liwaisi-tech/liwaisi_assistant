package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeKeyFile(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, masterKeySize), perm); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
}

// AC-004: 0600 mode loads successfully.
func TestLoadOrGenerateMasterKey_LoadsWith0600(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	path := filepath.Join(dir, "master.key")
	writeKeyFile(t, path, 0o600)

	got, err := LoadOrGenerateMasterKey(path)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if len(got) != masterKeySize {
		t.Fatalf("len=%d", len(got))
	}
}

// AC-003: 0644 mode rejected.
func TestLoadOrGenerateMasterKey_RejectsLooseFilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	path := filepath.Join(dir, "master.key")
	writeKeyFile(t, path, 0o644)

	_, err := LoadOrGenerateMasterKey(path)
	if err == nil {
		t.Fatal("expected error for 0644")
	}
	if !strings.Contains(err.Error(), "insecure mode") || !strings.Contains(err.Error(), path) {
		t.Fatalf("error should mention path and mode: %v", err)
	}
}

// AC-005: parent dir 0755 is self-healed to 0700 when the process owns it
// (the normal Docker named-volume case). The load succeeds and the parent is
// tightened as a side effect.
func TestLoadOrGenerateMasterKey_SelfHealsLooseParentDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	writeKeyFile(t, path, 0o600)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck

	if _, err := LoadOrGenerateMasterKey(path); err != nil {
		t.Fatalf("expected self-heal to succeed, got: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("parent dir mode after self-heal = %#o, want 0700", perm)
	}
}

// EC-001: symlinked key file rejected.
func TestLoadOrGenerateMasterKey_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	target := filepath.Join(dir, "real.key")
	writeKeyFile(t, target, 0o600)
	link := filepath.Join(dir, "master.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := LoadOrGenerateMasterKey(link)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("error should mention regular file: %v", err)
	}
}

// Generate path: when no file exists, key is created and parent dir checked.
func TestLoadOrGenerateMasterKey_GeneratesNewKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	path := filepath.Join(dir, "sub", "master.key")
	got, err := LoadOrGenerateMasterKey(path)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != masterKeySize {
		t.Fatalf("len=%d", len(got))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %#o", info.Mode().Perm())
	}
}
