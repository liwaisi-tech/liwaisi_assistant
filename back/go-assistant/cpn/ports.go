package cpn

import (
	"context"
	"encoding/json"
)

// ── Port Interfaces ─────────────────────────────────────────────────────────
// Ports define the boundaries between the CPN domain and external systems.
// Infrastructure adapters implement these interfaces (Hexagonal Architecture).

// LLMClient is the interface all LLM transitions call.
// OpenRouterClient is the production implementation (infra/openrouter).
// MockLLMClient is used in tests (defined in test files).
type LLMClient interface {
	// Complete sends a request to the LLM and returns the response.
	Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)

	// CompleteStream sends a streaming request to the LLM, invoking onChunk
	// for each content delta as it arrives, and returns the complete response.
	// When onChunk is nil, deltas are accumulated without callbacks.
	CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(chunk string)) (LLMResponse, error)

	// EstimateCost returns a USD cost estimate for the request
	// based on model pricing and estimated token counts.
	EstimateCost(req *LLMRequest) (float64, error)
}

// ChannelAdapter abstracts delivery of stream chunks and receipt of user
// messages over a specific communication channel.
// Concrete implementations are provided by infrastructure packages.
type ChannelAdapter interface {
	// Send delivers a stream chunk to the external channel (e.g., SSE, webhook).
	Send(ctx context.Context, chunk StreamChunk) error

	// Receive blocks until a user message arrives from the external channel.
	Receive(ctx context.Context) (Message, error)

	// Channel returns the channel type this adapter handles.
	Channel() ChannelType
}

// GroupNotifier notifies the parent group of CPN state changes.
// Implemented by GroupAgent (Block 11). Nil means no group coordination.
type GroupNotifier interface {
	SwitchCMP(cpnID string)
}

// CostProvider abstracts cost lookup for a session.
// Block 7 (TokenLedger) will implement this interface (REQ-007).
type CostProvider interface {
	SessionCostUSD(sessionID string) float64
}

// ── LLM Request / Response Types ────────────────────────────────────────────
// These are domain types used by the execution engine to communicate with
// any LLMClient implementation. They are NOT OpenRouter-specific.

// LLMRequest is the input to LLMClient.Complete().
type LLMRequest struct {
	// Model is the OpenRouter model string or a ModelRegistry key.
	Model string

	// Messages is the conversation history including system prompt.
	Messages []*LLMMessage

	// MaxTokens is the hard cap for the completion.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic, 2.0 = max).
	Temperature float64

	// Tools lists the tools available to the LLM for this request.
	Tools []*LLMTool

	// ResponseFmt is "json_object" when structured JSON output is required.
	ResponseFmt string

	// SessionID is passed to OpenRouter for per-user token tracking.
	SessionID string

	// ── v1.2 Extended Fields ────────────────────────────────────────────────

	// FallbackModels lists alternative models tried in order.
	FallbackModels []string

	// Endpoint selects the API surface: EndpointChat or EndpointMessages.
	Endpoint LLMEndpoint

	// JSONSchema configures structured JSON output with a schema.
	JSONSchema *JSONSchemaConfig

	// ToolChoice controls tool selection ("auto", "none", or specific tool).
	ToolChoice string

	// Provider configures per-request routing.
	Provider *ProviderConfig

	// Trace configures observability trace fields.
	Trace *TraceConfig

	// Plugins lists active OpenRouter plugins.
	Plugins []PluginConfig

	// Reasoning configures reasoning/thinking behavior.
	Reasoning *ReasoningConfig

	// Cache configures prompt caching.
	Cache *CacheControlConfig

	// Stream enables SSE streaming for this request.
	Stream bool
}

// LLMResponse is the output from LLMClient.Complete().
type LLMResponse struct {
	// Content is the text content of the assistant's response.
	Content string

	// ToolCalls contains any tool invocations requested by the LLM.
	ToolCalls []*LLMToolCall

	// InputTokens is the number of prompt tokens consumed.
	InputTokens int

	// OutputTokens is the number of completion tokens generated.
	OutputTokens int

	// Model is the actual model used (may differ if fallback occurred).
	Model string

	// CostUSD is the actual cost reported by OpenRouter.
	CostUSD float64

	// ── v1.2 Extended Fields ────────────────────────────────────────────────

	// ThinkingBlocks contains extended thinking content from Anthropic models.
	ThinkingBlocks []ThinkingBlock

	// CacheReadTokens is the number of tokens read from the cache.
	CacheReadTokens int

	// CacheCreationTokens is the number of tokens used to create the cache.
	CacheCreationTokens int

	// ReasoningTokens is the number of internal reasoning/thinking tokens
	// generated (Anthropic extended thinking, OpenAI o-series). Always 0 for
	// non-reasoning models.
	ReasoningTokens int

	// StopReason indicates why the model stopped generating.
	StopReason string
}

// ── LLM Configuration Types ────────────────────────────────────────────────

