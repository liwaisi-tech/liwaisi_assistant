package cpn

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultContextWindowSize is the default number of conversational turns
// to include in the sliding window (T3). 10 turns = 20 messages.
const DefaultContextWindowSize = 10

// CPNRoleLedger is the sentinel CPNRole value used to discriminate ledger
// entries from ordinary observer summaries on the existing messages table.
// See spec-architecture-brae-context-and-tool-hygiene.md §4.3.
//
// Storing the marker in CPNRole (rather than a new metadata column) keeps
// the persistence layer additive — no schema migration required, since
// CPNRole is already a column on messages and round-trips through
// persist/topology.go.
const CPNRoleLedger = "__ledger__"

// TransitionRole identifies the kind-of-work a transition performs, used
// to resolve a per-role ContextPolicy. It is independent of the CPN's
// runtime role label (which is free-form) — only the canonical set below
// gets first-class context-policy treatment; everything else falls back
// to RoleAssistantTransition.
type TransitionRole string

const (
	// RoleAssistantTransition is the default user-facing role: full
	// persona, full tool catalogue, workspace preamble, ledger surfaced.
	RoleAssistantTransition TransitionRole = "assistant"

	// RoleClassifyTransition is a short-context, no-persona role used
	// for routers, intent classifiers, and label predictors.
	RoleClassifyTransition TransitionRole = "classify"

	// RoleValidateTransition is for schema/lint/policy checks that
	// require no persona and no history.
	RoleValidateTransition TransitionRole = "validate"

	// RoleSynthesizeTransition is for output-shaping passes that need
	// minimal tone consistency but no full persona.
	RoleSynthesizeTransition TransitionRole = "synthesize"

	// RoleArchitectTransition is for the CPN architect: planning + tool
	// selection without the assistant's chat persona.
	RoleArchitectTransition TransitionRole = "architect"

	// RolePlanTransition is for plan-only transitions.
	RolePlanTransition TransitionRole = "plan"
)

// ContextPolicy controls how BuildContextWithPolicy assembles the LLM
// context for a single transition. Resolved per TransitionRole at the
// fire_llm callsite; see ContextPolicyResolver.
type ContextPolicy struct {
	// SystemPrompt is the rendered T1. When empty, BuildContextWithPolicy
	// falls back to the role-default prompt loaded from cpn/prompts/roles.
	SystemPrompt string

	// RawWindowTurns sets the T3 sliding-window size (turns; 1 turn = 2
	// messages). 0 disables T3 entirely. Negative values are normalised
	// to 0.
	RawWindowTurns int

	// IncludeWorkspacePreamble, when true, prepends a short
	// "[workspace state @ <ts>]" block built from the session's ledger
	// entries to the system prompt. Costs nothing if no ledger entries
	// exist in history.
	IncludeWorkspacePreamble bool

	// IncludeLedger, when true, surfaces ledger-marked messages in T2
	// alongside ordinary observer summaries. Defaults true for the
	// assistant role; defaults false for classifier-style roles where
	// the ledger would be noise.
	IncludeLedger bool
}

// ContextPolicyResolver maps a TransitionRole to the policy that should
// govern its context window. Implementations MUST be safe for concurrent
// use; the default resolver returned by NewDefaultPolicyResolver is.
type ContextPolicyResolver interface {
	For(role TransitionRole) ContextPolicy
}

// defaultPolicyResolver is the immutable, table-backed resolver used when
// callers do not supply their own.
type defaultPolicyResolver struct{}

// NewDefaultPolicyResolver returns the canonical resolver. It encodes the
// role budgets called out in the spec (REQ-002, GUD-001).
func NewDefaultPolicyResolver() ContextPolicyResolver { return defaultPolicyResolver{} }

