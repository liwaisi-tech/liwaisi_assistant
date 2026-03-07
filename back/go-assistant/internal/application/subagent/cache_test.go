package subagent

import (
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func makeSpec(name string) entity.SubAgentSpec {
	return entity.SubAgentSpec{
		Name:        name,
		Description: "Test agent " + name,
		Instruction: "You are " + name + ".",
	}
}

func putSpec(c *Cache, name string) {
	s := makeSpec(name)
	c.Put(&s)
}

func TestCache_GetPut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		putSpecs []entity.SubAgentSpec
		getName  string
		wantOK   bool
	}{
		{
			name:     "hit after put",
			putSpecs: []entity.SubAgentSpec{makeSpec("agent-a")},
			getName:  "agent-a",
			wantOK:   true,
		},
		{
			name:     "miss for unknown name",
			putSpecs: []entity.SubAgentSpec{makeSpec("agent-a")},
			getName:  "agent-b",
			wantOK:   false,
		},
		{
			name:     "empty cache miss",
			putSpecs: nil,
			getName:  "anything",
			wantOK:   false,
		},
		{
			name: "overwrite existing",
			putSpecs: []entity.SubAgentSpec{
				{Name: "agent-a", Description: "first", Instruction: "v1"},
				{Name: "agent-a", Description: "second", Instruction: "v2"},
			},
			getName: "agent-a",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCache(WithTTL(1*time.Hour), WithCleanupInterval(0))
			defer c.Close()

			for i := range tt.putSpecs {
				c.Put(&tt.putSpecs[i])
			}

			spec, ok := c.Get(tt.getName)
			if ok != tt.wantOK {
				t.Fatalf("Get(%q) ok = %v, want %v", tt.getName, ok, tt.wantOK)
			}
			if ok && spec.Name != tt.getName {
				t.Errorf("Get(%q).Name = %q", tt.getName, spec.Name)
			}
		})
	}
}

func TestCache_HitCount(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(1*time.Hour), WithCleanupInterval(0))
	defer c.Close()

	s := makeSpec("counter-agent")
	c.Put(&s)

	for i := 0; i < 5; i++ {
		_, ok := c.Get("counter-agent")
		if !ok {
			t.Fatalf("Get returned false on iteration %d", i)
		}
	}

	c.mu.RLock()
	entry := c.entries["counter-agent"]
	c.mu.RUnlock()

	if got := entry.HitCount.Load(); got != 5 {
		t.Errorf("HitCount = %d, want 5", got)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(50*time.Millisecond), WithCleanupInterval(0))
	defer c.Close()

	putSpec(c, "short-lived")

	if _, ok := c.Get("short-lived"); !ok {
		t.Fatal("expected hit immediately after put")
	}

	time.Sleep(100 * time.Millisecond)

	if _, ok := c.Get("short-lived"); ok {
		t.Error("expected miss after TTL expiry")
	}

	if c.Size() != 0 {
		t.Errorf("Size() = %d after expired get, want 0", c.Size())
	}
}

func TestCache_ZeroTTL(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(0), WithCleanupInterval(0))
	defer c.Close()

	putSpec(c, "instant-expire")

	time.Sleep(time.Millisecond)

	if _, ok := c.Get("instant-expire"); ok {
		t.Error("expected miss with zero TTL")
	}
}

func TestCache_Evict(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(50*time.Millisecond), WithCleanupInterval(0))
	defer c.Close()

	putSpec(c, "evict-a")
	putSpec(c, "evict-b")

	time.Sleep(100 * time.Millisecond)

	putSpec(c, "fresh")

	evicted := c.Evict()
	if evicted != 2 {
		t.Errorf("Evict() = %d, want 2", evicted)
	}

	if c.Size() != 1 {
		t.Errorf("Size() = %d after evict, want 1", c.Size())
	}

	if _, ok := c.Get("fresh"); !ok {
		t.Error("fresh entry should still be accessible")
	}
}

func TestCache_Concurrent(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(1*time.Second), WithCleanupInterval(0))
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			spec := entity.SubAgentSpec{
				Name:        "concurrent-agent",
				Description: "Concurrent test.",
				Instruction: "Instruction.",
				ModelTier:   valueobject.ModelTierFast,
			}
			switch idx % 5 {
			case 0:
				c.Put(&spec)
			case 1:
				c.Get("concurrent-agent")
			case 2:
				c.Size()
			case 3:
				c.Evict()
			case 4:
				c.Get("nonexistent")
			}
		}(i)
	}
	wg.Wait()
}

func TestCache_OverwriteResetsExpiry(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(100*time.Millisecond), WithCleanupInterval(0))
	defer c.Close()

	putSpec(c, "refresh-me")
	time.Sleep(60 * time.Millisecond)

	putSpec(c, "refresh-me")
	time.Sleep(60 * time.Millisecond)

	if _, ok := c.Get("refresh-me"); !ok {
		t.Error("expected hit after overwrite refreshed TTL")
	}
}

func TestCache_CloseIdempotent(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(1*time.Hour), WithCleanupInterval(100*time.Millisecond))
	c.Close()
	c.Close()
}

func TestCache_CloseConcurrent(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(1*time.Hour), WithCleanupInterval(100*time.Millisecond))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
}

func TestCache_GetDoesNotDeleteFreshEntryAfterConcurrentPut(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(50*time.Millisecond), WithCleanupInterval(0))
	defer c.Close()

	putSpec(c, "race-target")
	time.Sleep(80 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		putSpec(c, "race-target")
	}()

	go func() {
		defer wg.Done()
		c.Get("race-target")
	}()

	wg.Wait()

	time.Sleep(10 * time.Millisecond)
	putSpec(c, "race-target")

	if _, ok := c.Get("race-target"); !ok {
		t.Error("fresh entry should survive concurrent Get of expired entry")
	}
}

func TestCache_JanitorEvictsExpired(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(50*time.Millisecond), WithCleanupInterval(100*time.Millisecond))
	defer c.Close()

	putSpec(c, "janitor-target")

	time.Sleep(250 * time.Millisecond)

	if c.Size() != 0 {
		t.Errorf("Size() = %d after janitor run, want 0", c.Size())
	}
}

func TestCache_Size(t *testing.T) {
	t.Parallel()

	c := NewCache(WithTTL(1*time.Hour), WithCleanupInterval(0))
	defer c.Close()

	if c.Size() != 0 {
		t.Errorf("initial Size() = %d, want 0", c.Size())
	}

	putSpec(c, "size-a")
	putSpec(c, "size-b")

	if c.Size() != 2 {
		t.Errorf("Size() = %d after 2 puts, want 2", c.Size())
	}
}
