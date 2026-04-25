package helpparse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// fakeLLM returns a fixed JSON schema keyed on the binary named in the
// system prompt. Binaries named "fakenohelp" are never invoked because their
// merged HelpText is empty; those responses aren't exercised here.
type fakeLLM struct {
	byBinary map[string]string
	failFor  map[string]error
	mu       sync.Mutex
	calls    int
}

func (f *fakeLLM) Complete(_ context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	var binary string
	for _, m := range req.Messages {
		if m.Role == "system" {
			if idx := strings.Index(m.Content, `the binary "`); idx >= 0 {
				tail := m.Content[idx+len(`the binary "`):]
				if end := strings.IndexByte(tail, '"'); end > 0 {
					binary = tail[:end]
				}
			}
		}
	}
	if e, ok := f.failFor[binary]; ok {
		return cpn.LLMResponse{}, e
	}
	body, ok := f.byBinary[binary]
	if !ok {
		return cpn.LLMResponse{}, fmt.Errorf("no canned response for %q", binary)
	}
	return cpn.LLMResponse{Content: body, Model: "mock"}, nil
}
func (f *fakeLLM) CompleteStream(_ context.Context, _ *cpn.LLMRequest, _ func(string)) (cpn.LLMResponse, error) {
	return cpn.LLMResponse{}, errors.New("not implemented")
}
func (f *fakeLLM) EstimateCost(_ *cpn.LLMRequest) (float64, error) { return 0, nil }

// passThroughChain invokes exec once with a fake model name. Matches the
// FallbackRunner contract the production awakens.FallbackChain satisfies.
type passThroughChain struct{}

func (passThroughChain) Run(ctx context.Context, exec func(ctx context.Context, model string) error) error {
	return exec(ctx, "mock-model")
}

func TestHelpParse_Integration_GitRGSucceedFakeFails(t *testing.T) {
	t.Parallel()
	adapter := newFakeAdapter()
	adapter.reply["/usr/bin/git --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("usage: git ...")}
	adapter.reply["/usr/bin/git -h"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("usage: git -h")}
	adapter.reply["/usr/bin/rg --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("ripgrep help")}
	adapter.reply["/usr/bin/rg -h"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("ripgrep -h")}
	adapter.reply["/opt/fakenohelp --help"] = cpn.ExecResult{ExitCode: 1, Stderr: []byte("")}
	adapter.reply["/opt/fakenohelp -h"] = cpn.ExecResult{ExitCode: 1, Stderr: []byte("")}

	gitSchema := HelpSchema{Binary: "git", Flags: []HelpFlag{{Long: "--version"}}, Subcommands: []HelpSub{{Name: "push"}}, Examples: []string{}}
	rgSchema := HelpSchema{Binary: "rg", Flags: []HelpFlag{{Long: "--color"}}, Subcommands: []HelpSub{}, Examples: []string{}}
	gitJSON, _ := json.Marshal(gitSchema)
	rgJSON, _ := json.Marshal(rgSchema)

	llm := &fakeLLM{byBinary: map[string]string{
		"git": string(gitJSON),
		"rg":  string(rgJSON),
	}}

	var parsedCount, failedCount int
	var mu sync.Mutex
	deps := Deps{
		HostAdapter: adapter,
		HostGate:    &recordingGate{},
		LLM:         llm,
		Chain:       passThroughChain{},
		Timeout:     500 * time.Millisecond,
		OnHelpParsed: func(_ context.Context, _ string, _, _ int) {
			mu.Lock()
			parsedCount++
			mu.Unlock()
		},
		OnHelpFailed: func(_ context.Context, _, _, _ string) {
			mu.Lock()
			failedCount++
			mu.Unlock()
		},
	}
	binaries := []HelpInput{
		{Binary: "git", Path: "/usr/bin/git"},
		{Binary: "rg", Path: "/usr/bin/rg"},
		{Binary: "fakenohelp", Path: "/opt/fakenohelp"},
	}
	c, err := Compose("sess-int", binaries, deps)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	results := make([]HelpResult, 0, 3)
	for _, bin := range []string{"git", "rg", "fakenohelp"} {
		p, ok := c.Places[PlaceHelpResultPrefix+bin]
		if !ok {
			t.Fatalf("missing result place for %s", bin)
		}
		toks, _ := p.Peek()
		if len(toks) != 1 {
			t.Fatalf("binary %s: want 1 result, got %d", bin, len(toks))
		}
		results = append(results, toks[0].Payload.(HelpResult))
	}
	schemas := ReduceHelpResults(results)
	if len(schemas) != 2 {
		t.Fatalf("want 2 schemas, got %d: %+v", len(schemas), schemas)
	}
	if schemas[0].Binary != "git" || schemas[1].Binary != "rg" {
		t.Fatalf("wrong binaries: %+v", schemas)
	}
	for _, s := range schemas {
		if s.SourceSHA256 == "" {
			t.Fatalf("schema %s missing SourceSHA256", s.Binary)
		}
	}
	failures := ReduceHelpFailures(results)
	if len(failures) != 1 || failures[0].Binary != "fakenohelp" {
		t.Fatalf("want 1 failure for fakenohelp, got %+v", failures)
	}
	mu.Lock()
	defer mu.Unlock()
	if parsedCount != 2 {
		t.Fatalf("parsed events = %d (want 2)", parsedCount)
	}
	if failedCount != 1 {
		t.Fatalf("failed events = %d (want 1)", failedCount)
	}
}

func TestHelpParse_Integration_LLMFailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	adapter := newFakeAdapter()
	adapter.reply["/usr/bin/git --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("usage")}
	adapter.reply["/usr/bin/git -h"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("usage")}
	llm := &fakeLLM{
		byBinary: map[string]string{},
		failFor:  map[string]error{"git": errors.New("llm blew up")},
	}
	deps := Deps{HostAdapter: adapter, LLM: llm, Chain: passThroughChain{}, Timeout: 500 * time.Millisecond}
	c, err := Compose("sess", []HelpInput{{Binary: "git", Path: "/usr/bin/git"}}, deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run must not abort (REQ-1104): %v", err)
	}
	toks, _ := c.Places[PlaceHelpResultPrefix+"git"].Peek()
	if len(toks) != 1 {
		t.Fatalf("want 1 result, got %d", len(toks))
	}
	r := toks[0].Payload.(HelpResult)
	if r.Err == "" {
		t.Fatal("want err set")
	}
	if r.Stage != "llm" {
		t.Fatalf("stage=%q", r.Stage)
	}
}

func TestHelpParse_Integration_SchemaValidationFails(t *testing.T) {
	t.Parallel()
	adapter := newFakeAdapter()
	adapter.reply["/x --help"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("help")}
	adapter.reply["/x -h"] = cpn.ExecResult{ExitCode: 0, Stdout: []byte("help")}
	llm := &fakeLLM{byBinary: map[string]string{"x": `{"binary":"x","flags":"not-an-array","subcommands":[],"examples":[]}`}}
	deps := Deps{HostAdapter: adapter, LLM: llm, Chain: passThroughChain{}, Timeout: 500 * time.Millisecond}
	c, err := Compose("sess", []HelpInput{{Binary: "x", Path: "/x"}}, deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	toks, _ := c.Places[PlaceHelpResultPrefix+"x"].Peek()
	r := toks[0].Payload.(HelpResult)
	if r.Stage != "validate" {
		t.Fatalf("stage=%q err=%q", r.Stage, r.Err)
	}
}
