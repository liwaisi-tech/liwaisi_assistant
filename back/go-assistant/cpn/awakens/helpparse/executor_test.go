package helpparse

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

type fakeAdapter struct {
	mu    sync.Mutex
	calls []cpn.ExecRequest
	reply map[string]cpn.ExecResult
	err   map[string]error
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{reply: map[string]cpn.ExecResult{}, err: map[string]error{}}
}

func (f *fakeAdapter) Exec(_ context.Context, req cpn.ExecRequest) (cpn.ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	key := req.Command + " " + strings.Join(req.Args, " ")
	if e, ok := f.err[key]; ok {
		return cpn.ExecResult{}, e
	}
	if r, ok := f.reply[key]; ok {
		return r, nil
	}
	return cpn.ExecResult{ExitCode: 0, Stdout: []byte("generic help")}, nil
}
func (f *fakeAdapter) SpawnPTY(context.Context, cpn.PTYRequest) (cpn.PTYHandle, error) {
	return cpn.PTYHandle{}, errors.New("nope")
}
func (f *fakeAdapter) KillPID(context.Context, int, cpn.Signal) error { return errors.New("nope") }
func (f *fakeAdapter) ReadFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("nope")
}
func (f *fakeAdapter) WriteFile(context.Context, string, []byte, fs.FileMode) error {
	return errors.New("nope")
}
func (f *fakeAdapter) Stat(context.Context, string) (cpn.FileInfo, error) {
	return cpn.FileInfo{}, errors.New("nope")
}

type recordingGate struct {
	mu  sync.Mutex
	ops []cpn.GateOp
	err error
}

func (g *recordingGate) Check(_ context.Context, op cpn.GateOp) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, op)
	return g.err
}

