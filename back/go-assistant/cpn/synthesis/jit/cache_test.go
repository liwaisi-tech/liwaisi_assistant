package jit

import (
	"strconv"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestCache_BasicPutGet(t *testing.T) {
	c := NewCache()
	c.Put("k1", []byte("blob1"), cpn.TopologyDraft{ID: "a"}, "p1")
	blob, draft, ok := c.Get("k1")
	if !ok {
		t.Fatalf("expected hit")
	}
	if string(blob) != "blob1" {
		t.Errorf("blob = %q", blob)
	}
	if draft.ID != "a" {
		t.Errorf("draft.ID = %q", draft.ID)
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache()
	for i := 0; i < cacheCap+10; i++ {
		k := "k" + strconv.Itoa(i)
		c.Put(k, []byte(k), cpn.TopologyDraft{ID: k}, "p")
	}
	if c.Len() != cacheCap {
		t.Fatalf("len = %d, want %d", c.Len(), cacheCap)
	}
	// k0 should be evicted
	if _, _, ok := c.Get("k0"); ok {
		t.Errorf("k0 should be evicted")
	}
	if _, _, ok := c.Get("k" + strconv.Itoa(cacheCap+9)); !ok {
		t.Errorf("most-recent key missing")
	}
}

func TestCache_InvalidateOnDigestChange(t *testing.T) {
	c := NewCache()
	c.Put("k1", []byte("b1"), cpn.TopologyDraft{}, "alpha")
	c.Put("k2", []byte("b2"), cpn.TopologyDraft{}, "alpha")
	c.Put("k3", []byte("b3"), cpn.TopologyDraft{}, "beta")
	c.InvalidateOnDigestChange("alpha")
	if _, _, ok := c.Get("k3"); ok {
		t.Errorf("beta entry should be dropped")
	}
	if _, _, ok := c.Get("k1"); !ok {
		t.Errorf("alpha entry should survive")
	}
}

func TestCache_NilSafe(t *testing.T) {
	var c *Cache
	c.Put("k", nil, cpn.TopologyDraft{}, "")
	if _, _, ok := c.Get("k"); ok {
		t.Errorf("nil cache should miss")
	}
	c.InvalidateOnDigestChange("x") // no panic
	if c.Len() != 0 {
		t.Errorf("nil cache len should be 0")
	}
}
