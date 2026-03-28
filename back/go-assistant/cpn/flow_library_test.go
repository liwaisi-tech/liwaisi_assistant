package cpn

import (
	"sync"
	"testing"
)

func TestFlowLibrary_RegisterAndGet(t *testing.T) {
	fl := NewFlowLibrary()
	cpn := &CPN{ID: "cpn-1", Role: "analyst"}

	fl.Register(cpn, "hash-abc")

	got, ok := fl.Get("hash-abc")
	if !ok {
		t.Fatal("Get returned false for registered hash")
	}
	if got.ID != "cpn-1" {
		t.Errorf("got.ID = %q, want %q", got.ID, "cpn-1")
	}
	if fl.Len() != 1 {
		t.Errorf("Len = %d, want 1", fl.Len())
	}
}

func TestFlowLibrary_GetNotFound(t *testing.T) {
	fl := NewFlowLibrary()

	got, ok := fl.Get("nonexistent")
	if ok {
		t.Error("Get returned true for unknown hash")
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestFlowLibrary_Register_Overwrite(t *testing.T) {
	fl := NewFlowLibrary()
	fl.Register(&CPN{ID: "cpn-old"}, "hash-1")
	fl.Register(&CPN{ID: "cpn-new"}, "hash-1")

	got, ok := fl.Get("hash-1")
	if !ok {
		t.Fatal("Get returned false after overwrite")
	}
	if got.ID != "cpn-new" {
		t.Errorf("got.ID = %q, want %q (last write wins)", got.ID, "cpn-new")
	}
	if fl.Len() != 1 {
		t.Errorf("Len = %d, want 1 (overwrite, not duplicate)", fl.Len())
	}
}

func TestFlowLibrary_Propose_ReturnsNil(t *testing.T) {
	fl := NewFlowLibrary()
	fl.Register(&CPN{ID: "cpn-1"}, "hash-1")

	result := fl.Propose("client-1")
	if result != nil {
		t.Errorf("Propose = %v, want nil (Phase 1 stub)", result)
	}
}

func TestFlowLibrary_ConcurrentAccess(t *testing.T) {
	fl := NewFlowLibrary()
	const goroutines = 500

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers
	for i := range goroutines {
		go func() {
			defer wg.Done()
			cpn := &CPN{ID: "cpn-write"}
			fl.Register(cpn, "hash-concurrent")
		}()
		_ = i
	}

	// Readers
	for i := range goroutines {
		go func() {
			defer wg.Done()
			fl.Get("hash-concurrent")
		}()
		_ = i
	}

	wg.Wait()

	// At least the last write should be present.
	_, ok := fl.Get("hash-concurrent")
	if !ok {
		t.Error("Get returned false after concurrent writes")
	}
}
