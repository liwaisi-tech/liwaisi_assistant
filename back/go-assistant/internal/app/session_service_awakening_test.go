package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Fixtures ──────────────────────────────────────────────────────────────

// fakeAwakeningModeRegistry is a minimal test double that records Begin/End
// calls so we can assert the runAwakening path brackets the execution with
// the registry flag.
type fakeAwakeningModeRegistry struct {
	begins []string
	ends   []string
}

func (f *fakeAwakeningModeRegistry) Begin(sid string) { f.begins = append(f.begins, sid) }
func (f *fakeAwakeningModeRegistry) End(sid string)   { f.ends = append(f.ends, sid) }

// stubSandbox swaps fanout.DetectSandboxFn for the duration of a test so
// tests don't depend on bwrap/firejail being installed on the builder.
func stubSandbox(t *testing.T, s fanout.Sandbox, err error) {
	t.Helper()
	prev := fanout.DetectSandboxFn
	fanout.DetectSandboxFn = func() (fanout.Sandbox, error) { return s, err }
	t.Cleanup(func() { fanout.DetectSandboxFn = prev })
}

// ── Tests ─────────────────────────────────────────────────────────────────

// A fresh snapshot in the repository must short-circuit the full brae-awakens
// run — no factory call, no LLM traffic — and still yield a non-empty A2UI
// first-turn envelope.
func TestRunAwakening_CacheHitShortCircuits(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-cache")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	cached := persist.HostCapabilitySnapshot{
		ID:         "snap-fresh",
		HostID:     "machine-cache",
		CapturedAt: time.Now(),
		Source:     awakens.SourceAwakening,
		Kernel:     persist.HostKernel{OS: "linux", Arch: "amd64"},
		Identity:   persist.HostIdentity{Shell: "/bin/bash"},
	}
	if err := repo.Save(context.Background(), cached); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	factoryCalls := 0
	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			factoryCalls++
			return nil
		},
	}

	snap, envelope, source, err := svc.runAwakening(context.Background(), "sess-cache", "user-test")
	if err != nil {
		t.Fatalf("runAwakening: %v", err)
	}
	if factoryCalls != 0 {
		t.Errorf("cache hit must not invoke awakens factory (got %d calls)", factoryCalls)
	}
	if snap.ID != "snap-fresh" {
		t.Errorf("snapshot.ID = %q; want snap-fresh", snap.ID)
	}
	if source != awakens.SourceAwakening {
		t.Errorf("source = %q; want %q", source, awakens.SourceAwakening)
	}
	if len(envelope.Components) == 0 {
		t.Error("expected non-empty envelope components on cache hit")
	}
}

// AC-004 — Given a cache hit within 24 h, When awakening fires, Then no LLM
// call is made AND a first-turn A2UI card is still emitted (SC-03 REQ-303 /
// REQ-304). Captures slog output to prove `awakening.cache.hit` and
// `awakening.emitted` events both fire on the re-render path.
func TestRunAwakening_CacheHitReRenderPath_AC004(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-ac004")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	cached := persist.HostCapabilitySnapshot{
		ID:         "snap-ac004",
		HostID:     "machine-ac004",
		CapturedAt: time.Now().Add(-2 * time.Hour),
		Source:     awakens.SourceAwakening,
		Kernel:     persist.HostKernel{OS: "linux", Arch: "amd64"},
		Identity:   persist.HostIdentity{Shell: "/bin/zsh"},
		Binaries: []persist.BinaryProbe{
			{Name: "git", Present: true, Version: "2.42.0"},
			{Name: "docker", Present: false},
		},
	}
	if err := repo.Save(context.Background(), cached); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var llmCalls atomic.Int32
	llm := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *cpn.LLMRequest) (cpn.LLMResponse, error) {
			llmCalls.Add(1)
			return cpn.LLMResponse{Content: "should-not-be-called"}, nil
		},
	}

	svc := &SessionService{
		logger:             logger,
		hostCapabilityRepo: repo,
		llm:                llm,
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			t.Fatal("factory must not be invoked on cache hit")
			return nil
		},
	}

	snap, envelope, source, err := svc.runAwakening(context.Background(), "sess-ac004", "user-ac004")
	if err != nil {
		t.Fatalf("runAwakening: %v", err)
	}
	if got := llmCalls.Load(); got != 0 {
		t.Errorf("LLM must not be called on cache hit; got %d calls", got)
	}
	if source != awakens.SourceAwakening {
		t.Errorf("source = %q; want %q", source, awakens.SourceAwakening)
	}
	if snap.ID != "snap-ac004" {
		t.Errorf("snapshot.ID = %q; want snap-ac004", snap.ID)
	}

	if len(envelope.Components) == 0 {
		t.Fatal("cache hit MUST still emit a first-turn A2UI envelope (REQ-303)")
	}
	raw, err := envelope.Marshal()
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if !strings.Contains(string(raw), "git") {
		t.Errorf("envelope must surface cached tool 'git'; got %s", raw)
	}

	logs := buf.String()
	if !strings.Contains(logs, "brae.awakening.cache.hit") {
		t.Errorf("missing awakening.cache.hit slog event (REQ-304); logs=%s", logs)
	}
	if !strings.Contains(logs, "brae.awakening.emitted") {
		t.Errorf("missing awakening.emitted slog event on re-render path; logs=%s", logs)
	}
}