// TransitionRoleFromHint maps an authored LLMConfig.Role string (which
// today serves model-routing purposes — values like "classifier",
// "structured", "synthesizer") to the TransitionRole used for context
// policy resolution. Empty or unknown hints fall back to the assistant
// role so existing topologies keep their pre-spec behaviour exactly.
//
// The mapping is intentionally lenient: substring matches let topology
// authors use any of "classifier", "classify", "router", "intent" and
// land in the same bucket.
func TransitionRoleFromHint(hint string) TransitionRole {
	switch {
	case hint == "":
		return RoleAssistantTransition
	case containsAny(hint, "classif", "router", "intent", "label"):
		return RoleClassifyTransition
	case containsAny(hint, "valid", "lint", "check"):
		return RoleValidateTransition
	case containsAny(hint, "synth"):
		return RoleSynthesizeTransition
	case containsAny(hint, "architect"):
		return RoleArchitectTransition
	case containsAny(hint, "plan"):
		return RolePlanTransition
	default:
		return RoleAssistantTransition
	}
}

// containsAny reports whether s contains any of the substrings. Cheap
// case-insensitive scan; called once per LLM transition firing.
func containsAny(s string, subs ...string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(low, sub) {
			return true
		}
	}
	return false
}

// For returns the canonical ContextPolicy for role. Unknown roles fall
// back to the assistant policy so adding a new role label is never a
// breaking change — it just inherits the safe default.
func (defaultPolicyResolver) For(role TransitionRole) ContextPolicy {
	switch role {
	case RoleClassifyTransition, RoleValidateTransition:
		return ContextPolicy{
			RawWindowTurns:           4,
			IncludeWorkspacePreamble: false,
			IncludeLedger:            false,
		}
	case RoleSynthesizeTransition, RolePlanTransition, RoleArchitectTransition:
		return ContextPolicy{
			RawWindowTurns:           8,
			IncludeWorkspacePreamble: false,
			IncludeLedger:            true,
		}
	default: // RoleAssistantTransition + unknown
		return ContextPolicy{
			RawWindowTurns:           DefaultContextWindowSize,
			IncludeWorkspacePreamble: true,
			IncludeLedger:            true,
		}
	}
}

// ContextWindow holds the assembled context for a single LLM call.
// Built by BuildContext / BuildContextWithPolicy and used to create an
// LLMRequest.
type ContextWindow struct {
	// SystemPrompt is the T1 tier — static per transition, optionally
	// prepended with a [workspace state @ ts] block.
	SystemPrompt string

	// Messages is the assembled message list: T2 observers + ledger +
	// T3 raw window.
	Messages []*LLMMessage

	// InputTokenEstimate is the rough token count for budget checking.
	InputTokenEstimate int
}

// BuildContext is the legacy entry point preserved for back-compat with
// existing callers and tests. It applies the assistant-role policy with
// the supplied systemPrompt and contextWindowSize, and disables the
// workspace preamble (legacy callers don't expect injected lines).
//
// New code SHOULD call BuildContextWithPolicy directly.
func BuildContext(systemPrompt string, history []*Message, contextWindowSize int) ContextWindow {
	policy := ContextPolicy{
		SystemPrompt:             systemPrompt,
		RawWindowTurns:           contextWindowSize,
		IncludeWorkspacePreamble: false,
		IncludeLedger:            true,
	}
	return BuildContextWithPolicy(RoleAssistantTransition, history, policy)
}

