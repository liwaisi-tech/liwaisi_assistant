package awakens

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// SynthRunner is the injected tool-synthesis driver. Implementations take a
// list of binary names discovered on the host and, for each, run
// helpparse.Compose (to invoke --help/-h and extract a HelpSchema) followed
// by toolsynth.Compose (to materialise a ToolManifest staged in a pending
// store). Nothing is registered into cpn/tools.Registry — staged manifests
// require SC-13 HITL approval at invocation time before they become live
// tools.
//
// The contract is:
//   - Called once per awakening, after t-awaken-curate.
//   - Input `binaries` is the set of binary names to attempt synthesis for.
//     The caller already filters out system tools and anything already
//     promoted by curate.
//   - Return the count of PendingTools successfully staged. Zero is a valid
//     return value — no binaries may have yielded a parseable --help.
//   - Any error is logged and treated as "no tools synthesized" by the
//     transition; awakening MUST NOT fail on synthesis problems.
//
// The session service supplies the concrete runner wrapping helpparse +
// toolsynth with the live LLMClient, HostAdapter, HostGate, Sandbox, and
// PendingToolStore. Tests pass no runner so the transition is a pass-
// through.
type SynthRunner interface {
	Synthesize(ctx context.Context, sessionID string, binaries []string) (int, error)
}

// SynthRunnerFunc adapts a closure into a SynthRunner.
type SynthRunnerFunc func(ctx context.Context, sessionID string, binaries []string) (int, error)

// Synthesize implements SynthRunner.
func (f SynthRunnerFunc) Synthesize(ctx context.Context, sessionID string, binaries []string) (int, error) {
	return f(ctx, sessionID, binaries)
}

// MaxSynthBinaries caps the number of binaries handed to helpparse.Compose
// in a single awakening. Matches helpparse.MaxBinaries but duplicated here
// so the cap is enforced before the sub-CPN is composed.
const MaxSynthBinaries = 16

// newSynthTransition constructs the t-awaken-synth tool transition. It
// consumes the curated AwakeningReport, selects binaries that are present
// on the host but not already promoted by curate, and hands them to
// deps.Synth. The report flows through unchanged — synthesised manifests
// live in the pending store, not on the token. When no runner is wired or
// the runner errors, the transition degrades to a no-op pass-through so
// awakening boot is never blocked on synthesis.
func newSynthTransition(deps Deps) *cpn.Transition {
	emitter := deps.Emitter
	t := cpn.NewTransition(
		TransitionAwakenSynth,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningReport},
		[]string{PlaceAwakeningReportSynth},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenSynth)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}

		forward := func() map[string]cpn.Token {
			return map[string]cpn.Token{
				PlaceAwakeningReportSynth: {
					Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: report,
				},
			}
		}

		if deps.Synth == nil {
			slog.InfoContext(ctx, "awakens.synth.skipped", "reason", "no_runner_wired")
			if emitter != nil {
				emitter.SynthesisCompleted(ctx, "", "")
			}
			return forward(), nil
		}

		candidates := selectSynthCandidates(report)
		if len(candidates) == 0 {
			slog.InfoContext(ctx, "awakens.synth.skipped", "reason", "no_candidates")
			return forward(), nil
		}
		if len(candidates) > MaxSynthBinaries {
			candidates = candidates[:MaxSynthBinaries]
		}

		staged, runErr := deps.Synth.Synthesize(ctx, report.Host.Sandbox.Tool, candidates)
		if runErr != nil {
			slog.WarnContext(ctx, "awakens.synth.error", "error", runErr)
			return forward(), nil
		}
		slog.InfoContext(ctx, "awakens.synth.completed",
			"candidates", len(candidates),
			"staged", staged,
		)
		return forward(), nil
	}
	return t
}

// selectSynthCandidates returns the binary names the synth runner should
// attempt to help-parse: present on host, not already promoted by curate,
// not a trivial shell builtin or system tool. Ordered deterministically.
func selectSynthCandidates(r AwakeningReport) []string {
	skipSystem := map[string]struct{}{
		"bash_exec": {}, "file_read": {}, "file_write": {}, "register_tool": {},
	}
	skipTrivial := map[string]struct{}{
		"sh": {}, "bash": {}, "cat": {}, "ls": {}, "echo": {}, "pwd": {}, "cd": {},
	}
	alreadyPicked := make(map[string]struct{}, len(r.ToolsRegister))
	for _, p := range r.ToolsRegister {
		alreadyPicked[p.Name] = struct{}{}
	}
	out := make([]string, 0, len(r.PresentTools))
	for _, t := range r.PresentTools {
		n := t.Name
		if n == "" {
			continue
		}
		if _, ok := skipSystem[n]; ok {
			continue
		}
		if _, ok := skipTrivial[n]; ok {
			continue
		}
		if _, ok := alreadyPicked[n]; ok {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
