package config

import (
	"context"
	"os"
	"sync"
	"time"
)

// Provider is the hot-reloadable config provider.
// Resolution priority: env var > DB stored value > default.
type Provider struct {
	mu       sync.RWMutex
	store    Store
	cache    map[string]*Entry
	onChange []func(key, value string)
}

// NewProvider creates a Provider backed by the given store.
func NewProvider(store Store) *Provider {
	return &Provider{
		store: store,
		cache: make(map[string]*Entry),
	}
}

// Get returns the resolved value for a config key.
// Priority: env var > DB > default.
func (p *Provider) Get(key string) string {
	def := DefByKey(key)
	if def == nil {
		return ""
	}

	// 1. Env var takes precedence.
	if v := os.Getenv(def.EnvVar); v != "" {
		return v
	}

	// 2. DB cached value.
	p.mu.RLock()
	entry, ok := p.cache[key]
	p.mu.RUnlock()
	if ok && entry.Value != "" {
		return entry.Value
	}

	// 3. Default.
	return def.Default
}

// Source returns where the current value for a key comes from.
func (p *Provider) Source(key string) string {
	def := DefByKey(key)
	if def == nil {
		return "default"
	}
	if v := os.Getenv(def.EnvVar); v != "" {
		return "env"
	}
	p.mu.RLock()
	entry, ok := p.cache[key]
	p.mu.RUnlock()
	if ok && entry.Value != "" {
		return "db"
	}
	return "default"
}

// Set writes a config value to the store and updates the cache.
// Fires OnChange callbacks.
func (p *Provider) Set(ctx context.Context, key, value, updatedBy string) error {
	def := DefByKey(key)
	if def == nil {
		return ErrNotFound
	}

	entry := &Entry{
		Key:       key,
		Value:     value,
		IsSecret:  def.IsSecret,
		UpdatedAt: time.Now(),
		UpdatedBy: updatedBy,
	}
	if err := p.store.Set(ctx, entry); err != nil {
		return err
	}

	p.mu.Lock()
	p.cache[key] = entry
	p.mu.Unlock()

	// Fire callbacks.
	for _, fn := range p.onChange {
		fn(key, value)
	}
	return nil
}

// Delete removes a config value from the store and cache.
func (p *Provider) Delete(ctx context.Context, key string) error {
	if err := p.store.Delete(ctx, key); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.cache, key)
	p.mu.Unlock()

	// Fire callbacks with the new resolved value (env or default).
	resolved := p.Get(key)
	for _, fn := range p.onChange {
		fn(key, resolved)
	}
	return nil
}

// Refresh reloads all entries from the store into the cache.
func (p *Provider) Refresh(ctx context.Context) error {
	entries, err := p.store.List(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache = make(map[string]*Entry, len(entries))
	for _, e := range entries {
		p.cache[e.Key] = e
	}
	return nil
}

// OnChange registers a callback fired when a config value is changed via Set or Delete.
func (p *Provider) OnChange(fn func(key, value string)) {
	p.onChange = append(p.onChange, fn)
}

// ListAll returns all known config entries with their resolved values and sources.
// Secret values from DB are masked. Env-sourced secret values are also masked.
func (p *Provider) ListAll() []ResolvedConfig {
	result := make([]ResolvedConfig, 0, len(PlatformConfigs))
	for _, def := range PlatformConfigs {
		source := p.Source(def.Key)
		value := p.Get(def.Key)

		// Mask secrets.
		displayValue := value
		if def.IsSecret && value != "" {
			if len(value) > 10 {
				displayValue = value[:10] + "***"
			} else {
				displayValue = "********"
			}
		}

		var updatedAt, updatedBy string
		p.mu.RLock()
		if entry, ok := p.cache[def.Key]; ok {
			updatedAt = entry.UpdatedAt.Format(time.RFC3339)
			updatedBy = entry.UpdatedBy
		}
		p.mu.RUnlock()

		result = append(result, ResolvedConfig{
			Key:       def.Key,
			Value:     displayValue,
			IsSecret:  def.IsSecret,
			Source:    source,
			Label:     def.Label,
			Category:  def.Category,
			UpdatedAt: updatedAt,
			UpdatedBy: updatedBy,
		})
	}
	return result
}

// MissingRequired returns keys that are required but have no value from any source.
func (p *Provider) MissingRequired() []string {
	var missing []string
	for _, def := range PlatformConfigs {
		if def.Required && p.Get(def.Key) == "" {
			missing = append(missing, def.Key)
		}
	}
	return missing
}

// ResolvedConfig is the API-friendly representation of a config entry.
type ResolvedConfig struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret"`
	Source    string `json:"source"` // "env", "db", "default"
	Label     string `json:"label"`
	Category  string `json:"category"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}
