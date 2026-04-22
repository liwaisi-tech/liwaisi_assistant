package awakens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// LLMCuratorFunc is the injected seam that turns a probe-only AwakeningReport
// into a curated ToolsRegister slice. The awakening topology calls it after
// probe-fanout reduces but before the snapshot is persisted, so the LLM-picked
// tools flow through the same RegisterBatch path as any other awakening-
// authored tool.
//
// Contract:
//   - Input `probeReport` carries Binaries[Present=true/false] and Capabilities
//     discovered by the probe-fanout. ToolsRegister is empty.
//   - Return a validated slice of AwakeningToolRegister entries ready for
//     RegisterBatch. MAY be empty — zero curated tools is a legitimate outcome
//     when the probe surface is too minimal to promote anything.
//   - Any error is logged and treated as "no curated tools" by the caller;
//     awakening MUST NOT fail on curation problems (the boot path has no user
//     affordance to recover).
type LLMCuratorFunc func(ctx context.Context, probeReport AwakeningReport) ([]AwakeningToolRegister, error)

// CuratePromptHeader is the stable system-prompt text handed to the curator
// LLM. Kept exported so tests can fingerprint it and so the session service
// can reuse it when wiring its own curator implementation.
const CuratePromptHeader = `You are brae's awakening curator. The host-probe layer has already
discovered which binaries are present on this machine. Your job is to pick a
small, high-signal subset to register as first-class typed tools. Binaries you
do NOT pick remain reachable through the universal bash_exec fallback — there
is no harm in leaving them out. Prefer depth over breadth.

Selection rules:
  - Pick at most 5 binaries. Fewer is fine. Picking zero is fine when the
    surface is too thin.
  - Only pick binaries that appeared in "Available" below. Never invent names.
  - Skip anything already exposed as a system tool (bash_exec, file_read,
    file_write, register_tool) and skip trivial shell builtins (sh, bash,
    cat, ls, echo, pwd, cd).
  - Prefer binaries whose typed wrappers would meaningfully reduce
    bash_exec risk (curl, git, jq, make, etc.) over shell utilities that
    compose freely (awk, sed, grep).

Output ONLY a single JSON object with this exact shape — no prose, no fences:

{
  "picks": [
    {
      "name":     "<binary-name>",
      "basis":    "<one short sentence, <=120 chars, why this is worth promoting>",
      "toolbox":  "<one of: system | developer | web | image | pdf | data | general | awakens>",
      "hashtags": ["<=6 lowercase tokens from the lexicon; empty array allowed>"]
    }
  ]
}

Rules for each field:
  - "name" MUST be an exact match to one of the Available binaries below.
  - "basis" is free-form prose describing the reason to promote. Avoid jargon.
  - "toolbox" MUST be one of the 8 enumerated values. Pick "general" when
    unsure.
  - "hashtags" items SHOULD come from the taxonomy lexicon when relevant; the
    registry silently drops unknown tokens, so err on the side of omission.
`

// CuratorOutput is the JSON shape emitted by the curator LLM. Split out so
// tests and alternate implementations can share it without pulling the full
// awakens surface.
type CuratorOutput struct {
	Picks []AwakeningToolRegister `json:"picks"`
}

