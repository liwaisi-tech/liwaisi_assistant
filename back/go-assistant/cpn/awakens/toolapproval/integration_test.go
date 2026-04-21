package toolapproval

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

func TestIntegration_StageApproveAutoDriftReprompt(t *testing.T) {
	ctx := context.Background()
	pending := toolsynth.NewMemoryPendingToolStore()
	prompter := &stubPrompter{decision: DecisionApproved}
	g := &Gate{
		Approvals: NewMemoryApprovalStore(),
		Pending:   pending,
		Emitter:   awakens.NewEmitter(nil),
		Prompter:  prompter,
		Clock:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	stagePending(t, pending, "sess", "rg", "sha-A")

	d, err := g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionApproved {
		t.Fatalf("first call: %v / %v", d, err)
	}
	if prompter.calls != 1 {
		t.Fatalf("first call must prompt")
	}

	d, err = g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionApproved {
		t.Fatalf("second call: %v / %v", d, err)
	}
	if prompter.calls != 1 {
		t.Fatalf("second call must auto-approve, got %d prompts", prompter.calls)
	}

	_ = pending.Delete(ctx, "rg")
	stagePending(t, pending, "sess", "rg", "sha-B")

	d, err = g.CheckSynthesized(ctx, "sess", synthesizedManifest("rg"))
	if err != nil || d != DecisionApproved {
		t.Fatalf("drift call: %v / %v", d, err)
	}
	if prompter.calls != 2 {
		t.Fatalf("drift must re-prompt, got %d prompts", prompter.calls)
	}

	list, err := g.Approvals.ListForSession(ctx, "sess")
	if err != nil {
		t.Fatalf("ListForSession: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 approvals after drift, got %d", len(list))
	}
}
