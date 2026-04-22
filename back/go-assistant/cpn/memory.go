package cpn

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultContextWindowSize is the default number of conversational turns
// to include in the sliding window (T3). 10 turns = 20 messages.
const DefaultContextWindowSize = 10

// ContextWindow holds the assembled context for a single LLM call.
// Built by BuildContext and used to create an LLMRequest.
type ContextWindow struct {
	// SystemPrompt is the T1 tier — static per transition.
	SystemPrompt string

	// Messages is the assembled message list: T2 observers + T3 raw window.
	Messages []*LLMMessage

	// InputTokenEstimate is the rough token count for budget checking.
	InputTokenEstimate int
}

// BuildContext assembles the context window for a NodeKindLLM transition.
// Applies the three-tier memory strategy:
//
//	T1: systemPrompt — static, always included (returned in ContextWindow.SystemPrompt)
//	T2: All RoleObserver messages from history — compressed sub-CPN summaries, always included
//	T3: Last contextWindowSize*2 raw (RoleUser + RoleAssistant) messages — sliding window
//
// Messages are ordered: T2 observers first, then T3 raw messages.
// Does not modify the history slice.
func BuildContext(systemPrompt string, history []*Message, contextWindowSize int) ContextWindow {
	// T2: collect all observer messages.
	var observers []*LLMMessage
	for _, m := range history {
		if m.Role == RoleObserver {
			observers = append(observers, &LLMMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("[Summary from %s]: %s", m.CPNRole, m.Content),
			})
		}
	}

	// T3: sliding window of raw messages.
	raw := filterRaw(history)
	var window []*LLMMessage
	if contextWindowSize > 0 {
		limit := contextWindowSize * 2
		if len(raw) > limit {
			raw = raw[len(raw)-limit:]
		}
		window = make([]*LLMMessage, 0, len(raw))
		for _, m := range raw {
			window = append(window, &LLMMessage{
				Role:    string(m.Role),
				Content: sanitizeForLLM(m.Content),
			})
		}
	}

	// Assemble: T2 then T3.
	messages := make([]*LLMMessage, 0, len(observers)+len(window))
	messages = append(messages, observers...)
	messages = append(messages, window...)

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
func filterRaw(history []*Message) []*Message {
	result := make([]*Message, 0, len(history))
	for _, m := range history {
		if m.Role == RoleUser || m.Role == RoleAssistant {
			result = append(result, m)
		}
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