// BuildContextWithPolicy assembles the context window for an LLM
// transition under the supplied ContextPolicy.
//
// Three-tier assembly:
//
//	T1: policy.SystemPrompt — optionally prepended with a workspace
//	    state preamble built from ledger entries when
//	    policy.IncludeWorkspacePreamble is true.
//	T2: All RoleObserver messages from history. When
//	    policy.IncludeLedger is true, ledger-marked messages
//	    (CPNRole == CPNRoleLedger) are also surfaced verbatim.
//	T3: Last policy.RawWindowTurns*2 raw (RoleUser + RoleAssistant)
//	    messages — sliding window. 0 or negative disables T3.
//
// Messages are ordered: T2 first (observers + ledger interleaved by
// timestamp), then T3 raw. Does not modify history.
func BuildContextWithPolicy(role TransitionRole, history []*Message, policy ContextPolicy) ContextWindow {
	_ = role // reserved for future role-specific shaping; resolver picked policy already

	// T2: observer summaries + (optionally) ledger entries.
	var t2 []*LLMMessage
	for _, m := range history {
		switch {
		case m.Role == RoleObserver && m.CPNRole != CPNRoleLedger:
			t2 = append(t2, &LLMMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("[Summary from %s]: %s", m.CPNRole, m.Content),
			})
		case policy.IncludeLedger && m.CPNRole == CPNRoleLedger:
			// Ledger lines stand on their own — already shaped as
			// "[ok] verb target (size)" or "[fail] ... → reason".
			// Wrapping them in "[Summary from ...]" would dilute the
			// shape the LLM needs to recognise.
			t2 = append(t2, &LLMMessage{
				Role:    "assistant",
				Content: m.Content,
			})
		}
	}

	// T3: sliding window of raw messages.
	//
	// Messages tagged ephemeral (CPNRoleEphemeral) are filtered out:
	// they carry verbose, single-use tool output (e.g. a 500-row `ls`)
	// that the LLM already consumed in its producing turn and that
	// would otherwise eat sliding-window slots in subsequent turns
	// (REQ-004).
	var t3 []*LLMMessage
	if policy.RawWindowTurns > 0 {
		raw := filterRawNonEphemeral(history)
		limit := policy.RawWindowTurns * 2
		if len(raw) > limit {
			raw = raw[len(raw)-limit:]
		}
		t3 = make([]*LLMMessage, 0, len(raw))
		for _, m := range raw {
			t3 = append(t3, &LLMMessage{
				Role:    string(m.Role),
				Content: sanitizeForLLM(m.Content),
			})
		}
	}

	// T1: optional workspace preamble.
	systemPrompt := policy.SystemPrompt
	if policy.IncludeWorkspacePreamble {
		if pre := buildWorkspacePreamble(history, time.Now()); pre != "" {
			if systemPrompt == "" {
				systemPrompt = pre
			} else {
				systemPrompt = pre + "\n\n" + systemPrompt
			}
		}
	}

	messages := make([]*LLMMessage, 0, len(t2)+len(t3))
	messages = append(messages, t2...)
	messages = append(messages, t3...)

	return ContextWindow{
		SystemPrompt:       systemPrompt,
		Messages:           messages,
		InputTokenEstimate: estimateTokens(systemPrompt, messages),
	}
}

// a2uiMarker is the prefix fireHITL writes to History rows that carry a
// rendered A2UI surface. It must be kept in sync with the frontend constant
// (front/react-assistant/src/features/chat/a2ui/constants.ts).
const a2uiMarker = "$$a2ui:"

// sanitizeForLLM rewrites a History row's content before it is added to the
// LLM context window. Today this strips any A2UI surface payload — the LLM
// has no use for the raw JSON, and seeing the literal "$$a2ui:" marker
// teaches it to mimic the syntax in plain-text replies (which then breaks
// frontend rendering, since "$$…$$" is treated as KaTeX math).
//
// The replacement keeps the turn observable to the model ("a review form
// was shown") without leaking the wire format. Untouched content passes
// through unchanged so this stays cheap on the hot path.
func sanitizeForLLM(content string) string {
	prefix, _, found := strings.Cut(content, a2uiMarker)
	if !found {
		return content
	}
	return strings.TrimRight(prefix, " \t\n\r") + "\n[A2UI surface rendered to user]"
}

