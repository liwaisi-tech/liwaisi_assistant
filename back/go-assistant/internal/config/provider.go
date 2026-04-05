package config

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"
)

// Provider resolves configuration values using the chain: env > DB > default.
// It caches DB values in memory and supports change callbacks for hot-reload.
type Provider struct {
	store     ConfigStore
	mu        sync.RWMutex
	cache     map[string]*ConfigEntry
	callbacks map[string][]func(newValue string)
}

// NewProvider creates a Provider backed by the given store.
func NewProvider(store ConfigStore) *Provider {
	return &Provider{
		store:     store,
		cache:     make(map[string]*ConfigEntry),
		callbacks: make(map[string][]func(newValue string)),
	}
}

// Get resolves a config key. Returns (value, source).
// Source is "env", "db", or "default".
func (p *Provider) Get(key string) (string, string) {
	def := LookupConfigDef(key)
	if def == nil {
		return "", ""
	}

	// 1. Environment variable takes precedence.
	if def.EnvVar != "" {
		if v := os.Getenv(def.EnvVar); v != "" {
			return v, "env"
		}
	}

	// 2. DB cache.
	p.mu.RLock()
	entry, ok := p.cache[key]
	p.mu.RUnlock()
	if ok && entry != nil {
		return entry.Value, "db"
	}

	// 3. Default.
	return def.Default, "default"
}

// Set writes a config entry to the store, updates cache, and fires callbacks.
func (p *Provider) Set(ctx context.Context, entry *ConfigEntry) error {
	def := LookupConfigDef(entry.Key)
	if def != nil {
		entry.IsSecret = def.IsSecret
	}
	entry.UpdatedAt = time.Now()

	if err := p.store.Set(ctx, entry); err != nil {
		return err
	}

	p.mu.Lock()
	p.cache[entry.Key] = entry
	cbs := p.callbacks[entry.Key]
	p.mu.Unlock()

	// Resolve the effective value (env still wins).
	val, _ := p.Get(entry.Key)
	for _, fn := range cbs {
		fn(val)
	}
	return nil
}

// Delete removes a config entry from the store and cache.
func (p *Provider) Delete(ctx context.Context, key string) error {
	if err := p.store.Delete(ctx, key); err != nil {
		return err
	}

	p.mu.Lock()
	delete(p.cache, key)
	cbs := p.callbacks[key]
	p.mu.Unlock()

	// Fire callbacks with the new effective value (env or default).
	val, _ := p.Get(key)
	for _, fn := range cbs {
		fn(val)
	}
	return nil
}

// List returns a merged view of all known config keys with their values and sources.
// Secret values from DB are masked with "********".
func (p *Provider) List(ctx context.Context) []ConfigItem {
	items := make([]ConfigItem, 0, len(PlatformConfigs))
	for _, def := range PlatformConfigs {
		value, source := p.Get(def.Key)
		displayValue := value

		if def.IsSecret && value != "" && source != "default" {
			displayValue = maskSecret(value)
		}

		var updatedAt, updatedBy string
		p.mu.RLock()
		if entry, ok := p.cache[def.Key]; ok && entry != nil {
			updatedAt = entry.UpdatedAt.Format(time.RFC3339)
			updatedBy = entry.UpdatedBy
		}
		p.mu.RUnlock()

		items = append(items, ConfigItem{
			Key:       def.Key,
			Value:     displayValue,
			IsSecret:  def.IsSecret,
			Source:    source,
			Label:     def.Label,
			Category:  def.Category,
			Required:  def.Required,
			UpdatedAt: updatedAt,
			UpdatedBy: updatedBy,
		})
	}
	return items
}

// Refresh reloads all entries from the DB store into the cache.
func (p *Provider) Refresh(ctx context.Context) error {
	entries, err := p.store.List(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache = make(map[string]*ConfigEntry, len(entries))
	for _, e := range entries {
		p.cache[e.Key] = e
	}
	return nil
}

// OnChange registers a callback that fires when the effective value of key changes.
func (p *Provider) OnChange(key string, fn func(newValue string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callbacks[key] = append(p.callbacks[key], fn)
}

// SetupRequired returns true if any required config keys have no effective value,
// along with the list of missing keys.
func (p *Provider) SetupRequired() (bool, []string) {
	var missing []string
	for _, def := range PlatformConfigs {
		if !def.Required {
			continue
		}
		val, _ := p.Get(def.Key)
		if val == "" {
			missing = append(missing, def.Key)
		}
	}
	return len(missing) > 0, missing
}

// maskSecret returns a masked version of a secret value, showing first 4 chars.
func maskSecret(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", 8)
}
