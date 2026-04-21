package acl

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

const ruleA = `rules:
  - binary: git
    binary_sha256: "1111111111111111111111111111111111111111111111111111111111111111"
    decision: allow
    reason: "A"
`

const ruleB = `rules:
  - binary: git
    binary_sha256: "2222222222222222222222222222222222222222222222222222222222222222"
    decision: deny
    reason: "B"
`

func tmpPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/acl.yaml"
}

func mustWrite(t *testing.T, p, c string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWatcher_InitialLoad(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, ruleA)
	w, err := NewWatcher(p, slog.Default())
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	snap := w.Snapshot()
	if len(snap.Rules) != 1 || snap.Rules[0].Reason != "A" {
		t.Fatalf("bad initial snapshot: %+v", snap)
	}
}

func TestWatcher_NewWatcher_BadInitialFile(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, "not: [valid")
	if _, err := NewWatcher(p, nil); err == nil {
		t.Fatal("expected error for bad initial file")
	}
}

func TestWatcher_ReloadPicksUpChanges(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, ruleA)
	w, err := NewWatcher(p, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, ruleB)
	if err := w.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	snap := w.Snapshot()
	if snap.Rules[0].Reason != "B" {
		t.Fatalf("reload did not pick up changes: %+v", snap)
	}
}

func TestWatcher_MtimePollingPicksUpChanges(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, ruleA)
	w, err := NewWatcher(p, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	w.SetPollInterval(100 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Ensure mtime differs.
	time.Sleep(50 * time.Millisecond)
	newTime := time.Now().Add(2 * time.Second)
	mustWrite(t, p, ruleB)
	_ = os.Chtimes(p, newTime, newTime)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if w.Snapshot().Rules[0].Reason == "B" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("mtime polling did not reload within 10s; snapshot=%+v", w.Snapshot())
}

func TestWatcher_ReloadRejectsInvalid_KeepsPrevious(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, ruleA)
	w, err := NewWatcher(p, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, "rules:\n  - binary: x\n    binary_sha256: \"bad\"\n    decision: allow\n")
	if err := w.Reload(); err == nil {
		t.Fatal("expected reload error")
	}
	snap := w.Snapshot()
	if len(snap.Rules) != 1 || snap.Rules[0].Reason != "A" {
		t.Fatalf("previous snapshot clobbered: %+v", snap)
	}
}

func TestWatcher_SetPollIntervalIgnoresNonPositive(t *testing.T) {
	p := tmpPath(t)
	mustWrite(t, p, ruleA)
	w, err := NewWatcher(p, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	prev := w.pollInterval
	w.SetPollInterval(0)
	w.SetPollInterval(-1)
	if w.pollInterval != prev {
		t.Fatal("non-positive poll interval should be ignored")
	}
}