// filterRaw returns only RoleUser and RoleAssistant messages from history.
// Ledger-tagged messages and observer summaries are excluded. Retained
// for the legacy BuildContext signature; callers using ContextPolicy
// should prefer filterRawNonEphemeral.
func filterRaw(history []*Message) []*Message {
	result := make([]*Message, 0, len(history))
	for _, m := range history {
		if m.Role == RoleUser || m.Role == RoleAssistant {
			result = append(result, m)
		}
	}
	return result
}

// filterRawNonEphemeral is filterRaw plus an ephemeral skip. Ephemeral
// messages carry CPNRoleEphemeral and represent tool outputs that
// should not survive their producing turn in T3.
func filterRawNonEphemeral(history []*Message) []*Message {
	result := make([]*Message, 0, len(history))
	for _, m := range history {
		if m == nil {
			continue
		}
		if m.Role != RoleUser && m.Role != RoleAssistant {
			continue
		}
		if m.CPNRole == CPNRoleEphemeral {
			continue
		}
		result = append(result, m)
	}
	return result
}

// estimateTokens returns a rough token count for budget checking.
// Uses len(content)/4 as the char-to-token ratio, plus 4 tokens overhead per message.
func estimateTokens(systemPrompt string, messages []*LLMMessage) int {
	tokens := len(systemPrompt) / 4
	for _, m := range messages {
		tokens += len(m.Content)/4 + 4
	}
	return tokens
}

// CompressSubNetSummary generates a RoleObserver message summarizing
// a completed sub-CPN's output. Called when a sub-CPN reaches StateCompleted.
// The returned Message should be appended to the parent CPN's History.
func CompressSubNetSummary(childID, childRole string, childDepth int, outputTokens []Token) (*Message, error) {
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("compress sub-net summary: %w", err)
	}
	return &Message{
		ID:        id,
		Role:      RoleObserver,
		Content:   formatSummary(childRole, childDepth, outputTokens),
		CPNID:     childID,
		CPNRole:   childRole,
		CPNDepth:  childDepth,
		Timestamp: time.Now(),
	}, nil
}

