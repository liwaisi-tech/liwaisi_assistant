package jit

import (
	"container/list"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// cacheCap bounds per-session cache size (CON-004).
const cacheCap = 64

// Cache is the per-session in-process LRU digest cache (REQ-006, CON-006).
// Zero value is NOT ready to use; always construct via NewCache.
type Cache struct {
	mu  sync.Mutex
	ll  *list.List
	idx map[string]*list.Element
	cap int
}

type cacheEntry struct {
	key               string
	blob              []byte
	draft             cpn.TopologyDraft
	personalityDigest string
}

// NewCache returns a new LRU cache capped at 64 entries.
func NewCache() *Cache {
	return &Cache{
		ll:  list.New(),
		idx: make(map[string]*list.Element),
		cap: cacheCap,
	}
}

// Get returns the blob + draft cached under key, promoting it to
// most-recently-used on hit. Returns ok=false on miss.
func (c *Cache) Get(key string) ([]byte, cpn.TopologyDraft, bool) {
	if c == nil {
		return nil, cpn.TopologyDraft{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.idx[key]
	if !ok {
		return nil, cpn.TopologyDraft{}, false
	}
	c.ll.MoveToFront(el)
	e := el.Value.(*cacheEntry)
	// Return a defensive copy of the blob so callers that mutate it do not
	// corrupt the cache.
	out := make([]byte, len(e.blob))
	copy(out, e.blob)
	return out, e.draft, true
}

// Put stores blob + draft under key, tagged with personalityDigest so
// InvalidateOnDigestChange can evict stale personality entries. Evicts
// least-recently-used entries once size exceeds cap.
func (c *Cache) Put(key string, blob []byte, draft cpn.TopologyDraft, personalityDigest string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.idx[key]; ok {
		c.ll.MoveToFront(el)
		e := el.Value.(*cacheEntry)
		e.blob = append(e.blob[:0], blob...)
		e.draft = draft
		e.personalityDigest = personalityDigest
		return
	}
	blobCopy := make([]byte, len(blob))
	copy(blobCopy, blob)
	e := &cacheEntry{
		key:               key,
		blob:              blobCopy,
		draft:             draft,
		personalityDigest: personalityDigest,
	}
	c.idx[key] = c.ll.PushFront(e)
	for c.ll.Len() > c.cap {
		last := c.ll.Back()
		if last == nil {
			break
		}
		c.ll.Remove(last)
		delete(c.idx, last.Value.(*cacheEntry).key)
	}
}

// InvalidateOnDigestChange drops every entry whose stored personality
// digest differs from newPersonalityDigest. O(n); per-session caches stay
// small enough that this is cheap.
func (c *Cache) InvalidateOnDigestChange(newPersonalityDigest string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var drop []*list.Element
	for el := c.ll.Front(); el != nil; el = el.Next() {
		e := el.Value.(*cacheEntry)
		if e.personalityDigest != newPersonalityDigest {
			drop = append(drop, el)
		}
	}
	for _, el := range drop {
		c.ll.Remove(el)
		delete(c.idx, el.Value.(*cacheEntry).key)
	}
}

// Len returns the current entry count.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
