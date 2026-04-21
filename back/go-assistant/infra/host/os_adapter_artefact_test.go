//go:build linux

package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func TestOSHostAdapter_WriteFile_BackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil)
	a.AllowedRoot = dir

	path := filepath.Join(dir, "a.txt")
	if err := a.WriteFile(context.Background(), path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile without ledger: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q; want %q", got, "hello")
	}
}

func TestOSHostAdapter_WriteFile_RecordsArtefact(t *testing.T) {
	dir := t.TempDir()
	ledger := persist.NewMemoryArtefactLedger()
	a := NewOSHostAdapter(nil, WithArtefactLedger(ledger))
	a.AllowedRoot = dir

	path := filepath.Join(dir, "pkg", "main.go")
	ctx := cpn.WithWriteIntent(context.Background(), persist.WriteIntent{
		SetID:          "unit-test-set",
		ForgeRunID:     "unit-test-run",
		Classification: persist.ClassSource,
	})
	if err := a.WriteFile(ctx, path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	arts, err := ledger.SetForSet(context.Background(), "unit-test-set")
	if err != nil {
		t.Fatalf("SetForSet: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("count = %d; want 1", len(arts))
	}
	art := arts[0]
	if art.State != persist.ArtefactStateActive {
		t.Errorf("state = %q; want active", art.State)
	}
	if art.Size != int64(len("package main\n")) {
		t.Errorf("size = %d; want %d", art.Size, len("package main\n"))
	}
	if art.SHA256 == "" {
		t.Errorf("sha256 empty")
	}
	if art.Classification != persist.ClassSource {
		t.Errorf("classification = %q; want source", art.Classification)
	}
	if art.MIME == "" {
		t.Errorf("mime empty")
	}
}

func TestOSHostAdapter_WriteFile_AutoClassifies(t *testing.T) {
	dir := t.TempDir()
	ledger := persist.NewMemoryArtefactLedger()
	a := NewOSHostAdapter(nil, WithArtefactLedger(ledger))
	a.AllowedRoot = dir

	// No intent attached → host synthesises one and uses Classify(path,
	// mode) to pick the classification.
	path := filepath.Join(dir, "scripts", "run.py")
	if err := a.WriteFile(context.Background(), path, []byte("print('hi')"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	arts, err := ledger.ListByHost(context.Background(), "", persist.ArtefactFilter{})
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("count = %d; want 1", len(arts))
	}
	if arts[0].Classification != persist.ClassSource {
		t.Errorf("classification = %q; want source (auto)", arts[0].Classification)
	}
}

// abortingLedger implements just enough of AuthoredArtefactLedger to
// exercise the "PreWrite returns error → abort disk write" path.
type abortingLedger struct {
	persist.AuthoredArtefactLedger
}

func (abortingLedger) PreWrite(context.Context, persist.WriteIntent, string, uint32) (string, error) {
	return "", errors.New("ledger: refused")
}

func TestOSHostAdapter_WriteFile_PreWriteFailureAborts(t *testing.T) {
	dir := t.TempDir()
	a := NewOSHostAdapter(nil, WithArtefactLedger(abortingLedger{}))
	a.AllowedRoot = dir

	path := filepath.Join(dir, "aborted.txt")
	err := a.WriteFile(context.Background(), path, []byte("should not be written"), 0o644)
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("file %q should NOT exist after PreWrite failure", path)
	}
}
