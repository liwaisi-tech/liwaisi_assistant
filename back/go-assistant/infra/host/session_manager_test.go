//go:build linux

package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestSessionManager_OpenWriteKill verifies the basic lifecycle of a PTY
// session and that no goroutines leak afterwards.
func TestSessionManager_OpenWriteKill(t *testing.T) {
	defer goleak.VerifyNone(t,
		// creack/pty sometimes keeps its own goroutines alive across
		// test cases on older runtimes; we allow stdlib pool noise.
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	mgr := NewInMemoryBashSessionManager(nil)

	ctx := context.Background()
	id, err := mgr.Open(ctx, cpn.PTYRequest{Command: "bash"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Consume status/chunks so readLoop can proceed even if the buffer fills.
	chunks, _ := mgr.Chunks(id)
	status, _ := mgr.Status(id)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range chunks {
		}
		for range status {
		}
	}()

	if err := mgr.Write(ctx, id, []byte("echo hi\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Give bash a moment to produce output.
	time.Sleep(200 * time.Millisecond)

	if err := mgr.Kill(ctx, id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	wg.Wait()

	// Session should be removed from the map.
	if got := mgr.List(ctx); len(got) != 0 {
		t.Fatalf("expected 0 sessions after kill, got %d", len(got))
	}
}

// TestSessionManager_WriteUnknown returns ErrSessionNotFound.
func TestSessionManager_WriteUnknown(t *testing.T) {
	mgr := NewInMemoryBashSessionManager(nil)
	err := mgr.Write(context.Background(), "nope", []byte("x"))
	if err != cpn.ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// TestSessionManager_ConcurrentWrites ensures concurrent writes from
// multiple goroutines do not race.
func TestSessionManager_ConcurrentWrites(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	mgr := NewInMemoryBashSessionManager(nil)
	ctx := context.Background()
	id, err := mgr.Open(ctx, cpn.PTYRequest{Command: "bash"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	chunks, _ := mgr.Chunks(id)
	status, _ := mgr.Status(id)
	go func() {
		for range chunks {
		}
	}()
	go func() {
		for range status {
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = mgr.Write(ctx, id, []byte("echo "+strings.Repeat("x", 5)+"\n"))
		}(i)
	}
	wg.Wait()
	if err := mgr.Kill(ctx, id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
}

// TestSessionManager_ContextCancelCleansUp ensures that when the caller
// context is cancelled the session is torn down.
func TestSessionManager_ContextCancelCleansUp(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	mgr := NewInMemoryBashSessionManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	id, err := mgr.Open(ctx, cpn.PTYRequest{Command: "bash"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	chunks, _ := mgr.Chunks(id)
	status, _ := mgr.Status(id)
	go func() {
		for range chunks {
		}
	}()
	go func() {
		for range status {
		}
	}()
	cancel()
	// Give the watcher goroutine a moment to issue Kill.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.List(context.Background())) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session was not cleaned up within deadline")
}
