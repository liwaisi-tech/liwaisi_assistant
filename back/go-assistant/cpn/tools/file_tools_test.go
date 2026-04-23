package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Mocks ────────────────────────────────────────────────────────────────────

// fakeHost is an in-memory HostAdapter for unit tests. It records every
// WriteFile call so tests can assert on side-effects, and it lets a test
// inject a per-path error to simulate ENOENT, EACCES, etc.
type fakeHost struct {
	mu       sync.Mutex
	files    map[string][]byte
	readErr  map[string]error
	writeErr map[string]error
	writes   []writeEvent
}

type writeEvent struct {
	path string
	body []byte
	mode fs.FileMode
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		files:    make(map[string][]byte),
		readErr:  make(map[string]error),
		writeErr: make(map[string]error),
	}
}

// HostAdapter implementation — only the methods used by file tools are
// implemented. The rest panic loudly if accidentally called from a test.

func (f *fakeHost) ReadFile(_ context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.readErr[path]; ok {
		return nil, err
	}
	body, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

func (f *fakeHost) WriteFile(_ context.Context, path string, data []byte, mode fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.writeErr[path]; ok {
		return err
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	f.files[path] = cp
	f.writes = append(f.writes, writeEvent{path: path, body: cp, mode: mode})
	return nil
}

func (f *fakeHost) Exec(context.Context, cpn.ExecRequest) (cpn.ExecResult, error) {
	panic("fakeHost.Exec called by file tool — not allowed")
}
func (f *fakeHost) SpawnPTY(context.Context, cpn.PTYRequest) (cpn.PTYHandle, error) {
	panic("fakeHost.SpawnPTY called by file tool — not allowed")
}
func (f *fakeHost) KillPID(context.Context, int, cpn.Signal) error {
	panic("fakeHost.KillPID called by file tool — not allowed")
}
func (f *fakeHost) Stat(_ context.Context, path string) (cpn.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.files[path]
	if !ok {
		return cpn.FileInfo{}, fs.ErrNotExist
	}
	return cpn.FileInfo{Path: path, Size: int64(len(body))}, nil
}

// recordingSink is a LedgerSink that captures every appended message
// for inspection. Safe for concurrent Append.
type recordingSink struct {
	mu       sync.Mutex
	captured []*cpn.Message
}

func (s *recordingSink) Append(m *cpn.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captured = append(s.captured, m)
}

func (s *recordingSink) snapshot() []*cpn.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*cpn.Message, len(s.captured))
	copy(out, s.captured)
	return out
}

// argsToken builds a Token whose Payload is JSON-encoded args.
func argsToken(t *testing.T, v any) cpn.Token {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return cpn.Token{Color: cpn.ColorJSON, Payload: json.RawMessage(raw)}
}

// resultOf decodes a Token payload as JSON into v.
func resultOf(t *testing.T, tok cpn.Token, v any) {
	t.Helper()
	switch p := tok.Payload.(type) {
	case json.RawMessage:
		if err := json.Unmarshal(p, v); err != nil {
			t.Fatalf("unmarshal result: %v\npayload=%s", err, p)
		}
	default:
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal result for re-decode: %v", err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("unmarshal result: %v\nraw=%s", err, raw)
		}
	}
}

// ── RegisterFileTools ────────────────────────────────────────────────────────

func TestRegisterFileTools_ValidatesDeps(t *testing.T) {
	cases := []struct {
		name string
		reg  *Registry
		deps *FileToolDeps
		ok   bool
	}{
		{"nil registry", nil, &FileToolDeps{Host: newFakeHost()}, false},
		{"nil deps", NewRegistry(), nil, false},
		{"nil host", NewRegistry(), &FileToolDeps{}, false},
		{"happy", NewRegistry(), &FileToolDeps{Host: newFakeHost()}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RegisterFileTools(tc.reg, tc.deps)
			gotOK := err == nil
			if gotOK != tc.ok {
				t.Errorf("ok=%v err=%v want ok=%v", gotOK, err, tc.ok)
			}
		})
	}
}

