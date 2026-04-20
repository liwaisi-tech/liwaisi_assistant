package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Fixtures ──────────────────────────────────────────────────────────────

// fakeAwakeningModeRegistry is a minimal test double that records Begin/End
// calls so we can assert the runAwakening path brackets the execution with
// the registry flag (CON-003).
type fakeAwakeningModeRegistry struct {
	begins []string
	ends   []string
}

func (f *fakeAwakeningModeRegistry) Begin(sid string) { f.begins = append(f.begins, sid) }
func (f *fakeAwakeningModeRegistry) End(sid string)   { f.ends = append(f.ends, sid) }

// ── Tests ─────────────────────────────────────────────────────────────────

// AC-008: a fresh snapshot in the repository must short-circuit the full
// brae-awakens run — no factory call, no LLM traffic — and still yield a
// non-empty A2UI first-turn envelope.
func TestRunAwakening_CacheHitShortCircuits(t *testing.T) {
	t.Parallel()
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

	snap, envelope, source, err := svc.runAwakening(context.Background(), "sess-cache")
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

// errAwakeningNotConfigured must surface when the factory or repo is nil so
// CreateSession can cleanly fall back to ensureHostCapabilitiesSeed (CON-006).
func TestRunAwakening_NotConfiguredWhenFactoryNil(t *testing.T) {
	t.Parallel()
	svc := &SessionService{logger: testLogger()}
	_, _, _, err := svc.runAwakening(context.Background(), "sess-x")
	if !errors.Is(err, errAwakeningNotConfigured) {
		t.Fatalf("expected errAwakeningNotConfigured; got %v", err)
	}
}

// AC-004: when the LLM is not configured, runAwakening must route through
// the legacy host-discovery path and tag the snapshot source as
// "awakening-fallback".
func TestRunAwakening_FallbackWhenLLMAbsent(t *testing.T) {
	t.Parallel()
	restore := stubHostIDResolver(t, "machine-fb")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	mode := &fakeAwakeningModeRegistry{}
	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		awakensFactory: func(string, awakens.Deps) *cpn.CPN {
			t.Fatal("factory must not run when LLM is nil — fallback should fire first")
			return nil
		},
		awakeningMode: mode,
		// llm intentionally left nil.
	}

	snap, envelope, source, err := svc.runAwakening(context.Background(), "sess-fb")
	if err != nil {
		t.Fatalf("runAwakening: %v", err)
	}
	if source != awakens.SourceAwakeningFallback {
		t.Errorf("source = %q; want %q", source, awakens.SourceAwakeningFallback)
	}
	if snap.ID != "" {
		t.Errorf("snap.ID = %q; want empty (no discovery ran)", snap.ID)
	}
	if len(envelope.Components) == 0 {
		t.Error("fallback envelope must carry at least one component (BEH-003)")
	}
}

// AC-001 shape: appendAwakeningMessage writes a cpn_role="awakening"
// message with an A2UI JSON payload the front-end can decode.
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
	// Content must carry the A2UI marker the frontend uses to detect
	// structured payloads, with a valid v0.8 envelope body.
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