func TestInvokeExecutor_LongAndShortViaHostGate(t *testing.T) {
	t.Parallel()
	ad := newFakeAdapter()
	ad.reply["/usr/bin/git --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("git long help")}
	ad.reply["/usr/bin/git -h"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("git short help")}
	gate := &recordingGate{}
	deps := Deps{HostAdapter: ad, HostGate: gate}

	longExec := makeInvokeExecutor("git", "/usr/bin/git", HelpVariantLong, deps)
	shortExec := makeInvokeExecutor("git", "/usr/bin/git", HelpVariantShort, deps)
	tok1, _ := longExec(context.Background(), cpn.Token{})
	tok2, _ := shortExec(context.Background(), cpn.Token{})

	long := tok1.Payload.(HelpRaw)
	short := tok2.Payload.(HelpRaw)
	if long.HelpText != "git long help" {
		t.Fatalf("long text=%q", long.HelpText)
	}
	if short.HelpText != "git short help" {
		t.Fatalf("short text=%q", short.HelpText)
	}
	if long.Variant != HelpVariantLong || short.Variant != HelpVariantShort {
		t.Fatalf("variants wrong: %q %q", long.Variant, short.Variant)
	}

	if len(gate.ops) != 2 {
		t.Fatalf("want 2 gate ops, got %d", len(gate.ops))
	}
	for _, op := range gate.ops {
		if op.Kind != "exec" {
			t.Fatalf("gate op kind = %q (want exec for introspection class)", op.Kind)
		}
		if !strings.HasPrefix(op.Command, "/usr/bin/git ") {
			t.Fatalf("gate op command = %q", op.Command)
		}
	}
}

func TestInvokeExecutor_GateDenied(t *testing.T) {
	t.Parallel()
	gate := &recordingGate{err: errors.New("policy: denied")}
	deps := Deps{HostAdapter: newFakeAdapter(), HostGate: gate}
	exec := makeInvokeExecutor("git", "/usr/bin/git", HelpVariantLong, deps)
	tok, _ := exec(context.Background(), cpn.Token{})
	r := tok.Payload.(HelpRaw)
	if r.ExitCode != GateDenyExitCode {
		t.Fatalf("exit code = %d", r.ExitCode)
	}
	if !strings.Contains(r.Err, "gate") {
		t.Fatalf("err=%q", r.Err)
	}
}

func TestInvokeExecutor_NoHostAdapter(t *testing.T) {
	t.Parallel()
	exec := makeInvokeExecutor("git", "/usr/bin/git", HelpVariantLong, Deps{})
	tok, _ := exec(context.Background(), cpn.Token{})
	r := tok.Payload.(HelpRaw)
	if r.Err == "" {
		t.Fatal("want err")
	}
}

func TestInvokeExecutor_FallsBackToStderr(t *testing.T) {
	t.Parallel()
	ad := newFakeAdapter()
	ad.reply["/x --help"] = cpn.ExecResult{ExitCode: 1, Stderr: []byte("usage: x [opts]")}
	deps := Deps{HostAdapter: ad}
	exec := makeInvokeExecutor("x", "/x", HelpVariantLong, deps)
	tok, _ := exec(context.Background(), cpn.Token{})
	r := tok.Payload.(HelpRaw)
	if !strings.Contains(r.HelpText, "usage") {
		t.Fatalf("helpText=%q", r.HelpText)
	}
}

func TestInvokeExecutor_Timeout(t *testing.T) {
	t.Parallel()
	ad := newFakeAdapter()
	ad.err["/x --help"] = context.DeadlineExceeded
	exec := makeInvokeExecutor("x", "/x", HelpVariantLong, Deps{HostAdapter: ad})
	tok, _ := exec(context.Background(), cpn.Token{})
	r := tok.Payload.(HelpRaw)
	if r.ExitCode != TimeoutExitCode {
		t.Fatalf("exit=%d", r.ExitCode)
	}
	if r.Err != "timeout" {
		t.Fatalf("err=%q", r.Err)
	}
}

func TestInvokeExecutor_ObservabilityCallbacks(t *testing.T) {
	t.Parallel()
	ad := newFakeAdapter()
	ad.reply["/x --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("hi")}
	var gotBinary, gotVariant string
	deps := Deps{
		HostAdapter: ad,
		OnHelpInvoked: func(_ context.Context, b, v string, _ time.Duration, _ int) {
			gotBinary, gotVariant = b, v
		},
	}
	exec := makeInvokeExecutor("x", "/x", HelpVariantLong, deps)
	_, _ = exec(context.Background(), cpn.Token{})
	if gotBinary != "x" || gotVariant != HelpVariantLong {
		t.Fatalf("callback binary=%q variant=%q", gotBinary, gotVariant)
	}
}

func TestMergeHandler_ConcatenatesLongThenShort(t *testing.T) {
	t.Parallel()
	h := makeMergeHandler("git", PlaceHelpMergedPrefix+"git")
	tokens := []cpn.Token{
		helpRawToken(HelpRaw{Binary: "git", Variant: HelpVariantShort, HelpText: "short"}),
		helpRawToken(HelpRaw{Binary: "git", Variant: HelpVariantLong, HelpText: "long"}),
	}
	out, err := h(context.Background(), tokens)
	if err != nil {
		t.Fatal(err)
	}
	merged := out[PlaceHelpMergedPrefix+"git"].Payload.(HelpRaw)
	if merged.HelpText != "long\nshort" {
		t.Fatalf("merged=%q", merged.HelpText)
	}
}

func TestMergeHandler_BothEmpty(t *testing.T) {
	t.Parallel()
	h := makeMergeHandler("x", "pM")
	out, err := h(context.Background(), []cpn.Token{
		helpRawToken(HelpRaw{Binary: "x", Variant: HelpVariantLong, Err: "fail"}),
		helpRawToken(HelpRaw{Binary: "x", Variant: HelpVariantShort, Err: "fail"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	merged := out["pM"].Payload.(HelpRaw)
	if merged.Err == "" {
		t.Fatal("want merged.Err set")
	}
}

func TestSourceSHA256_Deterministic(t *testing.T) {
	t.Parallel()
	a := sourceSHA256("long", "short")
	b := sourceSHA256("long", "short")
	if a != b {
		t.Fatal("not deterministic")
	}
	if c := sourceSHA256("long", "other"); a == c {
		t.Fatal("collision")
	}
}
