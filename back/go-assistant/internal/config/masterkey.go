package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const masterKeySize = 32

// checkParentDirPerms ensures the parent directory of path is not group/world accessible.
func checkParentDirPerms(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("config: stat master key parent dir %s: %w", parent, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config: master key parent %s is not a directory", parent)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("config: master key parent dir %s has insecure mode %#o, want 0700", parent, perm)
	}
	return nil
}

// LoadOrGenerateMasterKey reads a 32-byte master key from path,
// generating one if the file does not exist.
func LoadOrGenerateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, fmt.Errorf("config: stat master key: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("config: master key at %s is not a regular file", path)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			return nil, fmt.Errorf("config: master key at %s has insecure mode %#o, want 0600", path, perm)
		}
		if err := checkParentDirPerms(path); err != nil {
			return nil, err
		}
		if len(data) != masterKeySize {
			return nil, fmt.Errorf("config: master key at %s has %d bytes, want %d", path, len(data), masterKeySize)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config: read master key: %w", err)
	}

	// Generate new key.
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("config: generate master key: %w", err)
	}

	// Ensure parent directory exists with strict perms.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("config: mkdir for master key: %w", err)
	}
	if err := checkParentDirPerms(path); err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("config: write master key: %w", err)
	}
	return key, nil
}
