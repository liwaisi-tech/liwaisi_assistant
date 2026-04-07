package config

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// memStore is an in-memory Store for tests.
type memStore struct {
	mu sync.Mutex
	m  map[string]*Entry
}

func newMemStore() *memStore { return &memStore{m: make(map[string]*Entry)} }

func (s *memStore) Get(_ context.Context, k string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.m[k]; ok {
		return e, nil
	}
	return nil, ErrNotFound
}
func (s *memStore) Set(_ context.Context, e *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[e.Key] = e
	return nil
}
func (s *memStore) Delete(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
func (s *memStore) List(_ context.Context) ([]*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Entry, 0, len(s.m))
	for _, e := range s.m {
		out = append(out, e)
	}
	return out, nil
}

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{"abc", "********"},
		{"abcdefg", "********"},
		{"abcdefgh", "********efgh"},
		{"sk-or-v1-abcdef1234", "********1234"},
	}
	for _, c := range cases {
		if got := maskSecret(c.in); got != c.out {
			t.Errorf("maskSecret(%q)=%q want %q", c.in, got, c.out)
		}
	}
}

// AC-011: callbacks for sequential Sets observe the latest value last.
func TestProvider_Set_OnChangeOrdering(t *testing.T) {
	p := NewProvider(newMemStore())
	var last atomic.Value
	last.Store("")
	p.OnChange(func(_, v string) { last.Store(v) })
	ctx := context.Background()

	if err := p.Set(ctx, "openrouter_api_key", "v1", ""); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(ctx, "openrouter_api_key", "v2", ""); err != nil {
		t.Fatal(err)
	}
	if got := last.Load().(string); got != "v2" {
		t.Errorf("last observed = %q want v2", got)
	}
}

// REQ-109: race test exercising concurrent Set/OnChange/Get.
func TestProvider_Race(t *testing.T) {
	p := NewProvider(newMemStore())
	ctx := context.Background()
	const N = 120

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = p.Set(ctx, "openrouter_api_key", "x", "tester")
		}()
		go func() {
			defer wg.Done()
			p.OnChange(func(_, _ string) {})
		}()
		go func() {
			defer wg.Done()
			_ = p.Get("openrouter_api_key")
		}()
	}
	wg.Wait()
}
