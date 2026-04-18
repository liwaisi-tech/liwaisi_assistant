package persist

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeFS is an in-memory FSMover for rollback/restore tests.
type fakeFS struct {
	mu       sync.Mutex
	files    map[string][]byte // full absolute path → content (for leaves)
	dirs     map[string]struct{}
	failOn   string            // path prefix that Move should fail on
	failKind string            // "src" or "dst" — which side triggers the fail
	moves    []string          // audit trail for assertions
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files: make(map[string][]byte),
		dirs:  make(map[string]struct{}),
	}
}

func (f *fakeFS) addFile(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = []byte("x")
	f.dirs[filepath.Dir(path)] = struct{}{}
}

func (f *fakeFS) Move(src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != "" {
		if f.failKind == "src" && strings.HasPrefix(src, f.failOn) {
			return errors.New("fake: move failed (src)")
		}
		if f.failKind == "dst" && strings.HasPrefix(dst, f.failOn) {
			return errors.New("fake: move failed (dst)")
		}
	}
	data, ok := f.files[src]
	if ok {
		delete(f.files, src)
		f.files[dst] = data
		f.dirs[filepath.Dir(dst)] = struct{}{}
		f.moves = append(f.moves, src+"→"+dst)
		return nil
	}
	// Directory rename — move every file under src to dst.
	if _, isDir := f.dirs[src]; isDir {
		for p, data := range f.files {
			if strings.HasPrefix(p, src+string(filepath.Separator)) {
				rel := strings.TrimPrefix(p, src+string(filepath.Separator))
				newPath := filepath.Join(dst, rel)
				delete(f.files, p)
				f.files[newPath] = data
				f.dirs[filepath.Dir(newPath)] = struct{}{}
			}
		}
		delete(f.dirs, src)
		f.dirs[dst] = struct{}{}
		f.moves = append(f.moves, src+"→"+dst)
		return nil
	}
	return errors.New("fake: source not found: " + src)
}

func (f *fakeFS) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, path)
	return nil
}

func (f *fakeFS) MkdirAll(path string, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[path] = struct{}{}
	return nil
}

func (f *fakeFS) RemoveAll(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for p := range f.files {
		if strings.HasPrefix(p, path) {
			delete(f.files, p)
		}
	}
	for d := range f.dirs {
		if strings.HasPrefix(d, path) {
			delete(f.dirs, d)
		}
	}
	return nil
}

func (f *fakeFS) Exists(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[path]; ok {
		return true
	}
	_, ok := f.dirs[path]
	return ok
}

// ── Test doubles for the tool deprecator ────────────────────────────────

type fakeTools struct {
	registered map[string]string // binary_path → qualified_name
	deprecated []string          // qualified_names deprecated
}

func (f *fakeTools) Deprecate(_ context.Context, qn, _ string) error {
	f.deprecated = append(f.deprecated, qn)
	return nil
}

func (f *fakeTools) LookupByBinaryPath(_ context.Context, p string) (string, bool) {
	qn, ok := f.registered[p]
	return qn, ok
}

// ── Tests ───────────────────────────────────────────────────────────────

