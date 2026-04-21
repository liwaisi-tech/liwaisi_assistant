package gate

import (
	"sync"
	"testing"
)

// TestStaticSandboxCapability_RaceFree exercises the cache from many
// goroutines at once to flush out concurrent-map-access panics. Must be
// run under `go test -race` to be meaningful (AC-001 / REQ-FIX-001).
func TestStaticSandboxCapability_RaceFree(t *testing.T) {
	s := NewStaticSandboxCapability()
	runtimes := []string{"bwrap", "firejail", "nsjail", "docker", "podman", "nonexistent-runtime-xyz"}
	const goroutines = 64
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			for j := range iterations {
				s.Available(runtimes[(i+j)%len(runtimes)])
			}
		}(i)
	}
	wg.Wait()
}

// TestStaticSandboxCapability_Memoises verifies the cache short-circuits
// repeat lookups — the probe happens once per runtime.
func TestStaticSandboxCapability_Memoises(t *testing.T) {
	s := NewStaticSandboxCapability().(*staticSandboxCapability)
	// First call populates the cache.
	_ = s.Available("nonexistent-runtime-xyz")

	s.mu.RLock()
	v, ok := s.cache["nonexistent-runtime-xyz"]
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("expected cache hit after first probe")
	}
	if v {
		t.Fatalf("expected nonexistent runtime to be reported unavailable, got true")
	}
}
