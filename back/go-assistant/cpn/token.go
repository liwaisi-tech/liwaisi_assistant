package cpn

import (
	"encoding/json"
	"fmt"
	"time"
)

// Token is the fundamental unit of data in the CPN. Immutable once created.
type Token struct {
	// Color classifies the type of data this token carries.
	Color ColorSet

	// Payload holds the token's data. Consumers must type-assert.
	Payload any

	// OriginID is the ID of the CPN that produced this token.
	OriginID string

	// OriginDepth is the depth of the producing CPN (0=root, 1=domain, 2+=worker, -1=human).
	OriginDepth int

	// OriginKind is the kind of transition that produced this token.
	OriginKind NodeKind

	// Space indicates which communication space this token currently occupies.
	Space SpaceKind

	// SessionID links this token to a user session.
	SessionID string

	// TraceID is propagated across sub-CPN boundaries for distributed tracing.
	TraceID string

	// Timestamp records when this token was created.
	Timestamp time.Time
}

// maxPayloadPreviewLen is the truncation threshold for TokenSnapshot.PayloadPreview.
const maxPayloadPreviewLen = 500

// Snapshot returns a serializable, truncated representation of this token.
// PayloadPreview is capped at 500 characters; longer payloads are truncated
// with a "..." suffix.
func (t *Token) Snapshot() TokenSnapshot {
	if t == nil {
		return TokenSnapshot{}
	}

	preview := formatPayloadPreview(t.Payload)

	return TokenSnapshot{
		Color:          string(t.Color),
		PayloadPreview: preview,
		Space:          string(t.Space),
		OriginID:       t.OriginID,
		OriginKind:     string(t.OriginKind),
	}
}

// formatPayloadPreview converts any payload to a string preview,
// truncated to maxPayloadPreviewLen characters.
func formatPayloadPreview(payload any) string {
	if payload == nil {
		return ""
	}

	var s string
	switch v := payload.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprintf("%v", v)
		} else {
			s = string(b)
		}
	}

	if len(s) > maxPayloadPreviewLen {
		return s[:maxPayloadPreviewLen] + "..."
	}
	return s
}

// IsHumanOrigin returns true if this token was produced by a HITL transition
// or carries human-provided content. Used by Centaurian guard functions.
func (t *Token) IsHumanOrigin() bool {
	if t == nil {
		return false
	}
	return t.Color == ColorHuman || t.OriginKind == NodeKindHITL
}

// ThinkingBlock represents Anthropic extended thinking content.
// The Signature field is cryptographic and must not be modified.
type ThinkingBlock struct {
	// Thinking is the raw thinking text from the model.
	Thinking string

	// Signature is the cryptographic signature from Anthropic.
	// Must be preserved intact when passed back in subsequent calls.
	Signature string
}

// LLMEndpoint specifies which OpenRouter API surface to call.
type LLMEndpoint string

const (
	// EndpointChat targets POST /chat/completions (OpenAI-compatible, default).
	EndpointChat LLMEndpoint = "chat"

	// EndpointMessages targets POST /messages (Anthropic-native).
	// Required for extended thinking, prompt caching, and Anthropic tool formats.
	EndpointMessages LLMEndpoint = "messages"
)

// LLMToolCall represents an LLM-initiated tool invocation.
type LLMToolCall struct {
	// ID is the unique identifier for this tool call.
	ID string

	// ToolName is the name of the tool to invoke.
	ToolName string

	// Arguments holds the tool call arguments as raw JSON.
	// Parsed by the tool's executor, not by the CPN engine.
	Arguments json.RawMessage
}

// LLMToolResult represents the result of a tool invocation.
type LLMToolResult struct {
	// ToolCallID links this result to the originating LLMToolCall.
	ToolCallID string

	// Content is the tool's output as a string.
	Content string
}

// LLMTool describes a tool available to an LLM transition.
type LLMTool struct {
	// Name is the tool's identifier.
	Name string

	// Description explains what the tool does (sent to the LLM).
	Description string

	// Parameters is the JSON schema of the tool's arguments.
	Parameters json.RawMessage
}

// LLMMessage represents a single message in an LLM conversation.
type LLMMessage struct {
	// Role is the message sender: "system", "user", "assistant", or "tool".
	Role string

	// Content is the text content of the message.
	Content string

	// ToolCall is populated when Role == "assistant" and the LLM invoked a tool.
	// Nil when no tool was called.
	ToolCall *LLMToolCall

	// ToolResult is populated when Role == "tool" with the tool's output.
	// Nil for non-tool messages.
	ToolResult *LLMToolResult

	// ThinkingContent carries Anthropic thinking blocks for multi-turn thinking.
	// The Signature field must be preserved intact. Nil when not applicable.
	ThinkingContent *ThinkingBlock
}
