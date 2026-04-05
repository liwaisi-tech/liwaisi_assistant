package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
)

// ConfigRepository implements config.ConfigStore using PostgreSQL with AES-256-GCM encryption.
type ConfigRepository struct {
	pool pgxPool
	enc  *config.Encryptor
}

// Compile-time interface assertion.
var _ config.ConfigStore = (*ConfigRepository)(nil)

// NewConfigRepository creates a ConfigRepository.
func NewConfigRepository(pool pgxPool, enc *config.Encryptor) *ConfigRepository {
	return &ConfigRepository{pool: pool, enc: enc}
}

// Get retrieves a config entry by key. Returns nil, nil if not found.
func (r *ConfigRepository) Get(ctx context.Context, key string) (*config.ConfigEntry, error) {
	var valueCT, nonce []byte
	entry := &config.ConfigEntry{Key: key}
	err := r.pool.QueryRow(ctx,
		`SELECT value, nonce, is_secret, updated_at, updated_by
		 FROM platform_config WHERE key = $1`, key,
	).Scan(&valueCT, &nonce, &entry.IsSecret, &entry.UpdatedAt, &entry.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("config get %s: %w", key, err)
	}

	plaintext, err := r.enc.Decrypt(valueCT, nonce)
	if err != nil {
		return nil, fmt.Errorf("config decrypt %s: %w", key, err)
	}
	entry.Value = string(plaintext)
	return entry, nil
}

// Set upserts a config entry, encrypting the value.
func (r *ConfigRepository) Set(ctx context.Context, entry *config.ConfigEntry) error {
	ciphertext, nonce, err := r.enc.Encrypt([]byte(entry.Value))
	if err != nil {
		return fmt.Errorf("config encrypt %s: %w", entry.Key, err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO platform_config (key, value, nonce, is_secret, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (key) DO UPDATE SET
		   value = $2, nonce = $3, is_secret = $4, updated_at = $5, updated_by = $6`,
		entry.Key, ciphertext, nonce, entry.IsSecret, entry.UpdatedAt, entry.UpdatedBy,
	)
	if err != nil {
		return fmt.Errorf("config set %s: %w", entry.Key, err)
	}
	return nil
}

// Delete removes a config entry by key.
func (r *ConfigRepository) Delete(ctx context.Context, key string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM platform_config WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("config delete %s: %w", key, err)
	}
	return nil
}

// List returns all config entries, decrypting values.
func (r *ConfigRepository) List(ctx context.Context) ([]*config.ConfigEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT key, value, nonce, is_secret, updated_at, updated_by FROM platform_config`)
	if err != nil {
		return nil, fmt.Errorf("config list: %w", err)
	}
	defer rows.Close()

	var entries []*config.ConfigEntry
	for rows.Next() {
		var valueCT, nonce []byte
		entry := &config.ConfigEntry{}
		if err := rows.Scan(&entry.Key, &valueCT, &nonce, &entry.IsSecret, &entry.UpdatedAt, &entry.UpdatedBy); err != nil {
			return nil, fmt.Errorf("config list scan: %w", err)
		}
		plaintext, err := r.enc.Decrypt(valueCT, nonce)
		if err != nil {
			return nil, fmt.Errorf("config list decrypt %s: %w", entry.Key, err)
		}
		entry.Value = string(plaintext)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