func TestRollbackService_HappyPath(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	paths := []string{
		filepath.Join(root, "src", "a.go"),
		filepath.Join(root, "src", "b.go"),
	}
	for _, p := range paths {
		fs.addFile(p)
		id, err := ledger.PreWrite(ctx, WriteIntent{SetID: "s1", Classification: ClassSource}, p, 0o644)
		if err != nil {
			t.Fatalf("PreWrite: %v", err)
		}
		_ = ledger.PostWrite(ctx, id, "h", 1, "t")
	}

	svc := &RollbackService{
		Ledger:     ledger,
		FS:         fs,
		AllowedRoot: root,
		Tools:      NoopToolDeprecator{},
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	if err := svc.Rollback(ctx, "s1", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Files are no longer at their original paths.
	for _, p := range paths {
		if fs.Exists(p) {
			t.Errorf("path %q still exists after rollback", p)
		}
	}
	// Final quarantine directory exists.
	if !fs.Exists(filepath.Join(root, "quarantine", "s1")) {
		t.Errorf("final quarantine dir missing")
	}
	// Ledger state is quarantined.
	arts, _ := ledger.SetForSet(ctx, "s1")
	for _, a := range arts {
		if a.State != ArtefactStateQuarantined {
			t.Errorf("state = %q; want quarantined", a.State)
		}
	}
}

func TestRollbackService_ReversesOnFailure(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	paths := []string{
		filepath.Join(root, "src", "a.go"),
		filepath.Join(root, "src", "b.go"), // moving this one will fail.
	}
	for _, p := range paths {
		fs.addFile(p)
		id, _ := ledger.PreWrite(ctx, WriteIntent{SetID: "s1", Classification: ClassSource}, p, 0o644)
		_ = ledger.PostWrite(ctx, id, "h", 1, "t")
	}
	// Trigger failure when the second file is moved (dst inside staging).
	fs.failOn = filepath.Join(root, "quarantine", ".staging-s1", "src", "b.go")
	fs.failKind = "dst"

	svc := &RollbackService{
		Ledger:     ledger,
		FS:         fs,
		AllowedRoot: root,
		Tools:      NoopToolDeprecator{},
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	err := svc.Rollback(ctx, "s1", "alice")
	if err == nil {
		t.Fatal("expected rollback error, got nil")
	}

	// Both files must be back at their original paths after reversal.
	for _, p := range paths {
		if !fs.Exists(p) {
			t.Errorf("path %q not restored after failure reversal", p)
		}
	}
	// Ledger must NOT be flipped to quarantined.
	arts, _ := ledger.SetForSet(ctx, "s1")
	for _, a := range arts {
		if a.State == ArtefactStateQuarantined {
			t.Errorf("ledger prematurely flipped for %q", a.Path)
		}
	}
}

func TestRollbackService_DeprecatesRegisteredTools(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	binPath := filepath.Join(root, "bin", "my-tool")
	fs.addFile(binPath)
	id, _ := ledger.PreWrite(ctx, WriteIntent{SetID: "s1", Classification: ClassBinary}, binPath, 0o755)
	_ = ledger.PostWrite(ctx, id, "h", 1, "application/octet-stream")

	tools := &fakeTools{
		registered: map[string]string{binPath: "brae/my-tool@1.0.0"},
	}
	svc := &RollbackService{
		Ledger:     ledger,
		FS:         fs,
		AllowedRoot: root,
		Tools:      tools,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	if err := svc.Rollback(ctx, "s1", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(tools.deprecated) != 1 || tools.deprecated[0] != "brae/my-tool@1.0.0" {
		t.Errorf("deprecated = %v; want [brae/my-tool@1.0.0]", tools.deprecated)
	}
}

func TestRollbackService_RestoreHappyPath(t *testing.T) {
	ctx := context.Background()
	ledger := NewMemoryArtefactLedger()
	fs := newFakeFS()
	const root = "/home/u/.local/brae"

	path := filepath.Join(root, "src", "a.go")
	fs.addFile(path)
	id, _ := ledger.PreWrite(ctx, WriteIntent{SetID: "s1", Classification: ClassSource}, path, 0o644)
	_ = ledger.PostWrite(ctx, id, "h", 1, "t")

	svc := &RollbackService{
		Ledger:     ledger,
		FS:         fs,
		AllowedRoot: root,
		Tools:      NoopToolDeprecator{},
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	if err := svc.Rollback(ctx, "s1", "alice"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Now restore.
	if err := svc.Restore(ctx, "s1", "alice"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !fs.Exists(path) {
		t.Errorf("path %q not restored", path)
	}
	arts, _ := ledger.SetForSet(ctx, "s1")
	if arts[0].State != ArtefactStateRestored {
		t.Errorf("state = %q; want restored", arts[0].State)
	}
}