// Manual flush (REQ-305c): FlushAwakeningCache wipes the repo cache so the
// next LookupCachedSnapshot reports a miss, forcing a fresh bootstrap.
func TestFlushAwakeningCache_ForcesMiss(t *testing.T) {
	t.Parallel()
	repo := persist.NewMemoryHostCapabilityRepository()
	hostID := "machine-flush"
	snap := persist.HostCapabilitySnapshot{
		ID:         "snap-flush",
		HostID:     hostID,
		CapturedAt: time.Now(),
		Source:     awakens.SourceAwakening,
	}
	if err := repo.Save(context.Background(), snap); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, fresh, err := awakens.LookupCachedSnapshot(context.Background(), repo, hostID, 0, nil); err != nil || !fresh {
		t.Fatalf("pre-flush: want fresh cache hit; err=%v fresh=%v", err, fresh)
	}

	if err := awakens.FlushAwakeningCache(context.Background(), repo, hostID); err != nil {
		t.Fatalf("FlushAwakeningCache: %v", err)
	}

	_, fresh, err := awakens.LookupCachedSnapshot(context.Background(), repo, hostID, 0, nil)
	if !errors.Is(err, awakens.ErrCacheMiss) {
		t.Fatalf("post-flush: want ErrCacheMiss; got err=%v fresh=%v", err, fresh)
	}
}

// errAwakeningNotConfigured must surface when the factory or repo is nil so
// CreateSession can cleanly skip the flow.
func TestRunAwakening_NotConfiguredWhenFactoryNil(t *testing.T) {
	t.Parallel()
	svc := &SessionService{logger: testLogger()}
	_, _, _, err := svc.runAwakening(context.Background(), "sess-x", "user-test")
	if !errors.Is(err, errAwakeningNotConfigured) {
		t.Fatalf("expected errAwakeningNotConfigured; got %v", err)
	}
}

// When the LLM is not configured but a factory + repo are wired, runAwakening
// MUST return an error rather than falling back to any legacy path. First-boot
// awakening is required to succeed via the LLM path.
func TestRunAwakening_ErrorsWhenLLMAbsent(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-fb")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	mode := &fakeAwakeningModeRegistry{}
	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			t.Fatal("factory must not run when LLM is nil — error must propagate first")
			return nil
		},
		awakeningMode: mode,
		// llm intentionally left nil.
	}

	_, _, _, err := svc.runAwakening(context.Background(), "sess-fb", "user-test")
	if err == nil {
		t.Fatal("expected error when LLM is nil; got nil")
	}
	if errors.Is(err, errAwakeningNotConfigured) {
		t.Fatalf("expected a distinct LLM-missing error, not errAwakeningNotConfigured; got %v", err)
	}
	if !strings.Contains(err.Error(), "LLM") {
		t.Errorf("error message should mention LLM; got %q", err.Error())
	}
}

// When the factory returns nil, runAwakening propagates an error to the
// caller instead of falling back.
func TestRunAwakening_ErrorsWhenFactoryReturnsNil(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-nil")
	defer restore()
	stubSandbox(t, fanout.Sandbox{Tool: fanout.ToolBwrap, Argv: []string{"--ro-bind", "/", "/"}}, nil)

	repo := persist.NewMemoryHostCapabilityRepository()
	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		llm:                &mockLLMClient{},
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			return nil
		},
	}

	_, _, _, err := svc.runAwakening(context.Background(), "sess-nil-factory", "user-test")
	if err == nil {
		t.Fatal("expected error when factory returns nil; got nil")
	}
	if !strings.Contains(err.Error(), "factory") {
		t.Errorf("error should mention factory; got %q", err.Error())
	}
}

