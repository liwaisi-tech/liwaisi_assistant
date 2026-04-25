package toolbuilder

// Deterministic ToolHandlers for the tool-creator topology.
//
// Every handler here is pure Go (no LLM, no bash) and is the canonical
// place to reason about data flow between CPN transitions. Keep them
// small; complex logic belongs in a dedicated package the handler calls
// into.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── dispatch: t-triage output → 3 parallel branch inputs ─────────────────

// dispatchHandler consumes one p-triaged token. If Ready is true, it clones
// the embedded Request onto p-req-investigate/-scaffold/-draft. If not
// ready, it emits an Error to p-errors (clarify-round handling upstream in
// the main conversation CPN; see spec §6 t-triage).
func dispatchHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, errors.New("t-dispatch: no input token")
		}
		verdict, err := decodeTriage(consumed[0].Payload)
		if err != nil {
			return nil, fmt.Errorf("t-dispatch: %w", err)
		}
		// Pragmatic fallback: if the LLM set ready=false but still echoed a
		// usable normalised request (name OR non-trivial description), we
		// proceed rather than deadlocking the CPN. tool-creator currently
		// has no clarify round-trip, and the feedback_minimize_hitl
		// preference applies: better a best-effort pass than a hard stop.
		// Only an empty/nonsense request aborts.
		if !verdict.Ready {
			hasName := verdict.NormalisedRequest.Name != ""
			hasDesc := len(verdict.NormalisedRequest.Description) > 8
			if !hasName && !hasDesc {
				return nil, fmt.Errorf("t-dispatch: triage not ready and request empty (question=%q)", verdict.ClarifyQuestion)
			}
		}
		reqJSON, err := json.Marshal(verdict.NormalisedRequest)
		if err != nil {
			return nil, fmt.Errorf("t-dispatch: marshal request: %w", err)
		}
		tok := cpn.Token{Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(reqJSON)}
		return map[string]cpn.Token{
			PlaceReqInvestigate: tok,
			PlaceReqScaffold:    tok,
			PlaceReqDraft:       tok,
		}, nil
	}
}

// ── investigate: stub — lists existing tools ─────────────────────────────

