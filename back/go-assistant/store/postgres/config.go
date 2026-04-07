package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
)

// ConfigRepository implements config.Store using PostgreSQL with AES-256-GCM encryption.
type ConfigRepository struct {
	pool      pgxPool
	encryptor *config.Encryptor
}

// Compile-time interface check.
var _ config.Store = (*ConfigRepository)(nil)

// NewConfigRepository creates a new encrypted config repository.
func NewConfigRepository(pool pgxPool, enc *config.Encryptor) *ConfigRepository {
	return &ConfigRepository{pool: pool, encryptor: enc}
}

// Get retrieves and decrypts a config entry by key.
func (r *ConfigRepository) Get(ctx context.Context, key string) (*config.Entry, error) {
	var valueCipher, nonce []byte
	var isSecret bool
	var updatedAt time.Time
	var updatedBy string

	err := r.pool.QueryRow(ctx,
		`SELECT value, nonce, is_secret, updated_at, updated_by FROM platform_config WHERE key = $1`, key,
	).Scan(&valueCipher, &nonce, &isSecret, &updatedAt, &updatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, config.ErrNotFound
		}
		return nil, fmt.Errorf("postgres config get %s: %w", key, err)
	}

	plaintext, err := r.encryptor.Decrypt(valueCipher, nonce)
	if err != nil {
		return nil, fmt.Errorf("postgres config decrypt %s: %w", key, err)
	}

	return &config.Entry{
		Key:       key,
		Value:     string(plaintext),
		IsSecret:  isSecret,
		UpdatedAt: updatedAt,
		UpdatedBy: updatedBy,
	}, nil
}

// Set encrypts and persists a config entry (upsert).
func (r *ConfigRepository) Set(ctx context.Context, entry *config.Entry) error {
	ciphertext, nonce, err := r.encryptor.Encrypt([]byte(entry.Value))
	if err != nil {
		return fmt.Errorf("postgres config encrypt %s: %w", entry.Key, err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO platform_config (key, value, nonce, is_secret, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (key) DO UPDATE SET
		   value = EXCLUDED.value,
		   nonce = EXCLUDED.nonce,
		   is_secret = EXCLUDED.is_secret,
		   updated_at = EXCLUDED.updated_at,
		   updated_by = EXCLUDED.updated_by`,
		entry.Key, ciphertext, nonce, entry.IsSecret, entry.UpdatedAt, entry.UpdatedBy,
	)
	if err != nil {
		return fmt.Errorf("postgres config set %s: %w", entry.Key, err)
	}
	return nil
}

// Delete removes a config entry by key.
func (r *ConfigRepository) Delete(ctx context.Context, key string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM platform_config WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("postgres config delete %s: %w", key, err)
	}
	return nil
}

// List retrieves all config entries with decrypted values.
func (r *ConfigRepository) List(ctx context.Context) ([]*config.Entry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT key, value, nonce, is_secret, updated_at, updated_by FROM platform_config`)
	if err != nil {
		return nil, fmt.Errorf("postgres config list: %w", err)
	}
	defer rows.Close()

	var entries []*config.Entry
	for rows.Next() {
		var valueCipher, nonce []byte
		var e config.Entry
		if err := rows.Scan(&e.Key, &valueCipher, &nonce, &e.IsSecret, &e.UpdatedAt, &e.UpdatedBy); err != nil {
			return nil, fmt.Errorf("postgres config scan: %w", err)
		}
		plaintext, err := r.encryptor.Decrypt(valueCipher, nonce)
		if err != nil {
			return nil, fmt.Errorf("postgres config decrypt %s: %w", e.Key, err)
		}
		e.Value = string(plaintext)
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
