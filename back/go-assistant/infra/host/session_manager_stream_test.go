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

// TestSessionManager_EmitPerChunk_AC004 covers spec GAP-9 AC-004:
// given EmitMode=per_chunk with a 1024-byte window, a process that
// prints exactly 4096 bytes yields 4 EventProcessStdout events on the
// session's event sink.
func TestSessionManager_EmitPerChunk_AC004(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	mgr := NewInMemoryBashSessionManager(nil)

	// Capture every published event.
	var (
		mu     sync.Mutex
		events []cpn.Event
	)
	sink := func(e cpn.Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	ctx := context.Background()
	// printf %4096s " " emits exactly 4096 space bytes (no newline).
	// We use bash -c to keep the command portable across installs.
	id, err := mgr.OpenWithEventsAndMode(
		ctx,
		cpn.PTYRequest{
			Command: "bash",
			Args:    []string{"-c", "printf '%*s' 4096 ' '"},
		},
		sink,
		cpn.EmitPerChunk,
		1024,
	)
	if err != nil {
		t.Fatalf("OpenWithEventsAndMode: %v", err)
	}

	// Drain chunks/status so the read loop and wait loop can finish.
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

	// Wait up to 3s for the session to finish.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		haveExit := false
		for _, e := range events {
			if e.Type == cpn.EventProcessExit {
				haveExit = true
				break
			}
		}
		mu.Unlock()
		if haveExit {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Clean up.
	_ = mgr.Close(ctx, id)
	wg.Wait()

	// Count stdout events.
	mu.Lock()
	defer mu.Unlock()
	var stdoutCount, exitCount, startedCount int
	for _, e := range events {
		switch e.Type {
		case cpn.EventProcessStdout:
			stdoutCount++
		case cpn.EventProcessExit:
			exitCount++
		case cpn.EventProcessStarted:
			startedCount++
		}
	}

	if startedCount != 1 {
		t.Errorf("started events = %d, want 1", startedCount)
	}
	if exitCount != 1 {
		t.Errorf("exit events = %d, want 1", exitCount)
	}
	// The 4096-byte payload split into 1024-byte chunks yields 4
	// events. Terminal echoes or PTY control sequences may add a
	// small number of trailing bytes (the shell prompt). We assert
	// AT LEAST 4 stdout events with at least 4 of them being full
	// 1024-byte fragments.
	fullChunks := 0
	for _, e := range events {
		if e.Type != cpn.EventProcessStdout {
			continue
		}
		payload, ok := e.Payload.(cpn.ProcessOutputPayload)
		if !ok {
			continue
		}
		if len(payload.Line) == 1024 {
			fullChunks++
		}
	}
	if fullChunks < 4 {
		t.Fatalf("full 1024-byte chunks = %d, want >= 4 (total stdout events = %d)", fullChunks, stdoutCount)
	}
}

// TestSessionManager_EmitPerLine_Default verifies that the default
// (EmitPerLine) mode still emits one event per newline-terminated line.
// This is the behaviour GAP-1 depended on.
func TestSessionManager_EmitPerLine_Default(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	mgr := NewInMemoryBashSessionManager(nil)

	var (
		mu     sync.Mutex
		events []cpn.Event
	)
	sink := func(e cpn.Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	ctx := context.Background()
	id, err := mgr.OpenWithEvents(
		ctx,
		cpn.PTYRequest{
			Command: "bash",
			Args:    []string{"-c", "for i in 1 2 3; do echo line-$i; done"},
		},
		sink,
	)
	if err != nil {
		t.Fatalf("OpenWithEvents: %v", err)
	}

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

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		haveExit := false
		for _, e := range events {
			if e.Type == cpn.EventProcessExit {
				haveExit = true
				break
			}
		}
		mu.Unlock()
		if haveExit {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = mgr.Close(ctx, id)
	wg.Wait()

	// Expect exactly 3 EventProcessStdout payloads carrying "line-1",
	// "line-2", "line-3" (order preserved).
	mu.Lock()
	defer mu.Unlock()
	var lines []string
	for _, e := range events {
		if e.Type != cpn.EventProcessStdout {
			continue
		}
		payload, ok := e.Payload.(cpn.ProcessOutputPayload)
		if !ok {
			continue
		}
		if strings.HasPrefix(payload.Line, "line-") {
			lines = append(lines, payload.Line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("line-* events = %d, want 3 (captured=%v)", len(lines), lines)
	}
	for i, want := range []string{"line-1", "line-2", "line-3"} {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want)
		}
	}
}
