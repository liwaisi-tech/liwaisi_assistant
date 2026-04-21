package awakens

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMemoryCache_HitMissPutGet(t *testing.T) {
	t.Parallel()
	c := NewMemoryPersonalityCache(4)

	if _, ok := c.Get("nope"); ok {
		t.Fatalf("expected miss on empty cache")
	}
	c.Put("d1", "block-1")
	got, ok := c.Get("d1")
	if !ok || got != "block-1" {
		t.Fatalf("hit mismatch: got=%q ok=%v", got, ok)
	}
	c.Put("d1", "block-1b")
	if got, _ := c.Get("d1"); got != "block-1b" {
		t.Fatalf("put-overwrite failed: %q", got)
	}
	c.Invalidate("d1")
	if _, ok := c.Get("d1"); ok {
		t.Fatalf("expected miss after invalidate")
	}
}

func TestMemoryCache_EvictsOverCap(t *testing.T) {
	t.Parallel()
	c := NewMemoryPersonalityCache(3)
	c.Put("a", "A")
	c.Put("b", "B")
	c.Put("c", "C")
	if _, ok := c.Get("a"); !ok {
		t.Fatalf("a should still be cached")
	}
	c.Put("d", "D") // forces LRU eviction; b is oldest (a was just touched).
	if _, ok := c.Get("b"); ok {
		t.Fatalf("b should have been evicted as LRU")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Fatalf("%s should still be cached", k)
		}
	}
	if got := c.Len(); got != 3 {
		t.Fatalf("len after eviction = %d, want 3", got)
	}
}

func TestMemoryCache_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	c := NewMemoryPersonalityCache(16)
	var wg sync.WaitGroup
	var hits int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k%d", (g*i)%24)
				c.Put(key, fmt.Sprintf("block-%d", i))
				if _, ok := c.Get(key); ok {
					atomic.AddInt64(&hits, 1)
				}
			}
		}(g)
	}
	wg.Wait()
	if atomic.LoadInt64(&hits) == 0 {
		t.Fatalf("expected at least one concurrent hit")
	}
}

func TestMemoryCache_EmptyDigestIgnored(t *testing.T) {
	t.Parallel()
	c := NewMemoryPersonalityCache(2)
	c.Put("", "block")
	if _, ok := c.Get(""); ok {
		t.Fatalf("empty digest should never hit")
	}
	c.Invalidate("")
	if c.Len() != 0 {
		t.Fatalf("empty digest must not populate cache")
	}
}

func TestMemoryCache_DefaultCap(t *testing.T) {
	t.Parallel()
	c := NewMemoryPersonalityCache(0)
	if c.cap != DefaultPersonalityCacheCap {
		t.Fatalf("cap = %d, want %d", c.cap, DefaultPersonalityCacheCap)
	}
}
