package toolapproval

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

type stubPrompter struct {
	decision Decision
	err      error
	calls    int
}

func (s *stubPrompter) PromptAndAwait(_ context.Context, _ awakens.A2UIMessage) (Decision, error) {
	s.calls++
	return s.decision, s.err
}

func newGate(t *testing.T, prompter *stubPrompter) (*Gate, *toolsynth.MemoryPendingToolStore) {
	t.Helper()
	pending := toolsynth.NewMemoryPendingToolStore()
	return &Gate{
		Approvals: NewMemoryApprovalStore(),
		Pending:   pending,
		Emitter:   awakens.NewEmitter(nil),
		Prompter:  prompter,
		Clock:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}, pending
}

func synthesizedManifest(name string) cpn.ToolManifest {
	return cpn.ToolManifest{
		Namespace: toolsynth.DefaultNamespace,
		Name:      name,
		Version:   toolsynth.DefaultVersion,
		Schema:    json.RawMessage(`{"type":"object","properties":{}}`),
		Kind:      toolsynth.KindSynthesized,
		Origin:    toolsynth.OriginHelpParser,
	}
}

func stagePending(t *testing.T, store toolsynth.PendingToolStore, sessionID, name, prov string) toolsynth.PendingTool {
	t.Helper()
	pt := toolsynth.PendingTool{
		Manifest:         synthesizedManifest(name),
		SourceSHA256:     "src-" + prov,
		ProvenanceSHA256: prov,
		SessionID:        sessionID,
		CreatedAt:        time.Unix(1700000000, 0).UTC(),
	}
	if err := store.Stage(context.Background(), pt); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	return pt
}

func TestGate_NonSynthesized_PassThrough(t *testing.T) {
	g, _ := newGate(t, &stubPrompter{decision: DecisionDenied})
	m := cpn.ToolManifest{Name: "rg", Kind: ""}
	d, err := g.CheckSynthesized(context.Background(), "sess", m)
	if err != nil || d != DecisionApproved {
		t.Fatalf("want approved pass-through, got %v / %v", d, err)
	}
}

func TestGate_FirstCall_PromptsAndRecords(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionApproved}
	g, pending := newGate(t, prompter)
	ctx := context.Background()
	stagePending(t, pending, "sess", "rg", "sha-A")

	d, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionApproved {
		t.Fatalf("want approved, got %v / %v", d, err)
	}
	if prompter.calls != 1 {
		t.Fatalf("want 1 prompt, got %d", prompter.calls)
	}
	got, _ := g.Approvals.Lookup(ctx, "sess", "rg", "sha-A")
	if got != DecisionApproved {
		t.Fatalf("approval not recorded: %v", got)
	}
}

func TestGate_SecondCall_AutoApproves(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionApproved}
	g, pending := newGate(t, prompter)
	ctx := context.Background()
	stagePending(t, pending, "sess", "rg", "sha-A")

	if _, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg")); err != nil {
		t.Fatalf("first: %v", err)
	}
	d, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionApproved {
		t.Fatalf("second: want approved, got %v / %v", d, err)
	}
	if prompter.calls != 1 {
		t.Fatalf("want 1 prompt total; auto-approval skipped second, got %d calls", prompter.calls)
	}
}

func TestGate_ProvenanceDrift_RePrompts(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionApproved}
	g, pending := newGate(t, prompter)
	ctx := context.Background()
	stagePending(t, pending, "sess", "rg", "sha-A")

	if _, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg")); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Re-synthesize with new provenance.
	_ = pending.Delete(ctx, "rg")
	stagePending(t, pending, "sess", "rg", "sha-B")

	if _, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg")); err != nil {
		t.Fatalf("drift: %v", err)
	}
	if prompter.calls != 2 {
		t.Fatalf("drift MUST re-prompt; got %d calls", prompter.calls)
	}
}

func TestGate_Denied_ReturnsDenied_DoesNotRecord(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionDenied}
	g, pending := newGate(t, prompter)
	ctx := context.Background()
	stagePending(t, pending, "sess", "rg", "sha-A")

	d, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionDenied {
		t.Fatalf("want denied, got %v / %v", d, err)
	}
	got, _ := g.Approvals.Lookup(ctx, "sess", "rg", "sha-A")
	if got != DecisionUnknown {
		t.Fatalf("denied MUST NOT record; got %v", got)
	}
}

func TestGate_PendingLookupMiss_FailsClosed(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionApproved}
	g, _ := newGate(t, prompter)

	d, err := g.CheckSynthesized(context.Background(), "sess", synthesizedManifest("ghost"))
	if d != DecisionDenied {
		t.Fatalf("want denied on pending miss, got %v", d)
	}
	if !errors.Is(err, toolsynth.ErrPendingNotFound) {
		t.Fatalf("want ErrPendingNotFound, got %v", err)
	}
	if prompter.calls != 0 {
		t.Fatalf("MUST NOT prompt on pending miss")
	}
}

func TestGate_MissingSession_FailsClosed(t *testing.T) {
	prompter := &stubPrompter{decision: DecisionApproved}
	g, _ := newGate(t, prompter)
	d, err := g.CheckSynthesized(context.Background(), "", synthesizedManifest("rg"))
	if d != DecisionDenied || err == nil {
		t.Fatalf("want denied+error on empty session, got %v / %v", d, err)
	}
}

func TestGate_Misconfigured_FailsClosed(t *testing.T) {
	g := &Gate{}
	d, err := g.CheckSynthesized(context.Background(), "sess", synthesizedManifest("rg"))
	if d != DecisionDenied || !errors.Is(err, ErrGateMisconfigured) {
		t.Fatalf("want misconfigured denial, got %v / %v", d, err)
	}
}

func TestGate_PrompterError_FailsClosed(t *testing.T) {
	prompter := &stubPrompter{err: errors.New("bus down")}
	g, pending := newGate(t, prompter)
	stagePending(t, pending, "sess", "rg", "sha-A")
	d, err := g.CheckSynthesized(context.Background(), "sess", synthesizedManifest("rg"))
	if d != DecisionDenied || err == nil {
		t.Fatalf("prompter error MUST fail closed, got %v / %v", d, err)
	}
}