// formatSummary generates a text summary from a child CPN's output tokens.
// Truncated at 500 characters to keep summaries compact.
func formatSummary(childRole string, childDepth int, outputTokens []Token) string {
	if len(outputTokens) == 0 {
		return fmt.Sprintf("[depth-%d %s] produced 0 output(s)", childDepth, childRole)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[depth-%d %s] produced %d output(s): ", childDepth, childRole, len(outputTokens))

	for i := range outputTokens {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%v", outputTokens[i].Payload)
		if b.Len() >= 500 {
			break
		}
	}

	s := b.String()
	if len(s) > 500 {
		s = s[:500]
		// Ensure we don't cut a multi-byte UTF-8 rune.
		for s != "" && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// newUUID generates a UUID v4 string using crypto/rand.
func newUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}

	// Set version 4.
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant (RFC 4122).
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// ── Workspace preamble ─────────────────────────────────────────────────────

// maxWorkspacePreambleEntries caps the preamble size so a session with
// hundreds of write operations does not blow out the system prompt.
// Older entries are dropped first; an [+N older entries] trailer is added.
const maxWorkspacePreambleEntries = 50

// stateChangingVerbs are the ledger verbs that produce durable artefacts
// worth surfacing in the workspace preamble. Read-only verbs are excluded
// even if they were (incorrectly) ledger-tagged, defensive against drift.
var stateChangingVerbs = map[string]struct{}{
	"write": {},
	"build": {},
	"chmod": {},
	"mv":    {},
	"mkdir": {},
	"cp":    {},
	"edit":  {},
}

// buildWorkspacePreamble walks the session history, extracts ledger
// entries for state-changing verbs, deduplicates by target keeping the
// most recent action, and renders a compact block. Returns "" when no
// qualifying entries exist.
//
// The generator is pure: it consumes history only and never shells out
// to the host (REQ-032). This keeps it deterministic, sandbox-safe, and
// cheap on the hot path.
func buildWorkspacePreamble(history []*Message, now time.Time) string {
	type entry struct {
		target  string
		summary string // short content fragment after the verb+target
		verb    string
		ts      time.Time
	}

	dedup := make(map[string]entry, 16)
	for _, m := range history {
		if m == nil || m.CPNRole != CPNRoleLedger {
			continue
		}
		verb, target, summary, ok := parseLedgerLine(m.Content)
		if !ok {
			continue
		}
		if _, isStateful := stateChangingVerbs[verb]; !isStateful {
			continue
		}
		// Most-recent-action wins per target.
		if prev, exists := dedup[target]; exists && prev.ts.After(m.Timestamp) {
			continue
		}
		dedup[target] = entry{target: target, summary: summary, verb: verb, ts: m.Timestamp}
	}

	if len(dedup) == 0 {
		return ""
	}

	entries := make([]entry, 0, len(dedup))
	for _, e := range dedup {
		entries = append(entries, e)
	}
	// Sort newest-first so trimming drops oldest entries.
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts.After(entries[j].ts) })

	dropped := 0
	if len(entries) > maxWorkspacePreambleEntries {
		dropped = len(entries) - maxWorkspacePreambleEntries
		entries = entries[:maxWorkspacePreambleEntries]
	}

	// Render with stable ordering: keep newest-first so the LLM sees
	// recently-touched paths first (likeliest to be relevant).
	var b strings.Builder
	fmt.Fprintf(&b, "[workspace state @ %s]\n", now.UTC().Format(time.RFC3339))
	for _, e := range entries {
		path := elideMiddle(e.target, 60)
		if e.summary != "" {
			fmt.Fprintf(&b, "%s  %s  %s %s\n",
				path, e.summary, e.verb, e.ts.UTC().Format("15:04"))
		} else {
			fmt.Fprintf(&b, "%s  %s %s\n",
				path, e.verb, e.ts.UTC().Format("15:04"))
		}
	}
	if dropped > 0 {
		fmt.Fprintf(&b, "[+%d older entries]\n", dropped)
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseLedgerLine extracts (verb, target, summary) from a ledger line.
// Recognises both shapes:
//
//	[ok] write ~/path/to/file (1247 bytes)
//	[fail] build ~/x → "strings imported and not used"
//
// Returns ok=false for malformed input — callers should silently skip.
func parseLedgerLine(content string) (verb, target, summary string, ok bool) {
	s := strings.TrimSpace(content)
	switch {
	case strings.HasPrefix(s, "[ok] "):
		s = s[len("[ok] "):]
	case strings.HasPrefix(s, "[fail] "):
		// Failures don't belong in the workspace preamble — they
		// represent absence of state, not presence. Return ok=false.
		return "", "", "", false
	default:
		return "", "", "", false
	}

	// Extract verb (first token).
	sp := strings.IndexByte(s, ' ')
	if sp <= 0 {
		return "", "", "", false
	}
	verb = s[:sp]
	rest := strings.TrimSpace(s[sp+1:])
	if rest == "" {
		return "", "", "", false
	}

	// Extract target (everything up to optional " (size...)" trailer).
	if openParen := strings.LastIndex(rest, " ("); openParen > 0 {
		target = strings.TrimSpace(rest[:openParen])
		summary = strings.TrimSpace(rest[openParen+1:])
	} else {
		target = rest
	}
	if target == "" {
		return "", "", "", false
	}
	return verb, target, summary, true
}

// elideMiddle shrinks s to at most max chars by replacing the middle
// with "...". Paths shorter than max pass through unchanged. Used so
// long /tmp/foo/bar/baz/.../file.go paths don't blow up the preamble.
func elideMiddle(s string, maxLen int) string {
	if maxLen < 8 || len(s) <= maxLen {
		return s
	}
	keep := (maxLen - 3) / 2
	return s[:keep] + "..." + s[len(s)-keep:]
}
