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

// LoadOrGenerateMasterKey reads a 32-byte master key from path,
// generating one if the file does not exist.
func LoadOrGenerateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
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

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("config: mkdir for master key: %w", err)
	}

	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("config: write master key: %w", err)
	}
	return key, nil
}