// appendAwakeningMessage writes a cpn_role="awakening" message with an A2UI
// JSON payload the front-end can decode.
func TestAppendAwakeningMessage_ShapeAndCPNRole(t *testing.T) {
	t.Parallel()

	svc := &SessionService{logger: testLogger()}
	root := emptyRootCPN()
	session := cpn.NewSession("sess-msg", "user-1", cpn.ChannelWeb, root)

	envelope := awakens.BuildFirstTurnMessage(awakens.AwakeningReport{
		OS:    awakens.AwakeningOS{Name: "linux", Arch: "amd64"},
		Shell: awakens.AwakeningShell{Path: "/bin/bash"},
	})
	svc.appendAwakeningMessage(context.Background(), session, envelope)

	msgs := session.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message appended; got %d", len(msgs))
	}
	got := msgs[0]
	if got.CPNRole != awakens.A2UICPNRole {
		t.Errorf("CPNRole = %q; want %q", got.CPNRole, awakens.A2UICPNRole)
	}
	if got.Role != cpn.RoleAssistant {
		t.Errorf("Role = %q; want assistant", got.Role)
	}
	if !strings.HasPrefix(got.Content, cpn.A2UIMarker) {
		t.Fatalf("content missing %q prefix: %q", cpn.A2UIMarker, got.Content)
	}
	var decoded map[string]any
	body := strings.TrimPrefix(got.Content, cpn.A2UIMarker)
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("content is not JSON: %v — content=%q", err, got.Content)
	}
	if _, ok := decoded["components"]; !ok {
		t.Errorf("A2UI envelope missing 'components' key: %v", decoded)
	}
}

// SC-15 — runAwakening wires its LLM transitions through the fallback chain.
// When the factory returns nil, that is a NON-eligible error so the chain
// terminates after a single attempt — confirming the chain is engaged and
// the primary model is evaluated first.
func TestRunAwakening_FallbackChainWired(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-sc15")
	defer restore()
	stubSandbox(t, fanout.Sandbox{Tool: fanout.ToolBwrap, Argv: []string{"--ro-bind", "/", "/"}}, nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo := persist.NewMemoryHostCapabilityRepository()
	calls := 0
	svc := &SessionService{
		logger:             logger,
		hostCapabilityRepo: repo,
		llm:                &mockLLMClient{},
		awakensFactory: func(_ string, _ awakens.Deps) *cpn.CPN {
			calls++
			return nil
		},
	}

	_, _, _, err := svc.runAwakening(context.Background(), "sess-sc15", "user-test")
	if err == nil {
		t.Fatal("expected error when factory returns nil")
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 factory call (non-eligible error short-circuits chain); got %d", calls)
	}
	if strings.Contains(buf.String(), "brae.awakening.llm.fallback_used") {
		t.Fatalf("non-eligible error must not emit fallback_used; logs=%s", buf.String())
	}
}

// extractEmittedMessage + extractAwakeningSnapshot are internal seams
// between runAwakening and the CPN's terminal places. They must tolerate
// nil CPNs and missing places gracefully.
func TestExtractHelpers_NilAndMissing(t *testing.T) {
	t.Parallel()
	if _, ok := extractEmittedMessage(nil); ok {
		t.Error("extractEmittedMessage(nil) must report ok=false")
	}
	if _, ok := extractAwakeningSnapshot(nil); ok {
		t.Error("extractAwakeningSnapshot(nil) must report ok=false")
	}
	root := emptyRootCPN()
	if _, ok := extractEmittedMessage(root); ok {
		t.Error("extractEmittedMessage on topology without -emitted place must be ok=false")
	}
}

// SC-10 REQ-1003 / AC-008 — when neither bwrap nor firejail resolves,
// awakening fails fast with error_class="sandbox_missing" and no probes run.
func TestRunAwakening_SandboxMissingFailsFast(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-sb")
	defer restore()
	stubSandbox(t, fanout.Sandbox{}, fanout.ErrSandboxMissing)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo := persist.NewMemoryHostCapabilityRepository()
	factoryCalls := 0
	svc := &SessionService{
		logger:             logger,
		hostCapabilityRepo: repo,
		llm:                &mockLLMClient{},
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			factoryCalls++
			return nil
		},
	}

	_, _, _, err := svc.runAwakening(context.Background(), "sess-sb", "user-test")
	if err == nil {
		t.Fatal("expected error when sandbox is missing")
	}
	if !errors.Is(err, fanout.ErrSandboxMissing) {
		t.Fatalf("expected ErrSandboxMissing; got %v", err)
	}
	if factoryCalls != 0 {
		t.Errorf("factory must NOT be invoked when sandbox missing; got %d calls", factoryCalls)
	}
	logs := buf.String()
	if !strings.Contains(logs, `error_class=sandbox_missing`) {
		t.Errorf("missing error_class=sandbox_missing in logs; logs=%s", logs)
	}
	if !strings.Contains(logs, "brae.awakening.failed") {
		t.Errorf("missing brae.awakening.failed event; logs=%s", logs)
	}
}
