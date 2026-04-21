package cpn

// DisplayLabel is a user-facing description of what a transition is doing,
// emitted alongside transition lifecycle events so the UI can render a
// fast-changing activity indicator (verb + optional detail).
//
// See spec-design-agent-activity-indicator.md §3.1 for the full contract.
type DisplayLabel struct {
	Verb   string `json:"verb"`
	Detail string `json:"detail,omitempty"`
}

// TransitionMeta carries per-transition hints that the resolver uses to
// derive a DisplayLabel detail. Callers populate the fields relevant to
// the transition's NodeKind; unused fields are zero-valued.
type TransitionMeta struct {
	// ToolName is the tool identifier for NodeKindTool transitions.
	ToolName string

	// ToolDetail is an optional argument preview for tool calls
	// (file path, search query, etc). Truncated to 40 chars by the resolver.
	ToolDetail string

	// SubNetRole is the role label of the child CPN for NodeKindSubNet
	// transitions. Empty when the child role is unknown at start time.
	SubNetRole string
}

// detailMaxLen caps display detail length to keep the activity bubble
// on a single line in the chat surface (spec REQ-029, §4.2).
const detailMaxLen = 40

// ResolveDisplayLabel maps (kind, role, meta) → DisplayLabel using the
// Verb Catalog v1 from spec-design-agent-activity-indicator.md §4.2.
//
// Returns nil for silent transitions (currently NodeKindObserver).
// For unknown (kind, role) tuples, returns the fallback verb "Working".
//
// The function is pure: no I/O, no randomness, no allocation beyond the
// returned struct. Safe to call from the hot emit path.
func ResolveDisplayLabel(kind NodeKind, role string, meta TransitionMeta) *DisplayLabel {
	switch kind {
	case NodeKindObserver:
		return nil

	case NodeKindLLM:
		return &DisplayLabel{Verb: llmVerbForRole(role)}

	case NodeKindTool:
		verb, detail := toolVerbAndDetail(meta.ToolName, meta.ToolDetail)
		return &DisplayLabel{Verb: verb, Detail: truncateDetail(detail)}

	case NodeKindValidate:
		return &DisplayLabel{Verb: "Validating"}

	case NodeKindSubNet:
		return &DisplayLabel{Verb: "Delegating", Detail: truncateDetail(meta.SubNetRole)}

	case NodeKindHITL:
		return &DisplayLabel{Verb: "Waiting for you"}

	default:
		return &DisplayLabel{Verb: "Working"}
	}
}

func llmVerbForRole(role string) string {
	switch role {
	case "summarizer":
		return "Summarizing"
	case "classifier", "router":
		return "Deciding"
	default:
		return "Thinking"
	}
}

func toolVerbAndDetail(toolName, toolDetail string) (verb, detail string) {
	switch toolName {
	case "fs_read":
		return "Reading", toolDetail
	case "fs_write":
		return "Writing", toolDetail
	case "web_search":
		return "Searching", toolDetail
	case "":
		return "Calling tool", ""
	default:
		// Unknown tool: surface the tool name itself as the detail so the
		// user can still see what's running.
		return "Calling tool", toolName
	}
}

func truncateDetail(s string) string {
	runes := []rune(s)
	if len(runes) <= detailMaxLen {
		return s
	}
	return string(runes[:detailMaxLen-1]) + "…"
}

// resolveLabelForTransition builds a DisplayLabel for a transition firing
// within a given CPN, or returns nil if the transition is Silent.
//
// Wraps ResolveDisplayLabel with the metadata extraction that only the
// executor has context for (tool hints from Transition, sub-net role from
// a statically-defined child CPN).
func resolveLabelForTransition(c *CPN, t *Transition) *DisplayLabel {
	if t == nil || t.Silent {
		return nil
	}
	meta := TransitionMeta{ToolName: t.ToolName}
	if t.Kind == NodeKindSubNet && t.SubNet != nil {
		meta.SubNetRole = t.SubNet.Role
	}
	role := ""
	if c != nil {
		role = c.Role
	}
	return ResolveDisplayLabel(t.Kind, role, meta)
}