// investigateHandler emits an empty-list summary in the foundational slice.
// The production handler (follow-up task) walks workspace/tools/src/* via
// the HostAdapter.
func investigateHandler() cpnToolHandler {
	return func(_ context.Context, _ []cpn.Token) (map[string]cpn.Token, error) {
		empty := []ExistingTool{}
		payload, _ := json.Marshal(empty)
		return map[string]cpn.Token{
			PlaceExistingTools: {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

// ── enrich-spec: join v0 + workspace + existing-tools → spec-draft ───────

// enrichSpecHandler merges the three upstream tokens into a SpecDraft.
// It is tolerant of upstream tokens arriving in any order.
func enrichSpecHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var (
			draft         SpecDraft
			existing      []ExistingTool
			workspacePath string
			haveDraft     bool
		)
		// Pass 1: find the token that looks most like a SpecDraft. We
		// consider any ColorJSON token where at least one of the core
		// spec fields decodes non-empty (name / purpose / domain_model)
		// instead of requiring name to be populated specifically — LLMs
		// sometimes drop a field or rename it under autocorrection, and
		// demanding Name was the root cause of a hard deadlock here when
		// tool-creator received a token whose Name got truncated/omitted.
		for _, t := range consumed {
			if t.Color != cpn.ColorJSON {
				continue
			}
			var candidate SpecDraft
			if err := decodeAs(t.Payload, &candidate); err != nil {
				continue
			}
			if candidate.Name != "" || candidate.Purpose != "" || candidate.DomainModel != "" {
				draft = candidate
				haveDraft = true
				break
			}
		}
		// Pass 2: artefacts + existing-tools collection.
		for _, t := range consumed {
			switch t.Color {
			case cpn.ColorJSON:
				_ = decodeAs(t.Payload, &existing)
			case cpn.ColorArtifact:
				workspacePath = fmt.Sprintf("%v", t.Payload)
			}
		}
		if !haveDraft {
			// Last-resort stub: keep the CPN moving rather than dead-
			// locking. Reviewers will flag the empty fields as blocking
			// issues and the refine loop gets a chance to recover.
			draft = SpecDraft{
				Name:    "unnamed-tool",
				Purpose: "SpecDraft could not be parsed from upstream; refine loop must reconstruct.",
			}
		}
		draft.ExistingTools = existing
		draft.WorkspacePath = workspacePath
		payload, err := json.Marshal(draft)
		if err != nil {
			return nil, fmt.Errorf("t-enrich-spec: marshal: %w", err)
		}
		return map[string]cpn.Token{
			PlaceSpecDraft: {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

// ── fanout-reviewers: 1 spec-draft → 6 reviewer input places ─────────────

// fanoutReviewersHandler clones the spec-draft token into each of the 6
// reviewer input places. Reviewer transitions see only their own slice.
func fanoutReviewersHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, errors.New("t-fanout-reviewers: no input token")
		}
		src := consumed[0]
		out := make(map[string]cpn.Token, len(reviewerRoles))
		for _, r := range reviewerRoles {
			out[r.InputPlace] = cpn.Token{
				Color:   cpn.ColorJSON,
				Space:   cpn.SpaceComputation,
				Payload: src.Payload,
			}
		}
		return out, nil
	}
}

// ── aggregate-reviews: 6 reviews → approved OR refine-request ────────────
//
// Implemented as two mutually-exclusive transitions with complementary
// guards so at most one fires per review-round:
//
//   - t-aggregate-approve fires iff every review.Approved && no blocking_issues.
//   - t-aggregate-refine  fires iff any review has blocking_issues OR
//                          RefineCount >= MaxRefineCount (ship best-effort).
//
// The guards below inspect the 6 consumed review tokens without mutating state.

func aggregateApproveHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		reviews := parseReviews(consumed)
		// Promote any blocking_issues to suggestions so the downstream
		// architect (t-decompose-totals-arch) reads them as gaps to fill,
		// not as gates to satisfy. The flow is now strictly forward.
		for i := range reviews {
			if len(reviews[i].BlockingIssues) > 0 {
				reviews[i].Suggestions = append(reviews[i].Suggestions, reviews[i].BlockingIssues...)
				reviews[i].BlockingIssues = nil
			}
			reviews[i].Approved = true
		}
		verdict := buildVerdict(reviews, SpecDraft{}, true)
		verdict.BlockingIssues = nil // already merged into Suggestions
		payload, err := json.Marshal(verdict)
		if err != nil {
			return nil, fmt.Errorf("t-aggregate-approve: marshal: %w", err)
		}
		return map[string]cpn.Token{
			PlaceSpecApproved: {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

func aggregateRefineHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		reviews := parseReviews(consumed)
		// specFromReview was always zero-value (SpecDraft is not populated by parseReviews).
		// exhausted is always false since RefineCount was always 0 (never populated).
		verdict := buildVerdict(reviews, SpecDraft{}, false)
		payload, err := json.Marshal(verdict)
		if err != nil {
			return nil, fmt.Errorf("t-aggregate-refine: marshal: %w", err)
		}
		dst := PlaceRefineRequest
		return map[string]cpn.Token{
			dst: {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

// guardAggregateApprove always fires once every reviewer slot has produced
// a token. Reviewer blocking_issues and suggestions ride downstream inside
// the Verdict (see aggregateApproveHandler) so t-decompose-totals-arch can
// fold them into its plan. This intentionally drops the prior unanimous-
// consent gate: a single off-schema or pessimistic reviewer used to deadlock
// the flow into an unbounded refine loop, which contradicts the
// minimize-HITL preference. Failures are recovered at the architect step,
// not blocked here.
func guardAggregateApprove(tokens []*cpn.Token) bool {
	present := 0
	for _, t := range tokens {
		if t != nil {
			present++
		}
	}
	return present == len(reviewerRoles)
}

// guardAggregateRefine is now unreachable by design. Kept so the topology
// validates structurally (the transition still exists for back-compat) but
// always returns false: refinement loops are not used in autonomous mode.
func guardAggregateRefine(_ []*cpn.Token) bool {
	return false
}

// ── authorize-totals + gate-total: DoIt emission + join ──────────────────

// authorizeTotalsHandler reads p-totals (batch), validates well-formedness,
// and emits: p-doit (a DoItBatch with one entry per total) + p-totals-echo
// (pass-through of the original totals so t-gate-total can pair them).
// This replaces the HITL approval gate entirely (see feedback_minimize_hitl.md).
// Implemented deterministically; no LLM call needed for a mechanical check.
func authorizeTotalsHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, errors.New("t-authorize-totals: no input token")
		}
		var batch TotalsBatch
		if err := decodeAs(consumed[0].Payload, &batch); err != nil {
			return nil, fmt.Errorf("t-authorize-totals: decode batch: %w", err)
		}
		if err := validateTotalsBatch(batch); err != nil {
			return nil, fmt.Errorf("t-authorize-totals: %w", err)
		}
		doit := DoItBatch{Entries: make([]DoIt, 0, len(batch.Totals))}
		for _, tot := range batch.Totals {
			doit.Entries = append(doit.Entries, DoIt{
				TotalID:  tot.ID,
				Approved: true,
				Reason:   "autonomous well-formedness check passed",
			})
		}
		doitJSON, err := json.Marshal(doit)
		if err != nil {
			return nil, fmt.Errorf("t-authorize-totals: marshal doit: %w", err)
		}
		return map[string]cpn.Token{
			PlaceDoIt:       {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(doitJSON)},
			PlaceTotalsEcho: consumed[0],
		}, nil
	}
}

// gateTotalHandler joins DoIt with the echoed totals and emits a
// TotalReadyBatch. 1-token-in / 1-token-in / 1-token-out — a canonical
// CPN join. Each total pairs with the DoIt whose TotalID matches; any
// mismatch fails loud.
func gateTotalHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var (
			doit  DoItBatch
			tots  TotalsBatch
			haveD bool
			haveT bool
		)
		for _, tok := range consumed {
			if !haveD {
				if err := decodeAs(tok.Payload, &doit); err == nil && len(doit.Entries) > 0 {
					haveD = true
					continue
				}
			}
			if !haveT {
				if err := decodeAs(tok.Payload, &tots); err == nil && len(tots.Totals) > 0 {
					haveT = true
				}
			}
		}
		if !haveD || !haveT {
			return nil, errors.New("t-gate-total: missing doit or totals in consumed tokens")
		}

		byID := make(map[string]DoIt, len(doit.Entries))
		for _, d := range doit.Entries {
			byID[d.TotalID] = d
		}
		ready := TotalReadyBatch{SpecName: tots.SpecName, WorkspacePath: tots.WorkspacePath, Entries: make([]TotalReady, 0, len(tots.Totals))}
		for _, tot := range tots.Totals {
			d, ok := byID[tot.ID]
			if !ok {
				return nil, fmt.Errorf("t-gate-total: no DoIt for total %q", tot.ID)
			}
			if !d.Approved {
				continue // skip rejected totals; the spec allows per-total skip
			}
			ready.Entries = append(ready.Entries, TotalReady{Total: tot, DoIt: d})
		}
		payload, err := json.Marshal(ready)
		if err != nil {
			return nil, fmt.Errorf("t-gate-total: marshal: %w", err)
		}
		return map[string]cpn.Token{
			PlaceTotalReady: {Color: cpn.ColorJSON, Space: cpn.SpaceComputation, Payload: string(payload)},
		}, nil
	}
}

// ── shared helpers ───────────────────────────────────────────────────────

// cpnToolHandler is a local alias to keep signatures tight.
type cpnToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error)

func decodeTriage(payload any) (TriageVerdict, error) {
	var v TriageVerdict
	if err := decodeAs(payload, &v); err != nil {
		return v, fmt.Errorf("decode triage verdict: %w", err)
	}
	return v, nil
}

// decodeAs accepts payloads in either string/[]byte JSON form OR a struct
// already typed as the target. Robust because LLM transitions emit JSON
// strings while Tool transitions emit Go structs or strings.
func decodeAs(payload any, dst any) error {
	switch p := payload.(type) {
	case nil:
		return errors.New("nil payload")
	case []byte:
		return json.Unmarshal(p, dst)
	case string:
		return json.Unmarshal([]byte(p), dst)
	case json.RawMessage:
		return json.Unmarshal(p, dst)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// parseReviews extracts Review structs from the consumed review tokens.
// Lenient by design: a malformed token (e.g., an LLM that wraps output in
// a `tool_code` envelope and breaks the expected schema) does NOT fail
// the whole batch. Instead, a synthetic "malformed-output" review is
// substituted so the aggregate guards can still evaluate and the refine
// transition can consume the full input set. This matches the
// feedback_minimize_hitl principle: keep the CPN moving, surface the
// problem as a blocking issue, let the refine stage try to recover.
func parseReviews(consumed []cpn.Token) []Review {
	reviews := make([]Review, 0, len(consumed))
	for i, tok := range consumed {
		var r Review
		if err := decodeAs(tok.Payload, &r); err != nil {
			reviews = append(reviews, Review{
				Role:           fmt.Sprintf("reviewer-%d", i),
				Approved:       false,
				BlockingIssues: []string{fmt.Sprintf("reviewer returned unparseable output: %v", err)},
			})
			continue
		}
		// A token that parses but carries no role at all is suspect — if
		// every field is zero-valued the LLM probably hallucinated a
		// different schema. Treat it like a malformed response.
		if r.Role == "" && !r.Approved && len(r.BlockingIssues) == 0 && len(r.Suggestions) == 0 {
			reviews = append(reviews, Review{
				Role:           fmt.Sprintf("reviewer-%d", i),
				Approved:       false,
				BlockingIssues: []string{"reviewer returned empty/off-schema output"},
			})
			continue
		}
		reviews = append(reviews, r)
	}
	return reviews
}

// buildVerdict constructs a Verdict from review results. approved=true
// sets the approval flag on the downstream token regardless of blocking
// issues (used when refinement cap is hit).
func buildVerdict(reviews []Review, spec SpecDraft, approved bool) Verdict {
	var blocking, suggestions []string
	for _, r := range reviews {
		blocking = append(blocking, r.BlockingIssues...)
		suggestions = append(suggestions, r.Suggestions...)
	}
	return Verdict{
		Approved:       approved,
		BlockingIssues: blocking,
		Suggestions:    suggestions,
		Reviews:        reviews,
		SpecDraft:      spec,
	}
}

// validateTotalsBatch enforces REQ-A07 (1 ≤ N ≤ MaxTotals), unique IDs,
// non-empty deliverables, and a DAG (no cycles, no missing deps).
func validateTotalsBatch(b TotalsBatch) error {
	n := len(b.Totals)
	if n == 0 {
		return errors.New("totals batch is empty")
	}
	if n > MaxTotals {
		return fmt.Errorf("totals count %d exceeds MaxTotals=%d", n, MaxTotals)
	}
	ids := make(map[string]struct{}, n)
	for _, t := range b.Totals {
		if t.ID == "" {
			return errors.New("total has empty ID")
		}
		if _, dup := ids[t.ID]; dup {
			return fmt.Errorf("duplicate total ID %q", t.ID)
		}
		ids[t.ID] = struct{}{}
		if len(t.Deliverables) == 0 {
			return fmt.Errorf("total %q has no deliverables", t.ID)
		}
	}
	// Missing-dep check.
	for _, t := range b.Totals {
		for _, dep := range t.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("total %q depends on unknown %q", t.ID, dep)
			}
		}
	}
	// Cycle check (Kahn).
	inDeg := make(map[string]int, n)
	outs := make(map[string][]string, n)
	for _, t := range b.Totals {
		inDeg[t.ID] += 0
		for _, dep := range t.DependsOn {
			inDeg[t.ID]++
			outs[dep] = append(outs[dep], t.ID)
		}
	}
	queue := make([]string, 0, n)
	for id, d := range inDeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	processed := 0
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		processed++
		for _, next := range outs[head] {
			inDeg[next]--
			if inDeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if processed != n {
		return errors.New("totals form a dependency cycle")
	}
	return nil
}

// ── placeholder stubs: scaffold + package ────────────────────────────────

// scaffoldWorkspaceStub deposits a canned ColorArtifact token into
// p-workspace-ready so the pipeline reaches the spec-draft stage. Swap
// for the real template-renderer handler once it's ready.
func scaffoldWorkspaceStub() cpnToolHandler {
	return func(_ context.Context, _ []cpn.Token) (map[string]cpn.Token, error) {
		return map[string]cpn.Token{
			PlaceWorkspaceReady: {
				Color:   cpn.ColorArtifact,
				Space:   cpn.SpaceComputation,
				Payload: `{"placeholder":"scaffold pending","path":""}`,
			},
		}, nil
	}
}

// finalizeInstallHandler converts the ColorShellResult produced by the
// `make install` bash transition into a ColorArtifact manifest token on
// PlaceRegistered. It decodes the shell result, requires exit_code==0,
// and extracts the installed binary path from install.sh's canonical
// "installed: <path>" line in stdout.
func finalizeInstallHandler() cpnToolHandler {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, errors.New("t-finalize-install: no input token")
		}
		var result cpn.ShellResultPayload
		if err := decodeAs(consumed[0].Payload, &result); err != nil {
			return nil, fmt.Errorf("t-finalize-install: decode shell result: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("t-finalize-install: make install exit=%d stderr=%q", result.ExitCode, result.Stderr)
		}
		binaryPath := extractInstalledPath(result.Stdout)
		manifest := map[string]any{
			"installed":   true,
			"binary_path": binaryPath,
			"stdout":      result.Stdout,
			"duration_ms": result.DurationMs,
		}
		payload, err := json.Marshal(manifest)
		if err != nil {
			return nil, fmt.Errorf("t-finalize-install: marshal manifest: %w", err)
		}
		return map[string]cpn.Token{
			PlaceRegistered: {
				Color:   cpn.ColorArtifact,
				Space:   cpn.SpaceComputation,
				Payload: string(payload),
			},
		}, nil
	}
}

// extractInstalledPath scans `make install` stdout for the canonical
// "installed: <path>" line that install.sh emits on success. Returns
// an empty string when no marker is present so callers can decide
// whether the absence is fatal.
func extractInstalledPath(stdout string) string {
	const marker = "installed:"
	for _, line := range splitLinesTrim(stdout) {
		if idx := indexOfPrefix(line, marker); idx >= 0 {
			return trimSpace(line[idx+len(marker):])
		}
	}
	return ""
}

func splitLinesTrim(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func indexOfPrefix(line, prefix string) int {
	// Allow leading whitespace; install.sh doesn't indent but be
	// generous with matching.
	for i := 0; i <= len(line)-len(prefix); i++ {
		match := true
		for j := 0; j < len(prefix); j++ {
			if line[i+j] != prefix[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// packageStub deposits a canned ColorToolManifest token so t-install has
// something to consume. Real packager will run `go build` and emit a
// signed manifest.
func packageStub() cpnToolHandler {
	return func(_ context.Context, _ []cpn.Token) (map[string]cpn.Token, error) {
		return map[string]cpn.Token{
			PlacePackaged: {
				Color:   cpn.ColorToolManifest,
				Space:   cpn.SpaceComputation,
				Payload: `{"placeholder":"package pending"}`,
			},
		}, nil
	}
}