// LLMConfig is per-transition model selection and budget constraints.
// v1.2 replaces v1.1 — all 6 original fields remain, 8 new fields added.
// Widening is backward-compatible: existing code using v1.1 fields compiles unchanged.
type LLMConfig struct {
	// Model is the concrete OpenRouter model id for this transition.
	// Populated at session resolve time by
	// internal/app/session_service.go::applyUserModelPreferences based on
	// (in precedence order) the user's per-role ModelOverride, the user's
	// global PreferredModel, or openrouter.ProductDefaultModel.
	//
	// Authored LLMConfig literals in topology factories MUST leave this
	// empty — the authoring layer expresses intent via Role, and the
	// resolver writes Model. See spec-architecture-model-selection-centralization.md
	// (REQ-CFG-003).
	Model string `json:"model,omitempty"`

	// Role is the logical model class this transition needs (e.g.
	// "classifier", "structured"). It is the hook applyUserModelPreferences
	// uses to look up per-role overrides on UserRecord.ModelOverrides. An
	// empty Role means the transition has no role-specific requirement and
	// inherits the user's global PreferredModel (or the product default).
	//
	// Role is authored; Model is derived. See REQ-CFG-004.
	Role string `json:"role,omitempty"`

	// FallbackModels lists alternative models tried in order if Model is unavailable.
	FallbackModels []string

	// Endpoint selects the API surface: EndpointChat or EndpointMessages.
	Endpoint LLMEndpoint

	// MaxTokens is the hard cap for the completion. Required.
	MaxTokens int

	// Temperature controls randomness. Default 0.0 for deterministic.
	Temperature float64

	// StreamOutput enables real-time streaming (not implemented until Block 20).
	StreamOutput bool

	// RequireJSON sets response_format to json_object.
	RequireJSON bool `json:"require_json,omitempty"`

	// ResponseFmtRequired upgrades RequireJSON from "best-effort" to
	// "verify-and-retry". After a successful LLM response, fireLLM re-runs
	// extractJSONObject on the content; if no balanced JSON object is
	// present, a single retry is issued with a terse "JSON only, no
	// thinking, no markdown fences" instruction appended to the system
	// prompt. The retry's cost is attributed to the same transition. Set
	// on t-ask to guard against reasoning-mode models (e.g. Gemma 4 31B)
	// that emit <think>…</think> preambles before the JSON payload. See
	// spec-architecture-model-selection-centralization.md (REQ-PAR-004).
	ResponseFmtRequired bool `json:"response_fmt_required,omitempty"`

	// JSONSchema configures structured JSON output with a schema.
	JSONSchema *JSONSchemaConfig

	// Budget is the per-call cost ceiling in USD. 0 disables budget check.
	Budget float64

	// Provider configures per-request routing: sort, ZDR, max price.
	Provider *ProviderConfig

	// Trace configures observability trace fields.
	Trace *TraceConfig

	// Plugins lists active OpenRouter plugins for this request.
	Plugins []PluginConfig

	// Reasoning configures reasoning/thinking behavior.
	Reasoning *ReasoningConfig

	// CacheControl configures Anthropic prompt caching.
	CacheControl *CacheControlConfig

	// SkipHistory, when true, prevents BuildContext from including
	// conversation history. The LLM only sees its system prompt and the
	// consumed tokens. Used by classifier transitions that must classify
	// each message independently, without bias from prior conversation.
	// Note: SkipHistory has dual semantics — it ALSO suppresses the
	// post-call append of input/output to c.History (see fire_llm.go
	// output guard). For transitions that need to READ history but
	// must NOT pollute it on output, use SkipOutputHistory instead.
	SkipHistory bool

	// SkipOutputHistory, when true, suppresses the post-call append of
	// the consumed-tokens "user" replay AND the LLM "assistant" output
	// to c.History, without affecting the input-side context window.
	// Used by transitions whose output is routing metadata consumed
	// downstream via the CPN token pipeline (not via history) and which
	// would otherwise pollute the conversation transcript on rehydration.
	// See spec-process-bugfix-a2ui-rehydration-completion.md REQ-101..107.
	SkipOutputHistory bool

	// SkipRegionalPreamble, when true, prevents fireLLM from prepending the
	// per-session regional-variant preamble to this transition's SystemPrompt.
	// Default (false) is opt-in: every LLM transition gets the preamble.
	// Set to true on transitions where dialect hints add no value (e.g.
	// structured-JSON planners that never read the user's tone).
	SkipRegionalPreamble bool
}

// ── v1.2 Sub-Types ──────────────────────────────────────────────────────────

// JSONSchemaConfig configures structured JSON output.
type JSONSchemaConfig struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Strict      *bool
}

// ProviderConfig configures per-request OpenRouter provider routing.
type ProviderConfig struct {
	Order             []string
	Only              []string
	Ignore            []string
	AllowFallbacks    *bool
	Sort              string
	MaxPrice          *ProviderMaxPrice
	DataCollection    string
	ZDR               bool
	RequireParameters bool
}

// ProviderMaxPrice caps per-unit pricing for provider selection.
type ProviderMaxPrice struct {
	Prompt     string
	Completion string
	Image      string
	Audio      string
	Request    string
}

// TraceConfig configures observability fields sent to OpenRouter.
type TraceConfig struct {
	TraceID        string
	TraceName      string
	SpanName       string
	GenerationName string
	ParentSpanID   string
}

// PluginConfig is a plugin name string for OpenRouter.
// Valid values: "auto-router", "moderation", "web", "file-parser",
// "response-healing", "context-compression".
type PluginConfig = string

// ReasoningConfig configures reasoning/thinking behavior.
// For /chat/completions: Effort, Summary, MaxTokens, Enabled, Exclude are serialized.
// For /messages: BudgetTokens is serialized as thinking.budget_tokens (Anthropic-native).
type ReasoningConfig struct {
	Effort       string
	Summary      string
	MaxTokens    int
	Enabled      *bool
	Exclude      *bool
	BudgetTokens int
}

// CacheControlConfig configures Anthropic prompt caching.
type CacheControlConfig struct {
	TTL string
}