func TestRegisterFileTools_RegistersAllThree(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterFileTools(reg, &FileToolDeps{Host: newFakeHost()}); err != nil {
		t.Fatalf("RegisterFileTools: %v", err)
	}
	for _, qn := range []string{"file/read_file", "file/write_file", "file/edit_file"} {
		if _, err := reg.Get(context.Background(), qn); err != nil {
			t.Errorf("expected tool %s registered, got: %v", qn, err)
		}
	}
}

// ── read_file ───────────────────────────────────────────────────────────────

func TestReadFile_HappyPath(t *testing.T) {
	host := newFakeHost()
	host.files["/tmp/x.txt"] = []byte("hello world\n")
	exec := makeReadFile(&FileToolDeps{Host: host})

	tok, err := exec(context.Background(), argsToken(t, readFileInput{Path: "/tmp/x.txt"}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var got ReadFileResult
	resultOf(t, tok, &got)

	want := ReadFileResult{
		Path:    "/tmp/x.txt",
		Bytes:   12,
		Content: "hello world\n",
		SHA256:  "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestReadFile_MissingPath(t *testing.T) {
	exec := makeReadFile(&FileToolDeps{Host: newFakeHost()})
	_, err := exec(context.Background(), argsToken(t, readFileInput{}))
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Errorf("expected path-required error, got %v", err)
	}
}

func TestReadFile_HostError(t *testing.T) {
	host := newFakeHost()
	host.readErr["/tmp/missing"] = fs.ErrNotExist
	exec := makeReadFile(&FileToolDeps{Host: host})
	_, err := exec(context.Background(), argsToken(t, readFileInput{Path: "/tmp/missing"}))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected wrapped ErrNotExist, got %v", err)
	}
}

func TestReadFile_BadPayload(t *testing.T) {
	exec := makeReadFile(&FileToolDeps{Host: newFakeHost()})
	_, err := exec(context.Background(), cpn.Token{Color: cpn.ColorJSON, Payload: nil})
	if err == nil {
		t.Errorf("expected nil-payload error")
	}
}

// ── write_file ──────────────────────────────────────────────────────────────

func TestWriteFile_NewFile_EmitsCreatedAndLedger(t *testing.T) {
	host := newFakeHost()
	sink := &recordingSink{}
	exec := makeWriteFile(&FileToolDeps{Host: host})

	ctx := cpn.WithLedgerSink(context.Background(), sink)
	tok, err := exec(ctx, argsToken(t, writeFileInput{
		Path: "/tmp/new.txt", Content: "alpha\nbeta\n",
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var got WriteFileResult
	resultOf(t, tok, &got)

	if !got.Created {
		t.Errorf("expected Created=true on new file")
	}
	if got.NoOp {
		t.Errorf("did not expect NoOp on new file")
	}
	if got.Lines != 2 {
		t.Errorf("Lines=%d want 2", got.Lines)
	}
	if got.Bytes != 11 {
		t.Errorf("Bytes=%d want 11", got.Bytes)
	}

	// Ledger entry: success, write verb, target = path.
	captured := sink.snapshot()
	if len(captured) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(captured))
	}
	if !cpn.IsLedgerEntry(captured[0]) {
		t.Errorf("captured message is not a ledger entry: %+v", captured[0])
	}
	if !strings.HasPrefix(captured[0].Content, "[ok] write /tmp/new.txt") {
		t.Errorf("unexpected ledger content: %q", captured[0].Content)
	}
}

func TestWriteFile_OverwriteEmitsCreatedFalse(t *testing.T) {
	host := newFakeHost()
	host.files["/tmp/existing.txt"] = []byte("old")
	exec := makeWriteFile(&FileToolDeps{Host: host})

	tok, err := exec(context.Background(), argsToken(t, writeFileInput{
		Path: "/tmp/existing.txt", Content: "new content",
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var got WriteFileResult
	resultOf(t, tok, &got)
	if got.Created {
		t.Errorf("expected Created=false on overwrite")
	}
	if got.NoOp {
		t.Errorf("did not expect NoOp on actual change")
	}
}

func TestWriteFile_IdenticalContentIsNoOp(t *testing.T) {
	host := newFakeHost()
	host.files["/tmp/same.txt"] = []byte("same content")
	sink := &recordingSink{}
	exec := makeWriteFile(&FileToolDeps{Host: host})

	ctx := cpn.WithLedgerSink(context.Background(), sink)
	tok, err := exec(ctx, argsToken(t, writeFileInput{
		Path: "/tmp/same.txt", Content: "same content",
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var got WriteFileResult
	resultOf(t, tok, &got)
	if !got.NoOp {
		t.Errorf("expected NoOp=true for identical content")
	}
	if got.Created {
		t.Errorf("did not expect Created on no-op")
	}

	// Critically: the host MUST NOT have been called for write (this is
	// what stops the "wrote the same file 4 times" loop).
	if len(host.writes) != 0 {
		t.Errorf("no-op should not invoke WriteFile; got %d writes", len(host.writes))
	}

	// Ledger entry should record the no-op explicitly.
	captured := sink.snapshot()
	if len(captured) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(captured))
	}
	if !strings.Contains(captured[0].Content, "no-op, identical") {
		t.Errorf("expected no-op marker in ledger: %q", captured[0].Content)
	}
}

func TestWriteFile_HostErrorEmitsFailureLedger(t *testing.T) {
	host := newFakeHost()
	host.writeErr["/tmp/readonly"] = fs.ErrPermission
	sink := &recordingSink{}
	exec := makeWriteFile(&FileToolDeps{Host: host})

	ctx := cpn.WithLedgerSink(context.Background(), sink)
	_, err := exec(ctx, argsToken(t, writeFileInput{
		Path: "/tmp/readonly", Content: "x",
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	captured := sink.snapshot()
	if len(captured) != 1 {
		t.Fatalf("expected 1 failure ledger entry, got %d", len(captured))
	}
	if !strings.HasPrefix(captured[0].Content, "[fail] write ") {
		t.Errorf("expected fail-write ledger, got %q", captured[0].Content)
	}
}

func TestWriteFile_NoSink_StillSucceeds(t *testing.T) {
	// Critical: tool must work even when no LedgerSink is installed.
	// Otherwise standalone test rigs and degraded callers break.
	exec := makeWriteFile(&FileToolDeps{Host: newFakeHost()})
	_, err := exec(context.Background(), argsToken(t, writeFileInput{
		Path: "/tmp/x", Content: "y",
	}))
	if err != nil {
		t.Errorf("write must succeed without sink: %v", err)
	}
}

func TestWriteFile_ResultJSONHasNoContentField(t *testing.T) {
	// AC-004 wire-shape guard at the executor boundary.
	host := newFakeHost()
	exec := makeWriteFile(&FileToolDeps{Host: host})
	tok, err := exec(context.Background(), argsToken(t, writeFileInput{
		Path: "/tmp/x", Content: "secret data",
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	raw, ok := tok.Payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type %T is not json.RawMessage", tok.Payload)
	}
	wire := string(raw)
	if strings.Contains(wire, "secret data") {
		t.Errorf("write_file result leaked file content: %s", wire)
	}
	for _, banned := range []string{`"content"`, `"body"`, `"bytes_written"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("write_file result contains banned field %s: %s", banned, wire)
		}
	}
}

// ── edit_file ───────────────────────────────────────────────────────────────

func TestEditFile_HappyPath(t *testing.T) {
	host := newFakeHost()
	host.files["/tmp/code.go"] = []byte("package main\n\nfunc main() { println(\"hi\") }\n")
	sink := &recordingSink{}
	exec := makeEditFile(&FileToolDeps{Host: host})

	ctx := cpn.WithLedgerSink(context.Background(), sink)
	tok, err := exec(ctx, argsToken(t, editFileInput{
		Path: "/tmp/code.go",
		Old:  `println("hi")`,
		New:  `println("hello")`,
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	var got EditFileResult
	resultOf(t, tok, &got)
	if !strings.Contains(got.Diff, "hello") {
		t.Errorf("expected diff to mention new text, got: %s", got.Diff)
	}
	captured := sink.snapshot()
	if len(captured) != 1 || !strings.HasPrefix(captured[0].Content, "[ok] edit ") {
		t.Errorf("expected one [ok] edit ledger entry, got %v", captured)
	}
}

func TestEditFile_OldNotFoundEmitsFailure(t *testing.T) {
	host := newFakeHost()
	host.files["/tmp/x"] = []byte("body")
	sink := &recordingSink{}
	exec := makeEditFile(&FileToolDeps{Host: host})

	ctx := cpn.WithLedgerSink(context.Background(), sink)
	_, err := exec(ctx, argsToken(t, editFileInput{
		Path: "/tmp/x", Old: "missing", New: "y",
	}))
	if err == nil {
		t.Fatalf("expected error when old not found")
	}
	captured := sink.snapshot()
	if len(captured) != 1 || !strings.Contains(captured[0].Content, "[fail] edit ") {
		t.Errorf("expected failure ledger, got %v", captured)
	}
}

func TestEditFile_ResultHasDiffNotFullBody(t *testing.T) {
	// AC-equivalent for edit_file: diff field present, no full body.
	host := newFakeHost()
	host.files["/tmp/x"] = []byte("foo bar baz")
	exec := makeEditFile(&FileToolDeps{Host: host})

	tok, err := exec(context.Background(), argsToken(t, editFileInput{
		Path: "/tmp/x", Old: "bar", New: "BAZ",
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	raw := tok.Payload.(json.RawMessage)
	wire := string(raw)
	if !strings.Contains(wire, `"diff"`) {
		t.Errorf("expected diff field: %s", wire)
	}
	for _, banned := range []string{`"content"`, `"body"`, `"after"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("edit_file result contains banned field %s: %s", banned, wire)
		}
	}
}

// ── unifiedDiff truncation ──────────────────────────────────────────────────

func TestUnifiedDiff_TruncatesPastMax(t *testing.T) {
	old := strings.Repeat("a\n", 200)
	new := strings.Repeat("b\n", 200)
	diff, truncated := unifiedDiff("p", []byte(old), []byte(new), 50)
	if !truncated {
		t.Errorf("expected truncated=true")
	}
	if !strings.Contains(diff, "diff truncated") {
		t.Errorf("expected truncation marker, got: %s", diff)
	}
}

func TestUnifiedDiff_PassThroughUnderMax(t *testing.T) {
	_, truncated := unifiedDiff("p", []byte("a\n"), []byte("b\n"), 200)
	if truncated {
		t.Errorf("small diff should not be truncated")
	}
}

// ── countLines ──────────────────────────────────────────────────────────────

func TestCountLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a\n", 1},
		{"a\nb\n", 2},
		{"a\nb", 2}, // unterminated final line counts
		{"\n\n\n", 3},
	}
	for _, tc := range cases {
		got := countLines([]byte(tc.in))
		if got != tc.want {
			t.Errorf("countLines(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

// ── unmarshalArgs (multiple wire shapes) ───────────────────────────────────

func TestUnmarshalArgs_AcceptsMultipleShapes(t *testing.T) {
	want := writeFileInput{Path: "/x", Content: "y"}
	cases := []struct {
		name    string
		payload any
	}{
		{"json.RawMessage", json.RawMessage(`{"path":"/x","content":"y"}`)},
		{"[]byte", []byte(`{"path":"/x","content":"y"}`)},
		{"string", `{"path":"/x","content":"y"}`},
		{"struct", writeFileInput{Path: "/x", Content: "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got writeFileInput
			err := unmarshalArgs(cpn.Token{Payload: tc.payload}, &got)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUnmarshalArgs_RejectsNil(t *testing.T) {
	var dst writeFileInput
	err := unmarshalArgs(cpn.Token{Payload: nil}, &dst)
	if err == nil {
		t.Errorf("expected nil-payload error")
	}
}
