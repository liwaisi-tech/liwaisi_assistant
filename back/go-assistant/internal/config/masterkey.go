package config

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const masterKeySize = 32

// LoadOrGenerateMasterKey reads a 32-byte master key from path,
// or generates one if the file does not exist.
// The file is created with 0600 permissions; parent directories are created as needed.
func LoadOrGenerateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != masterKeySize {
			return nil, fmt.Errorf("master key at %s has invalid length %d (expected %d)", path, len(data), masterKeySize)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	// Generate a new key.
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create master key directory: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}

	return key, nil
}
