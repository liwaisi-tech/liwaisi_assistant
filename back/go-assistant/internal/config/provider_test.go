package config

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// memConfigStore is an in-memory ConfigStore for testing.
type memConfigStore struct {
	mu      sync.RWMutex
	entries map[string]*ConfigEntry
}

func newMemConfigStore() *memConfigStore {
	return &memConfigStore{entries: make(map[string]*ConfigEntry)}
}

func (m *memConfigStore) Get(_ context.Context, key string) (*ConfigEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[key]
	if !ok {
		return nil, nil
	}
	cp := *e
	return &cp, nil
}

func (m *memConfigStore) Set(_ context.Context, entry *ConfigEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *entry
	m.entries[entry.Key] = &cp
	return nil
}

func (m *memConfigStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *memConfigStore) List(_ context.Context) ([]*ConfigEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*ConfigEntry, 0, len(m.entries))
	for _, e := range m.entries {
		cp := *e
		result = append(result, &cp)
	}
	return result, nil
}

func TestProviderResolutionPriority(t *testing.T) {
	store := newMemConfigStore()
	provider := NewProvider(store)
	ctx := context.Background()

	// 1. Default value when nothing is set.
	val, source := provider.Get("default_model")
	if source != "default" {
		t.Fatalf("expected source 'default', got %q", source)
	}
	if val != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("expected default model, got %q", val)
	}

	// 2. DB value takes precedence over default.
	err := provider.Set(ctx, &ConfigEntry{
		Key:       "default_model",
		Value:     "custom/model",
		UpdatedBy: "test",
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	val, source = provider.Get("default_model")
	if source != "db" {
		t.Fatalf("expected source 'db', got %q", source)
	}
	if val != "custom/model" {
		t.Fatalf("expected 'custom/model', got %q", val)
	}

	// 3. Env var takes precedence over DB.
	os.Setenv("DEFAULT_MODEL", "env/model")
	defer os.Unsetenv("DEFAULT_MODEL")

	val, source = provider.Get("default_model")
	if source != "env" {
		t.Fatalf("expected source 'env', got %q", source)
	}
	if val != "env/model" {
		t.Fatalf("expected 'env/model', got %q", val)
	}
}

func TestProviderOnChangeCallback(t *testing.T) {
	store := newMemConfigStore()
	provider := NewProvider(store)
	ctx := context.Background()

	var callbackValue string
	var callbackCalled bool
	provider.OnChange("default_model", func(newValue string) {
		callbackValue = newValue
		callbackCalled = true
	})

	err := provider.Set(ctx, &ConfigEntry{
		Key:       "default_model",
		Value:     "changed/model",
		UpdatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !callbackCalled {
		t.Fatal("expected callback to be called")
	}
	if callbackValue != "changed/model" {
		t.Fatalf("expected 'changed/model', got %q", callbackValue)
	}
}

func TestProviderSetupRequired(t *testing.T) {
	store := newMemConfigStore()
	provider := NewProvider(store)

	// With no env or DB value for openrouter_api_key, setup is required.
	required, missing := provider.SetupRequired()
	if !required {
		t.Fatal("expected setup to be required")
	}
	found := false
	for _, k := range missing {
		if k == "openrouter_api_key" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected openrouter_api_key in missing list")
	}

	// Set it via DB.
	err := provider.Set(context.Background(), &ConfigEntry{
		Key:   "openrouter_api_key",
		Value: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}

	required, _ = provider.SetupRequired()
	if required {
		t.Fatal("expected setup NOT to be required after setting key")
	}
}

func TestProviderRefresh(t *testing.T) {
	store := newMemConfigStore()
	provider := NewProvider(store)
	ctx := context.Background()

	// Write directly to store (bypassing provider).
	_ = store.Set(ctx, &ConfigEntry{Key: "default_model", Value: "refreshed/model", UpdatedAt: time.Now()})

	// Provider cache doesn't have it yet.
	val, source := provider.Get("default_model")
	if source != "default" {
		t.Fatalf("expected 'default' before refresh, got %q", source)
	}
	_ = val

	// After refresh, the DB value should be visible.
	if err := provider.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	val, source = provider.Get("default_model")
	if source != "db" {
		t.Fatalf("expected 'db' after refresh, got %q", source)
	}
	if val != "refreshed/model" {
		t.Fatalf("expected 'refreshed/model', got %q", val)
	}
}

func TestProviderDeleteFiresCallback(t *testing.T) {
	store := newMemConfigStore()
	provider := NewProvider(store)
	ctx := context.Background()

	_ = provider.Set(ctx, &ConfigEntry{Key: "default_model", Value: "to-delete"})

	var callbackValue string
	provider.OnChange("default_model", func(newValue string) {
		callbackValue = newValue
	})

	_ = provider.Delete(ctx, "default_model")

	// After delete, should fall back to default.
	if callbackValue != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("expected default value after delete, got %q", callbackValue)
	}
}