// ParseCuratorOutput decodes the LLM's JSON and returns a validated slice of
// tool-register entries ready for RegisterBatch. Tolerates markdown-fenced or
// prose-wrapped JSON because OpenRouter + Gemini do not always honour
// RequireJSON when tools are attached.
func ParseCuratorOutput(raw []byte, availableBinaries map[string]struct{}) ([]AwakeningToolRegister, error) {
	candidate := raw
	var out CuratorOutput
	if err := json.Unmarshal(candidate, &out); err != nil {
		extracted, ok := extractJSONObject(raw)
		if !ok {
			return nil, fmt.Errorf("%w: curator decode: %v", ErrInvalidReport, err)
		}
		candidate = extracted
		if err := json.Unmarshal(candidate, &out); err != nil {
			return nil, fmt.Errorf("%w: curator decode after extract: %v", ErrInvalidReport, err)
		}
	}
	if len(out.Picks) > MaxToolsToRegister {
		out.Picks = out.Picks[:MaxToolsToRegister]
	}
	filtered := make([]AwakeningToolRegister, 0, len(out.Picks))
	seen := make(map[string]struct{}, len(out.Picks))
	for _, p := range out.Picks {
		name := strings.TrimSpace(p.Name)
		if !toolNameRe.MatchString(name) {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		if availableBinaries != nil {
			if _, present := availableBinaries[name]; !present {
				continue
			}
		}
		seen[name] = struct{}{}
		filtered = append(filtered, AwakeningToolRegister{
			Name:     name,
			Basis:    strings.TrimSpace(p.Basis),
			Toolbox:  canonicaliseToolbox(p.Toolbox),
			Hashtags: p.Hashtags,
		})
	}
	return filtered, nil
}

// BuildCurateUserMessage serialises the probe-only report into the concise
// user-turn text the curator LLM consumes. Binaries are split into
// present/absent, sorted, and rendered one-per-line so the model can scan
// them. Capabilities (few, short) come after.
func BuildCurateUserMessage(r AwakeningReport) string {
	var b strings.Builder
	b.WriteString("Awakening probe results for this host:\n\n")

	present := make([]string, 0, len(r.PresentTools))
	for _, t := range r.PresentTools {
		present = append(present, t.Name)
	}
	sort.Strings(present)
	absent := append([]string(nil), r.AbsentTools...)
	sort.Strings(absent)

	if len(present) > 0 {
		b.WriteString("Available:\n")
		for _, n := range present {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	} else {
		b.WriteString("Available: (none)\n")
	}
	if len(absent) > 0 {
		b.WriteString("\nNOT available:\n")
		for _, n := range absent {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	if len(r.Capabilities) > 0 {
		b.WriteString("\nCapabilities:\n")
		for _, c := range r.Capabilities {
			status := "no"
			if c.Satisfied {
				status = "yes"
			}
			fmt.Fprintf(&b, "  - %s: %s\n", c.Name, status)
		}
	}
	b.WriteString("\nReturn the JSON object now.")
	return b.String()
}

// newCurateTransition constructs the t-awaken-curate tool transition. It
// consumes the probe-only AwakeningReport, invokes deps.LLMCurator, merges the
// returned ToolsRegister into the report, and passes the enriched report
// forward. When the curator is unavailable or errors, the transition degrades
// to a no-op merge (probe-only report passes through untouched) so the boot
// path never fails on curation.
func newCurateTransition(deps Deps) *cpn.Transition {
	emitter := deps.Emitter
	t := cpn.NewTransition(
		TransitionAwakenCurate,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningReportRaw},
		[]string{PlaceAwakeningReport},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenCurate)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}

		if deps.LLMCurator == nil {
			slog.InfoContext(ctx, "awakens.curate.skipped",
				"reason", "no_curator_wired",
			)
			if emitter != nil {
				emitter.Curated(ctx, 0, "no_curator")
			}
			return map[string]cpn.Token{
				PlaceAwakeningReport: {
					Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: report,
				},
			}, nil
		}

		picks, cErr := deps.LLMCurator(ctx, report)
		if cErr != nil {
			slog.WarnContext(ctx, "awakens.curate.error",
				"error", cErr,
			)
			if emitter != nil {
				emitter.Curated(ctx, 0, "curator_error")
			}
			// Degrade: pass the probe-only report forward untouched.
			return map[string]cpn.Token{
				PlaceAwakeningReport: {
					Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: report,
				},
			}, nil
		}

		merged := mergeCurationIntoReport(report, picks)
		slog.InfoContext(ctx, "awakens.curate.completed",
			"picked", len(merged.ToolsRegister),
			"present_binaries", len(report.PresentTools),
		)
		if emitter != nil {
			emitter.Curated(ctx, len(merged.ToolsRegister), "ok")
		}
		return map[string]cpn.Token{
			PlaceAwakeningReport: {
				Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: merged,
			},
		}, nil
	}
	return t
}

// mergeCurationIntoReport returns a copy of report with ToolsRegister
// populated from picks. Entries are filtered against the set of Present
// binaries so a hallucinated name never reaches RegisterBatch. Deduplicated
// by name.
func mergeCurationIntoReport(report AwakeningReport, picks []AwakeningToolRegister) AwakeningReport {
	available := make(map[string]struct{}, len(report.PresentTools))
	for _, p := range report.PresentTools {
		available[p.Name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(picks))
	out := report
	out.ToolsRegister = make([]AwakeningToolRegister, 0, len(picks))
	for _, p := range picks {
		name := strings.TrimSpace(p.Name)
		if !toolNameRe.MatchString(name) {
			continue
		}
		if _, ok := available[name]; !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out.ToolsRegister = append(out.ToolsRegister, AwakeningToolRegister{
			Name:     name,
			Basis:    strings.TrimSpace(p.Basis),
			Toolbox:  canonicaliseToolbox(p.Toolbox),
			Hashtags: p.Hashtags,
		})
	}
	return out
}

// ErrCuratorUnavailable signals that the LLM curator was not injected. The
// curate transition treats this as a no-op (pass-through), but callers that
// want to fail closed can branch on it.
var ErrCuratorUnavailable = errors.New("awakens: llm curator not wired")
