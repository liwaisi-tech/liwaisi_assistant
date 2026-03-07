package subagent

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

const (
	defaultCacheTTL             = 30 * time.Minute
	defaultCacheCleanupInterval = 5 * time.Minute
)

// CacheEntry holds a cached SubAgentSpec with TTL metadata.
type CacheEntry struct {
	Spec      entity.SubAgentSpec
	CreatedAt time.Time
	ExpiresAt time.Time
	HitCount  atomic.Int64
}

// Cache stores subagent specs with configurable TTL and automatic
// expiration. All methods are safe for concurrent use.
type Cache struct {
	mu              sync.RWMutex
	entries         map[string]*CacheEntry
	ttl             time.Duration
	cleanupInterval time.Duration
	janitor         *time.Ticker
	done            chan struct{}
	closeOnce       sync.Once
}

// CacheOption configures optional Cache behavior.
type CacheOption func(*Cache)

// WithTTL sets the time-to-live for cache entries.
func WithTTL(d time.Duration) CacheOption {
	return func(c *Cache) {
		c.ttl = d
	}
}

// WithCleanupInterval sets how often the background janitor removes
// expired entries.
func WithCleanupInterval(d time.Duration) CacheOption {
	return func(c *Cache) {
		c.cleanupInterval = d
	}
}

// NewCache creates a cache with the given options and starts a
// background janitor goroutine. Call Close to stop the janitor.
func NewCache(opts ...CacheOption) *Cache {
	c := &Cache{
		entries:         make(map[string]*CacheEntry),
		ttl:             defaultCacheTTL,
		cleanupInterval: defaultCacheCleanupInterval,
		done:            make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.cleanupInterval > 0 {
		c.janitor = time.NewTicker(c.cleanupInterval)
		go c.runJanitor()
	}

	return c
}

// Get returns the cached spec for name if it exists and has not expired.
// On hit, the entry's HitCount is incremented.
func (c *Cache) Get(name string) (entity.SubAgentSpec, bool) {
	c.mu.RLock()
	entry, ok := c.entries[name]
	c.mu.RUnlock()

	if !ok {
		return entity.SubAgentSpec{}, false
	}

	if time.Now().After(entry.ExpiresAt) {
		c.mu.Lock()
		if current, ok := c.entries[name]; ok && current == entry {
			delete(c.entries, name)
		}
		c.mu.Unlock()
		return entity.SubAgentSpec{}, false
	}

	entry.HitCount.Add(1)
	return entry.Spec, true
}

// Put stores the spec with the configured TTL. If an entry with the
// same name exists, it is overwritten.
func (c *Cache) Put(spec *entity.SubAgentSpec) {
	now := time.Now()
	entry := &CacheEntry{
		Spec:      *spec,
		CreatedAt: now,
		ExpiresAt: now.Add(c.ttl),
	}

	c.mu.Lock()
	c.entries[spec.Name] = entry
	c.mu.Unlock()
}

// Evict removes all expired entries and returns the number evicted.
func (c *Cache) Evict() int {
	now := time.Now()
	evicted := 0

	c.mu.Lock()
	for name, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, name)
			evicted++
		}
	}
	c.mu.Unlock()

	return evicted
}

// Size returns the number of entries currently in the cache (including
// potentially expired ones that haven't been cleaned up yet).
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Close stops the background janitor goroutine. Safe to call multiple
// times concurrently; subsequent calls are no-ops.
func (c *Cache) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.janitor != nil {
			c.janitor.Stop()
		}
	})
}

func (c *Cache) runJanitor() {
	for {
		select {
		case <-c.janitor.C:
			c.Evict()
		case <-c.done:
			return
		}
	}
}
