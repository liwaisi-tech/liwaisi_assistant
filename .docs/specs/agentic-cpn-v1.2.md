# Unified Agentic CPN — Spec Driven Design
> Version 1.2 — Supersedes v1.1  
> Theoretical foundation: Borghoff, Bottoni & Pareschi (2025) — *Human-Artificial Interaction in the Age of Agentic AI*  
> Engineering foundation: *The 7 Foundational Building Blocks of AI Agents*  
> API reference: OpenRouter OpenAPI 3.1.0

---

## 0. Related Documents

| Document | Description | Status |
|---|---|---|
| **Unified Agentic CPN — Spec Driven Design v1.2** (this document) | Full OpenRouter API integration: dual endpoints, streaming, guardrails, activity billing | current |
| Unified Agentic CPN v1.1 | 7 building blocks integrated. OpenRouter specified at high level only. | archived |
| Unified Agentic CPN v1.0 | First unified spec. No building blocks, no OpenRouter. | archived |
| Agentic OS — Spec Driven Development v0.1 | Superseded by v1.0 | archived |
| CPN Tool Engine — Spec Driven Design v0.1 | Superseded by v1.0 | archived |

---

## 1. What Changed in v1.2

| Area | Change |
|---|---|
| LLMConfig | Added `FallbackModels`, `Endpoint` (chat/messages), `ProviderConfig`, `TraceConfig`, `PluginConfig`, `ReasoningConfig`, `CacheControlConfig`, `JSONSchemaConfig` — all derived from the OpenRouter OpenAPI spec |
| OpenRouterClient | Two build methods: `buildCompletionsBody()` for `/chat/completions`, `buildMessagesBody()` for `/messages` (system message extracted, tools converted to Anthropic `input_schema` format) |
| StreamHandler | New `streaming.go`: SSE parsing for both endpoint formats — `ParseChatSSE()` (OpenAI delta format) and `ParseAnthropicSSE()` (message_start / content_block_delta / message_stop / thinking blocks) |
| Error taxonomy | Retry vs no-retry HTTP status codes mapped from OpenRouter spec into `OpenRouterRetryOn` |
| GuardrailsClient | New `guardrails.go`: account-level spending limits, model allowlists, ZDR enforcement via `/guardrails` API |
| ActivityClient | New `activity.go`: authoritative billing data from `/activity`, `SyncToLedger()` reconciles real-time estimates |
| ModelRegistry | Updated with canonical OpenRouter model strings from OpenAPI examples |
| Package structure | Added `streaming.go`, `guardrails.go`, `activity.go` |
| Implementation order | Extended to Step 22 |

---

## 2. Design Axioms

Unchanged from v1.1. Reproduced here for completeness.

| # | Axiom | Source |
|---|---|---|
| A1 | **Everything is a CPN.** A tool, an agent, a director, and the orchestrator are all instances of the same `CPN` type. Their identity emerges from topology (depth) and role label. | Decision Q2 |
| A2 | **Sub-CPNs are isolated.** A child CPN has its own place namespace. It cannot read or write parent places directly. | Decision Q1 |
| A3 | **Sub-CPNs are observable.** A child CPN emits events to a shared bus. The parent observes via `NodeKindObserver` transitions. | Decision Q1 + Paper §4.2 |
| A4 | **HITL is a first-class node kind.** A `NodeKindHITL` transition blocks its branch until a human-provided token arrives on a channel. | Decision Q3 |
| A5 | **Three communication spaces are structural.** Every Place belongs to Surface, Observation, or Computation space. The executor enforces this. | Paper §4.2 |
| A6 | **Tokens carry origin.** Every token knows which CPN produced it and its depth. | Paper §4.4 |
| A7 | **MAS and Centaurian are runtime modes.** A CPN can switch between `ModeMAS` and `ModeCentaurian` at any executor iteration. | Paper §2.4 |
| A8 | **Parallelism is topological.** Concurrency emerges from the firing rule, never declared explicitly. | Paper §3.1 |
| A9 | **Event Store is append-only.** No event is ever modified. | Spec v0.1 + Paper §4.2 |
| A10 | **The human channel is the only interface.** The user never sees topology, depth, or mode. | Spec v0.1 §2 |
| A11 | **LLM is the last resort.** An LLM call is the most expensive and most dangerous operation in the system. Every transition that can be implemented as `NodeKindTool` must be. `NodeKindLLM` is used only when reasoning with context is genuinely required. | Building Blocks §1 |
| A12 | **Context engineering is the core skill.** When an LLM call is unavoidable, its quality depends entirely on sending the right context at the right time to the right model. | Building Blocks §1 |

---

## 3. Glossary

Unchanged from v1.1. See that document for the full table.

---

## 4. Core Types

Sections 4.1–4.12 (SpaceKind, ColorSet, NodeKind, CPNMode, Token, Place, RetryPolicy, ValidateConfig, HITLConfig, Transition, GroupAgent, CPN) are unchanged from v1.1.

This section specifies only the v1.2 changes.

### 4.1 LLMConfig (replaced in v1.2)

The v1.1 `LLMConfig` is replaced in its entirety. Every field from the OpenRouter OpenAPI `ChatGenerationParams` and `AnthropicMessagesRequest` schemas that affects a single LLM call is represented here.

```go
// LLMEndpoint — which OpenRouter endpoint to call.
type LLMEndpoint string

const (
    EndpointChat     LLMEndpoint = "chat"     // POST /chat/completions (default, OpenAI-compatible)
    EndpointMessages LLMEndpoint = "messages" // POST /messages (Anthropic-native)
)

// LLMConfig — per-transition model and request configuration.
// All fields map 1:1 to OpenRouter request body fields.
type LLMConfig struct {
    // Model — primary OpenRouter model string.
    // Examples: "anthropic/claude-sonnet-4-6", "google/gemini-2.0-flash-001"
    // If empty, falls back to OpenRouterClient.DefaultModel.
    Model string

    // FallbackModels — tried in order if the primary model is unavailable.
    // Maps to the "models" array in both endpoint request bodies.
    FallbackModels []string

    // Endpoint — which OpenRouter API surface to use. Default: EndpointChat.
    // Use EndpointMessages for Anthropic extended thinking (budget_tokens),
    // prompt caching (cache_control on messages), and Anthropic tool formats.
    Endpoint LLMEndpoint

    // MaxTokens — hard cap for the completion. Required.
    MaxTokens int

    // Temperature — 0.0 to 2.0. Default 0.0 for deterministic outputs.
    Temperature float64

    // StreamOutput — if true, chunks are sent to Session.Stream as they arrive.
    StreamOutput bool

    // RequireJSON — sets response_format: {type: "json_object"}.
    // Always follow with NodeKindValidate.
    RequireJSON bool

    // JSONSchema — sets response_format: {type: "json_schema", json_schema: ...}.
    // Stricter than RequireJSON; model must conform to the provided schema.
    JSONSchema *JSONSchemaConfig

    // Budget — optional per-call cost ceiling in USD.
    // Estimated before the call; if exceeded, emits ErrBudgetExceeded.
    Budget float64

    // Provider — optional provider routing preferences.
    Provider *ProviderConfig

    // Trace — optional observability metadata propagated to OpenRouter tracing.
    Trace *TraceConfig

    // Plugins — optional OpenRouter plugins for this request.
    // IDs: "auto-router", "moderation", "web", "file-parser", "response-healing"
    Plugins []PluginConfig

    // Reasoning — extended thinking / reasoning configuration.
    // For Anthropic: set BudgetTokens and use EndpointMessages.
    // For OpenAI o-series / Gemini: set Effort and use EndpointChat.
    Reasoning *ReasoningConfig

    // CacheControl — enables prompt caching on the last cacheable block.
    // Currently supported for Anthropic Claude via EndpointMessages.
    // TTL: "5m" (default) or "1h".
    CacheControl *CacheControlConfig
}

// JSONSchemaConfig — structured output schema.
type JSONSchemaConfig struct {
    Name        string          // max 64 chars, a-z A-Z 0-9 underscores dashes
    Description string
    Schema      json.RawMessage // the JSON Schema object
    Strict      *bool           // enable strict schema adherence; nil = default
}

// ProviderConfig — provider routing preferences for a single request.
// Maps to the "provider" object in both endpoint request bodies.
type ProviderConfig struct {
    Order          []string          // ordered provider slugs to try first
    Only           []string          // whitelist merged with account-wide settings
    Ignore         []string          // blacklist merged with account-wide settings
    AllowFallbacks *bool             // default true; false = only primary provider
    Sort           string            // "price" | "throughput" | "latency"
    MaxPrice       *ProviderMaxPrice // per-token price ceiling (USD per million)
    DataCollection string            // "allow" (default) | "deny"
    ZDR            bool              // route only to Zero Data Retention endpoints
    RequireParameters bool           // filter to providers supporting all params
}

// ProviderMaxPrice — price ceiling per token category, USD per million tokens.
type ProviderMaxPrice struct {
    Prompt     string // e.g. "1.5"
    Completion string
    Image      string
    Audio      string
    Request    string // per-request fee
}

// TraceConfig — observability metadata sent to OpenRouter.
// Maps to the "trace" object in both endpoint request bodies.
type TraceConfig struct {
    TraceID        string
    TraceName      string
    SpanName       string
    GenerationName string
    ParentSpanID   string
}

// PluginConfig — an OpenRouter plugin to enable for this request.
type PluginConfig struct {
    ID      string
    Enabled *bool          // nil = default (true)
    Options map[string]any // plugin-specific options (e.g. max_results for "web")
}

// ReasoningConfig — extended thinking / reasoning configuration.
// For /chat/completions: Effort + Summary + MaxTokens + Enabled.
// For /messages (Anthropic): BudgetTokens (sets "thinking": {"type":"enabled","budget_tokens":N}).
type ReasoningConfig struct {
    Effort       string // "xhigh"|"high"|"medium"|"low"|"minimal"|"none"
    Summary      string // "auto"|"concise"|"detailed"
    MaxTokens    int
    Enabled      *bool
    BudgetTokens int    // Anthropic /messages only
}

// CacheControlConfig — prompt caching.
// Maps to "cache_control" at the top level of ChatGenerationParams,
// and to per-block cache_control in AnthropicMessagesRequest.
type CacheControlConfig struct {
    TTL string // "5m" | "1h"
}
```

### 4.2 Token (extended in v1.2)

`ThinkingBlock` is added to support Anthropic extended thinking across turns.

```go
type ThinkingBlock struct {
    Thinking  string // raw thinking text from the model
    Signature string // cryptographic signature from Anthropic — must not be modified
}

// LLMMessage — extended in v1.2 to carry ThinkingContent.
type LLMMessage struct {
    Role           string
    Content        string
    ToolCall       *LLMToolCall
    ToolResult     *LLMToolResult
    // ThinkingContent — non-nil for Anthropic thinking blocks passed back in context.
    // The Signature field must be preserved intact.
    ThinkingContent *ThinkingBlock
}
```

---

## 5. Communication Spaces — Structural Rules

Unchanged from v1.1.

---

## 6. OpenRouter Integration (extended in v1.2)

### 6.1 Why OpenRouter

OpenRouter is the single LLM proxy. All `NodeKindLLM` transitions route through it. Benefits:

- One API key covers all providers (Anthropic, Google, Meta, Mistral, OpenAI, etc.)
- Unified billing and per-user token tracking via `session_id`
- Model fallback: `FallbackModels` tried in order if primary is unavailable
- Provider routing: sort by price/throughput/latency, restrict to ZDR endpoints, enforce spending caps
- Two API surfaces: `/chat/completions` (OpenAI-compatible) and `/messages` (Anthropic-native)

Base URL: `https://openrouter.ai/api/v1`  
Auth: `Authorization: Bearer <api_key>`  
Session tracking: `X-Session-Id` header OR `session_id` in body (body takes precedence, max 128 chars)

### 6.2 Dual Endpoint Architecture

| Endpoint | URL | Use case |
|---|---|---|
| `EndpointChat` (default) | `POST /chat/completions` | Universal. Works with all providers. OpenAI-compatible format. |
| `EndpointMessages` | `POST /messages` | Anthropic-native. Required for: extended thinking (`budget_tokens`), prompt caching (`cache_control` on messages), Anthropic tool formats, thinking blocks in context. |

The executor calls `buildCompletionsBody()` or `buildMessagesBody()` depending on `LLMConfig.Endpoint`. This is transparent to CPN topology.

### 6.3 LLMClient Interface

```go
type LLMClient interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
    EstimateCost(req LLMRequest) (float64, error)
}
```

### 6.4 LLMRequest and LLMResponse

```go
type LLMRequest struct {
    Model          string
    FallbackModels []string
    Endpoint       LLMEndpoint
    Messages       []LLMMessage
    MaxTokens      int
    Temperature    float64
    ResponseFmt    string           // "json_object" when RequireJSON
    JSONSchema     *JSONSchemaConfig
    Tools          []LLMTool
    ToolChoice     string            // "auto" | "none" | "required"
    Provider       *ProviderConfig
    Trace          *TraceConfig
    Plugins        []PluginConfig
    Reasoning      *ReasoningConfig
    Cache          *CacheControlConfig
    SessionID      string
    TraceID        string
}

type LLMResponse struct {
    Content             string
    ToolCalls           []LLMToolCall
    ThinkingBlocks      []ThinkingBlock // non-empty when EndpointMessages + reasoning
    InputTokens         int
    OutputTokens        int
    CacheReadTokens     int     // Anthropic cache_read_input_tokens
    CacheCreationTokens int     // Anthropic cache_creation_input_tokens
    Model               string  // actual model used (may differ if fallback occurred)
    CostUSD             float64 // actual cost from OpenRouter usage field
    StopReason          string  // "end_turn"|"max_tokens"|"tool_use"|"stop"|etc.
}

type LLMTool struct {
    Name        string
    Description string
    Parameters  json.RawMessage // JSON schema
}
```

### 6.5 OpenRouterClient

```go
type OpenRouterClient struct {
    APIKey        string
    DefaultModel  string
    BaseURL       string          // default: "https://openrouter.ai/api/v1"
    HTTPClient    *http.Client
    TokenLedger   *TokenLedger
    StreamHandler *StreamHandler
}

func NewOpenRouterClient(apiKey, defaultModel string) *OpenRouterClient {
    return &OpenRouterClient{
        APIKey:        apiKey,
        DefaultModel:  defaultModel,
        BaseURL:       "https://openrouter.ai/api/v1",
        HTTPClient:    &http.Client{Timeout: 120 * time.Second},
        TokenLedger:   NewTokenLedger(),
        StreamHandler: NewStreamHandler(),
    }
}

func (c *OpenRouterClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
    if req.Model == "" {
        req.Model = c.DefaultModel
    }

    var body []byte
    var endpoint string
    switch req.Endpoint {
    case EndpointMessages:
        body = c.buildMessagesBody(req)
        endpoint = c.BaseURL + "/messages"
    default:
        body = c.buildCompletionsBody(req)
        endpoint = c.BaseURL + "/chat/completions"
    }

    httpReq, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
    httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("X-Session-Id", req.SessionID)

    resp, err := c.HTTPClient.Do(httpReq)
    if err != nil {
        return LLMResponse{}, err
    }
    defer resp.Body.Close()

    switch resp.StatusCode {
    case 400: return LLMResponse{}, ErrBadRequest
    case 401: return LLMResponse{}, ErrUnauthorized
    case 402: return LLMResponse{}, ErrInsufficientCredits
    case 403: return LLMResponse{}, ErrForbidden
    case 404: return LLMResponse{}, ErrNotFound
    case 408: return LLMResponse{}, ErrRequestTimeout
    case 413: return LLMResponse{}, ErrPayloadTooLarge
    case 422: return LLMResponse{}, ErrUnprocessableEntity
    case 429: return LLMResponse{}, ErrRateLimited
    case 524: return LLMResponse{}, ErrEdgeTimeout
    case 529: return LLMResponse{}, ErrProviderOverloaded
    }
    if resp.StatusCode >= 500 {
        return LLMResponse{}, ErrProviderUnavailable
    }

    var out LLMResponse
    var parseErr error
    if req.Endpoint == EndpointMessages {
        out, parseErr = c.StreamHandler.ParseAnthropicSSE(resp.Body)
    } else {
        out, parseErr = c.StreamHandler.ParseChatSSE(resp.Body)
    }
    if parseErr != nil {
        return LLMResponse{}, parseErr
    }
    c.TokenLedger.RecordWithCache(
        req.SessionID, out.InputTokens, out.OutputTokens,
        out.CacheReadTokens, out.CacheCreationTokens, out.CostUSD,
    )
    return out, nil
}
```

### 6.6 buildCompletionsBody

Builds `ChatGenerationParams` for `POST /chat/completions`.

```go
func (c *OpenRouterClient) buildCompletionsBody(req LLMRequest) []byte {
    body := map[string]any{
        "model":      req.Model,
        "messages":   formatChatMessages(req.Messages),
        "max_tokens": req.MaxTokens,
        "session_id": req.SessionID,
    }
    if len(req.FallbackModels) > 0 {
        body["models"] = append([]string{req.Model}, req.FallbackModels...)
    }
    if req.Temperature != 0 {
        body["temperature"] = req.Temperature
    }
    if req.ResponseFmt == "json_object" {
        body["response_format"] = map[string]string{"type": "json_object"}
    }
    if req.JSONSchema != nil {
        body["response_format"] = map[string]any{
            "type": "json_schema",
            "json_schema": map[string]any{
                "name": req.JSONSchema.Name, "description": req.JSONSchema.Description,
                "schema": req.JSONSchema.Schema, "strict": req.JSONSchema.Strict,
            },
        }
    }
    if len(req.Tools) > 0 {
        body["tools"] = formatChatTools(req.Tools)
        if req.ToolChoice != "" {
            body["tool_choice"] = req.ToolChoice
        }
    }
    if req.Provider != nil { body["provider"] = formatProviderConfig(req.Provider) }
    if req.Trace    != nil { body["trace"]    = formatTrace(req.Trace) }
    if len(req.Plugins) > 0 { body["plugins"] = formatPlugins(req.Plugins) }
    if req.Reasoning != nil {
        body["reasoning"] = map[string]any{
            "effort":     req.Reasoning.Effort,
            "summary":    req.Reasoning.Summary,
            "max_tokens": req.Reasoning.MaxTokens,
            "enabled":    req.Reasoning.Enabled,
        }
    }
    if req.Cache != nil {
        body["cache_control"] = map[string]string{"type": "ephemeral", "ttl": req.Cache.TTL}
    }
    out, _ := json.Marshal(body)
    return out
}

// formatChatMessages — OpenAI messages array.
// System messages kept in-place; tool results use role "tool" with tool_call_id.
func formatChatMessages(msgs []LLMMessage) []map[string]any {
    var out []map[string]any
    for _, m := range msgs {
        msg := map[string]any{"role": m.Role, "content": m.Content}
        if m.ToolCall != nil {
            msg["tool_calls"] = []map[string]any{{
                "id": m.ToolCall.ID, "type": "function",
                "function": map[string]any{
                    "name": m.ToolCall.ToolName,
                    "arguments": string(m.ToolCall.Arguments),
                },
            }}
        }
        if m.ToolResult != nil {
            msg["role"] = "tool"
            msg["tool_call_id"] = m.ToolResult.ToolCallID
        }
        out = append(out, msg)
    }
    return out
}
```

### 6.7 buildMessagesBody

Builds `AnthropicMessagesRequest` for `POST /messages`.

Structural differences from OpenAI format:
- `system` is a top-level field, not a role="system" message
- Tools use `input_schema` instead of `parameters`
- Extended thinking uses `"thinking": {"type": "enabled", "budget_tokens": N}`
- Thinking blocks from previous turns must carry their `signature` intact

```go
func (c *OpenRouterClient) buildMessagesBody(req LLMRequest) []byte {
    body := map[string]any{
        "model":      req.Model,
        "max_tokens": req.MaxTokens,
        "session_id": req.SessionID,
    }
    if len(req.FallbackModels) > 0 {
        body["models"] = append([]string{req.Model}, req.FallbackModels...)
    }

    // Extract system message — Anthropic expects it at top level, not in messages.
    var systemPrompt string
    var userMsgs []LLMMessage
    for _, m := range req.Messages {
        if m.Role == "system" {
            systemPrompt = m.Content
        } else {
            userMsgs = append(userMsgs, m)
        }
    }
    if systemPrompt != "" {
        if req.Cache != nil {
            body["system"] = []map[string]any{{
                "type": "text", "text": systemPrompt,
                "cache_control": map[string]string{"type": "ephemeral", "ttl": req.Cache.TTL},
            }}
        } else {
            body["system"] = systemPrompt
        }
    }

    body["messages"] = formatAnthropicMessages(userMsgs, req.Cache)

    if req.Temperature != 0 { body["temperature"] = req.Temperature }

    // Anthropic tools use input_schema, not parameters
    if len(req.Tools) > 0 { body["tools"] = formatAnthropicTools(req.Tools) }

    // Extended thinking — maps to "thinking" object, not "reasoning"
    if req.Reasoning != nil && req.Reasoning.BudgetTokens > 0 {
        body["thinking"] = map[string]any{
            "type":          "enabled",
            "budget_tokens": req.Reasoning.BudgetTokens,
        }
    }

    if req.Provider != nil { body["provider"] = formatProviderConfig(req.Provider) }
    if req.Trace    != nil { body["trace"]    = formatTrace(req.Trace) }
    if len(req.Plugins) > 0 { body["plugins"] = formatPlugins(req.Plugins) }

    out, _ := json.Marshal(body)
    return out
}

// formatAnthropicMessages — converts LLMMessage slice to Anthropic content blocks.
// ThinkingBlocks are serialized as {"type":"thinking","thinking":"...","signature":"..."}.
func formatAnthropicMessages(msgs []LLMMessage, cache *CacheControlConfig) []map[string]any {
    var out []map[string]any
    for _, m := range msgs {
        if m.ThinkingContent != nil {
            out = append(out, map[string]any{
                "role": "assistant",
                "content": []map[string]any{{
                    "type":      "thinking",
                    "thinking":  m.ThinkingContent.Thinking,
                    "signature": m.ThinkingContent.Signature,
                }},
            })
            continue
        }
        msg := map[string]any{"role": m.Role}
        if cache != nil && m.Role == "user" {
            msg["content"] = []map[string]any{{
                "type": "text", "text": m.Content,
                "cache_control": map[string]string{"type": "ephemeral", "ttl": cache.TTL},
            }}
        } else {
            msg["content"] = m.Content
        }
        if m.ToolCall != nil {
            msg["content"] = []map[string]any{{
                "type": "tool_use", "id": m.ToolCall.ID,
                "name": m.ToolCall.ToolName, "input": m.ToolCall.Arguments,
            }}
        }
        if m.ToolResult != nil {
            msg["content"] = []map[string]any{{
                "type": "tool_result", "tool_use_id": m.ToolResult.ToolCallID,
                "content": m.ToolResult.Content,
            }}
        }
        out = append(out, msg)
    }
    return out
}

// formatAnthropicTools — Anthropic uses "input_schema" where OpenAI uses "parameters".
func formatAnthropicTools(tools []LLMTool) []map[string]any {
    var out []map[string]any
    for _, t := range tools {
        out = append(out, map[string]any{
            "name": t.Name, "description": t.Description, "input_schema": t.Parameters,
        })
    }
    return out
}
```

### 6.8 StreamHandler

Parses SSE from both endpoint formats into a single `LLMResponse`.

```go
type StreamHandler struct{}

func NewStreamHandler() *StreamHandler { return &StreamHandler{} }

// ParseChatSSE — parses /chat/completions SSE stream.
// Format: data: {"choices":[{"delta":{"content":"..."}}]}
// Terminates on: data: [DONE]
// Collects: content (assembled from deltas), tool_calls, usage from final chunk.
func (h *StreamHandler) ParseChatSSE(body io.ReadCloser) (LLMResponse, error) {
    var (
        content      strings.Builder
        toolCalls    []LLMToolCall
        inputTokens  int
        outputTokens int
        costUSD      float64
        model        string
        stopReason   string
    )
    scanner := bufio.NewScanner(body)
    for scanner.Scan() {
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") { continue }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" { break }

        var chunk struct {
            Model   string `json:"model"`
            Choices []struct {
                Delta struct {
                    Content   string `json:"content"`
                    ToolCalls []struct {
                        ID       string `json:"id"`
                        Function struct {
                            Name      string `json:"name"`
                            Arguments string `json:"arguments"`
                        } `json:"function"`
                    } `json:"tool_calls"`
                } `json:"delta"`
                FinishReason string `json:"finish_reason"`
            } `json:"choices"`
            Usage *struct {
                PromptTokens     int     `json:"prompt_tokens"`
                CompletionTokens int     `json:"completion_tokens"`
                Cost             float64 `json:"cost"` // OpenRouter extension
            } `json:"usage"`
        }
        if err := json.Unmarshal([]byte(data), &chunk); err != nil { continue }
        if chunk.Model != "" { model = chunk.Model }
        for _, choice := range chunk.Choices {
            content.WriteString(choice.Delta.Content)
            if choice.FinishReason != "" { stopReason = choice.FinishReason }
            for _, tc := range choice.Delta.ToolCalls {
                toolCalls = append(toolCalls, LLMToolCall{
                    ID: tc.ID, ToolName: tc.Function.Name,
                    Arguments: json.RawMessage(tc.Function.Arguments),
                })
            }
        }
        if chunk.Usage != nil {
            inputTokens  = chunk.Usage.PromptTokens
            outputTokens = chunk.Usage.CompletionTokens
            costUSD      = chunk.Usage.Cost
        }
    }
    return LLMResponse{
        Content: content.String(), ToolCalls: toolCalls,
        InputTokens: inputTokens, OutputTokens: outputTokens,
        CostUSD: costUSD, Model: model, StopReason: stopReason,
    }, scanner.Err()
}

// ParseAnthropicSSE — parses /messages SSE stream.
// Event types consumed:
//   message_start        — model, input_tokens
//   content_block_start  — block type (text | thinking)
//   content_block_delta  — text_delta, thinking_delta, signature_delta
//   message_delta        — stop_reason, output_tokens
//   message_stop         — terminates loop
//   error                — returns ErrProviderUnavailable
func (h *StreamHandler) ParseAnthropicSSE(body io.ReadCloser) (LLMResponse, error) {
    var (
        content          strings.Builder
        thinkingBuffer   strings.Builder
        thinkingBlocks   []ThinkingBlock
        inputTokens      int
        outputTokens     int
        cacheRead        int
        cacheCreate      int
        costUSD          float64
        model            string
        stopReason       string
        currentEvent     string
        blockTypes       = map[int]string{}
    )
    scanner := bufio.NewScanner(body)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "event: ") {
            currentEvent = strings.TrimPrefix(line, "event: ")
            continue
        }
        if !strings.HasPrefix(line, "data: ") { continue }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" { break }

        switch currentEvent {
        case "message_start":
            var ev struct {
                Message struct {
                    Model string `json:"model"`
                    Usage struct {
                        InputTokens              int `json:"input_tokens"`
                        CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
                        CacheReadInputTokens     int `json:"cache_read_input_tokens"`
                    } `json:"usage"`
                } `json:"message"`
            }
            if json.Unmarshal([]byte(data), &ev) == nil {
                model       = ev.Message.Model
                inputTokens = ev.Message.Usage.InputTokens
                cacheCreate = ev.Message.Usage.CacheCreationInputTokens
                cacheRead   = ev.Message.Usage.CacheReadInputTokens
            }

        case "content_block_start":
            var ev struct {
                Index        int    `json:"index"`
                ContentBlock struct { Type string `json:"type"` } `json:"content_block"`
            }
            if json.Unmarshal([]byte(data), &ev) == nil {
                blockTypes[ev.Index] = ev.ContentBlock.Type
                if ev.ContentBlock.Type == "thinking" {
                    thinkingBuffer.Reset()
                }
            }

        case "content_block_delta":
            var ev struct {
                Index int `json:"index"`
                Delta struct {
                    Type      string `json:"type"`
                    Text      string `json:"text"`      // text_delta
                    Thinking  string `json:"thinking"`  // thinking_delta
                    Signature string `json:"signature"` // signature_delta
                } `json:"delta"`
            }
            if json.Unmarshal([]byte(data), &ev) == nil {
                switch ev.Delta.Type {
                case "text_delta":
                    content.WriteString(ev.Delta.Text)
                case "thinking_delta":
                    thinkingBuffer.WriteString(ev.Delta.Thinking)
                case "signature_delta":
                    if blockTypes[ev.Index] == "thinking" {
                        thinkingBlocks = append(thinkingBlocks, ThinkingBlock{
                            Thinking:  thinkingBuffer.String(),
                            Signature: ev.Delta.Signature,
                        })
                    }
                }
            }

        case "message_delta":
            var ev struct {
                Delta struct{ StopReason string `json:"stop_reason"` } `json:"delta"`
                Usage struct{ OutputTokens int `json:"output_tokens"` } `json:"usage"`
            }
            if json.Unmarshal([]byte(data), &ev) == nil {
                stopReason   = ev.Delta.StopReason
                outputTokens = ev.Usage.OutputTokens
            }

        case "message_stop":
            goto done

        case "error":
            var ev struct {
                Error struct{ Type, Message string } `json:"error"`
            }
            if json.Unmarshal([]byte(data), &ev) == nil {
                return LLMResponse{}, fmt.Errorf("%w: %s", ErrProviderUnavailable, ev.Error.Message)
            }
        }
    }
done:
    return LLMResponse{
        Content: content.String(), ThinkingBlocks: thinkingBlocks,
        InputTokens: inputTokens, OutputTokens: outputTokens,
        CacheReadTokens: cacheRead, CacheCreationTokens: cacheCreate,
        CostUSD: costUSD, Model: model, StopReason: stopReason,
    }, scanner.Err()
}
```

### 6.9 Error Taxonomy and RetryPolicy

HTTP status codes from the OpenRouter spec map to retry/no-retry decisions.

```
Retry (transient):     408, 429, 500, 502, 503, 524, 529
Do not retry (permanent): 400, 401, 402, 403, 404, 413, 422
```

```go
// OpenRouterRetryOn — canonical RetryOn function for all NodeKindLLM transitions.
func OpenRouterRetryOn(err error, _ int) bool {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return false
    }
    // Permanent — do not retry
    for _, perm := range []error{
        ErrBadRequest, ErrUnauthorized, ErrInsufficientCredits, ErrForbidden,
        ErrNotFound, ErrPayloadTooLarge, ErrUnprocessableEntity,
    } {
        if errors.Is(err, perm) { return false }
    }
    return true // transient — retry
}

// OpenRouterRetryPolicy — default retry policy for NodeKindLLM transitions.
func OpenRouterRetryPolicy() *RetryPolicy {
    return &RetryPolicy{
        MaxAttempts: 3,
        InitialWait: 500 * time.Millisecond,
        MaxWait:     30 * time.Second,
        Multiplier:  2.0,
        RetryOn:     OpenRouterRetryOn,
        CircuitBreaker: &CircuitBreakerConfig{
            FailureThreshold: 5,
            OpenDuration:     60 * time.Second,
        },
    }
}
```

### 6.10 Model Registry

```go
// ModelRegistry — suggested model assignments by task type.
// Using the cheapest model that can do the job operationalizes Axiom A11.
var ModelRegistry = map[string]string{
    "classifier":   "google/gemini-2.0-flash-001",
    "structured":   "anthropic/claude-haiku-4-5-20251001",
    "reasoning":    "anthropic/claude-sonnet-4-6",
    "long-context": "google/gemini-2.0-pro-001",
    "summarize":    "meta-llama/llama-3.3-8b-instruct",
    "thinking":     "anthropic/claude-opus-4-6",
}
```

### 6.11 TokenLedger (extended in v1.2)

Cache token fields added for Anthropic prompt caching cost accounting.

```go
type SessionTokenRecord struct {
    SessionID           string
    InputTokens         int
    OutputTokens        int
    CacheReadTokens     int     // Anthropic cache_read_input_tokens
    CacheCreationTokens int     // Anthropic cache_creation_input_tokens
    TotalCostUSD        float64
    Calls               int
    LastUpdated         time.Time
}

func (l *TokenLedger) RecordWithCache(
    sessionID string, in, out, cacheRead, cacheCreate int, costUSD float64,
) { /* thread-safe update */ }
```

---

## 7. Memory — Context Window Management

Unchanged from v1.1. One addition: Anthropic thinking blocks from `LLMResponse.ThinkingBlocks` are stored as `RoleObserver` messages in CPN history (Tier 2 compressed memory). The `Signature` field must be preserved exactly when passed back in subsequent calls via `LLMMessage.ThinkingContent`.

---

## 8. The 7 Building Blocks Mapped to CPN Primitives

Unchanged from v1.1.

---

## 9. GuardrailsClient (new in v1.2)

Guardrails enforce account-level spending limits, model allowlists, and ZDR policies via `/guardrails`. This is the account-level enforcement layer complementing the per-transition `Budget` field: `Budget` prevents runaway single calls; Guardrails prevent runaway across all transitions in a 24-hour window.

When the guardrail cap is hit, OpenRouter returns 402. The executor maps this to `ErrInsufficientCredits`, which routes to `ErrorPlace` via the transition's fallback branch.

```go
// GuardrailsClient — manages account-level spending guardrails.
// Requires a management API key (not a regular API key).
type GuardrailsClient struct {
    APIKey     string
    BaseURL    string
    HTTPClient *http.Client
}

type Guardrail struct {
    ID               string
    Name             string
    Description      string
    LimitUSD         float64   // spending limit; reset by ResetInterval
    ResetInterval    string    // "daily" | "weekly" | "monthly" | ""
    AllowedProviders []string  // provider slugs; nil = all allowed
    AllowedModels    []string  // canonical model slugs; nil = all allowed
    EnforceZDR       bool      // restrict to ZDR endpoints only
    CreatedAt        time.Time
    UpdatedAt        *time.Time
}

func (c *GuardrailsClient) Create(ctx context.Context, g Guardrail) (Guardrail, error)
func (c *GuardrailsClient) Update(ctx context.Context, id string, g Guardrail) (Guardrail, error)
func (c *GuardrailsClient) Delete(ctx context.Context, id string) error
func (c *GuardrailsClient) List(ctx context.Context) ([]Guardrail, error)
func (c *GuardrailsClient) AssignKeys(ctx context.Context, guardrailID string, keyHashes []string) (int, error)
func (c *GuardrailsClient) UnassignKeys(ctx context.Context, guardrailID string, keyHashes []string) (int, error)

// ApplicationInit — called once at application startup.
// Creates a daily spending guardrail and assigns the app API key to it.
func ApplicationInit(ctx context.Context, client *GuardrailsClient, dailyLimitUSD float64, apiKeyHash string) error {
    g, err := client.Create(ctx, Guardrail{
        Name:          "app-daily-limit",
        LimitUSD:      dailyLimitUSD,
        ResetInterval: "daily",
    })
    if err != nil { return err }
    _, err = client.AssignKeys(ctx, g.ID, []string{apiKeyHash})
    return err
}
```

---

## 10. ActivityClient (new in v1.2)

`TokenLedger` tracks real-time cost estimates. `/activity` provides authoritative billing data. `SyncToLedger()` reconciles the two, correcting estimation drift and enabling accurate Flow Intelligence rankings.

```go
// ActivityClient — fetches authoritative usage from /activity.
// Requires a management API key.
type ActivityClient struct {
    APIKey     string
    BaseURL    string
    HTTPClient *http.Client
}

// ActivityRecord — one row from /activity, grouped by (date, model, endpoint).
type ActivityRecord struct {
    Date             string  // YYYY-MM-DD UTC
    Model            string  // e.g. "anthropic/claude-sonnet-4-6"
    ModelPermaslug   string  // e.g. "anthropic/claude-sonnet-4-6-20250514"
    EndpointID       string  // UUID of the provider endpoint
    ProviderName     string
    UsageUSD         float64 // OpenRouter credits spent
    BYOKUsageUSD     float64 // BYOK inference cost
    Requests         int
    PromptTokens     int
    CompletionTokens int
    ReasoningTokens  int
}

// Fetch — activity for the last 30 completed UTC days.
// date: optional YYYY-MM-DD filter; empty string = all 30 days.
func (c *ActivityClient) Fetch(ctx context.Context, date string) ([]ActivityRecord, error)

// SyncToLedger — reconciles TokenLedger estimates with authoritative /activity data.
// Updates DailyTotal in the ledger; used by Flow Intelligence for cost-aware ranking.
// Called on a periodic schedule (e.g. hourly).
func (c *ActivityClient) SyncToLedger(ctx context.Context, ledger *TokenLedger, date string) error {
    records, err := c.Fetch(ctx, date)
    if err != nil { return err }
    var totalUSD float64
    for _, r := range records { totalUSD += r.UsageUSD }
    ledger.SetDailyTotal(date, totalUSD)
    return nil
}
```

---

## 11. Executor

Unchanged from v1.1 in structure. The default `Retry` for `NodeKindLLM` transitions without an explicit `Retry` field is now `OpenRouterRetryPolicy()`.

---

## 12–16. SubNet Protocol / HITL Protocol / Mode Switching / Session

Unchanged from v1.1.

---

## 17. Error Types

```go
var (
    // v1.1 errors (unchanged)
    ErrColorMismatch       = errors.New("token color does not match place color set")
    ErrEmptyPlace          = errors.New("no tokens available in place")
    ErrInvalidArc          = errors.New("transition references non-existent place")
    ErrDeadlock            = errors.New("no transitions can fire but CPN is not complete")
    ErrTimeout             = errors.New("CPN execution exceeded timeout")
    ErrSpaceMismatch       = errors.New("token space does not match place space kind")
    ErrSpaceViolation      = errors.New("token cannot bypass observation space")
    ErrSubNetFailed        = errors.New("sub-CPN reached failed state")
    ErrNoHITLWaiting       = errors.New("no HITL transition is currently waiting")
    ErrInvalidNodeKind     = errors.New("transition kind is not recognized")
    ErrCentaurianGuard     = errors.New("centaurian guard failed: missing human or AI token")
    ErrValidationFailed    = errors.New("token failed schema validation after max corrections")
    ErrCircuitOpen         = errors.New("circuit breaker is open: transition blocked")
    ErrRateLimited         = errors.New("OpenRouter 429: rate limited")
    ErrProviderUnavailable = errors.New("OpenRouter 5xx: provider unavailable")
    ErrBudgetExceeded      = errors.New("estimated LLM cost exceeds transition budget")
    ErrHITLRejected        = errors.New("human rejected the HITL gate")
    ErrHITLMaxRevisions    = errors.New("HITL revision loop exceeded max rounds")

    // v1.2 errors (new) — mapped from OpenRouter HTTP status codes
    ErrBadRequest          = errors.New("OpenRouter 400: invalid request parameters")
    ErrUnauthorized        = errors.New("OpenRouter 401: authentication required or invalid")
    ErrInsufficientCredits = errors.New("OpenRouter 402: insufficient credits")
    ErrForbidden           = errors.New("OpenRouter 403: insufficient permissions")
    ErrNotFound            = errors.New("OpenRouter 404: resource does not exist")
    ErrRequestTimeout      = errors.New("OpenRouter 408: request timeout")
    ErrPayloadTooLarge     = errors.New("OpenRouter 413: payload exceeds size limit")
    ErrUnprocessableEntity = errors.New("OpenRouter 422: semantic validation failure")
    ErrEdgeTimeout         = errors.New("OpenRouter 524: edge network timeout")
    ErrProviderOverloaded  = errors.New("OpenRouter 529: provider temporarily overloaded")
)
```

---

## 18. Reference Networks

### 18.1 Financial Analysis with Extended Thinking

```
P:INPUT (STRING, Surface)
    ↓
[llm:planner — classifier model, JSON, EndpointChat]
    ↓
[validate:plan-schema]
    ├─ valid → P:TICKER + P:QUERY
    └─ invalid → [llm:correct-plan] → retry
    ↓ (parallel)
[tool:get_fin, retry=OpenRouterRetryPolicy]   [tool:web_search, retry=OpenRouterRetryPolicy]
    ↓                                                  ↓
P:FIN_DATA                                    P:WEB_SENT + P:WEB_SUMM
    └────────────────────┬─────────────────────────────┘
                         ↓
               [llm:report — thinking model, EndpointMessages,
                BudgetTokens=8000, ZDR=true]
                         ↓
                   P:OUTPUT (ARTIFACT)
```

The `report` transition `LLMConfig`:

```go
LLMConfig{
    Model:    ModelRegistry["thinking"],   // "anthropic/claude-opus-4-6"
    Endpoint: EndpointMessages,
    MaxTokens: 16000,
    Reasoning: &ReasoningConfig{BudgetTokens: 8000},
    Budget:    0.50,
    Provider:  &ProviderConfig{Only: []string{"Anthropic"}, ZDR: true},
    Trace:     &TraceConfig{SpanName: "report-generation", GenerationName: "financial-report"},
}
```

Thinking blocks from the `report` response are compressed into a `RoleObserver` message in the parent CPN history. The `Signature` fields are preserved intact for potential continuation calls.

### 18.2 Full-Stack Marketing Email (v1.1 extended with ProviderConfig and plugins)

The v1.1 example from §16.4 gains these v1.2 additions to the `draft-email` transition:

```go
LLMConfig{
    Model:    ModelRegistry["reasoning"],
    Endpoint: EndpointMessages,          // enables cache_control on system prompt
    MaxTokens: 800,
    Temperature: 0.7,
    RequireJSON: true,
    CacheControl: &CacheControlConfig{TTL: "5m"}, // cache system prompt across calls
    Provider: &ProviderConfig{Sort: "latency"},
    Trace:    &TraceConfig{SpanName: "draft-email"},
    Plugins:  []PluginConfig{{ID: "response-healing", Enabled: boolPtr(true)}},
}
```

---

## 19. Flow Intelligence Engine

Extended in v1.2:

**Phase 1 (original):** Per-CPN metrics from `TokenLedger` estimates.

**Phase 1 extension:** `ActivityClient.SyncToLedger()` runs hourly. Flow rankings incorporate `DailyTotal` from authoritative billing data, not just per-request estimates. Flows with lower actual cost rank higher for the same output quality (operationalizes A11 with real numbers).

**Phase 2 — Pattern Mining:** Unchanged.

**Phase 3 — Flow Crystallization:** Crystallized flows now carry an `ActualCostProfile` (average USD per execution from `ActivityClient`, not estimates). Cost transparency is accurate to within one sync interval.

---

## 20. Linux Sandbox

Unchanged from v1.1.

---

## 21. Package Structure

```
cpn/
├── colors.go           // ColorSet constants
├── space.go            // SpaceKind constants
├── kinds.go            // NodeKind constants
├── mode.go             // CPNMode, CPNState constants
├── token.go            // Token, ThinkingBlock, LLMMessage, IsHumanOrigin()
├── place.go            // Place, Deposit, Consume, Peek
├── retry.go            // RetryPolicy, CircuitBreaker, fireWithRetry,
│                       //   OpenRouterRetryOn, OpenRouterRetryPolicy
├── validate.go         // ValidateConfig, fireValidate
├── transition.go       // Transition, CanFire, LLMConfig, LLMEndpoint,
│                       //   ProviderConfig, ProviderMaxPrice, TraceConfig,
│                       //   PluginConfig, ReasoningConfig, CacheControlConfig,
│                       //   JSONSchemaConfig, HITLConfig
├── group_agent.go      // GroupAgent protocol
├── cpn.go              // CPN struct
├── executor.go         // Run loop, dispatch, fireWithRetry, fireLLM, etc.
├── memory.go           // ContextWindow, buildContext, compressSubNetSummary
├── streaming.go        // StreamHandler, ParseChatSSE, ParseAnthropicSSE     ← NEW v1.2
├── openrouter.go       // OpenRouterClient, buildCompletionsBody,             ← extended v1.2
│                       //   buildMessagesBody, formatChatMessages,
│                       //   formatAnthropicMessages, formatAnthropicTools,
│                       //   formatProviderConfig, formatPlugins, formatTrace
├── token_ledger.go     // TokenLedger, SessionTokenRecord, RecordWithCache    ← extended v1.2
├── model_registry.go   // ModelRegistry map                                   ← updated v1.2
├── guardrails.go       // GuardrailsClient, Guardrail, ApplicationInit        ← NEW v1.2
├── activity.go         // ActivityClient, ActivityRecord, SyncToLedger        ← NEW v1.2
├── validator.go        // CPN-level Validate
├── errors.go           // all typed errors                                    ← extended v1.2
├── session.go          // Session, StreamChunk, Message, ResolveHITL
├── flow_library.go     // FlowLibrary
├── sandbox.go          // SandboxedTool
└── cpn_test.go         // one test function per spec statement
```

---

## 22. Implementation Order

Steps 1–18 from v1.1 are unchanged.

```
Step 1–18  — unchanged from v1.1

Step 19 — OpenRouterClient both endpoints
           openrouter.go: buildCompletionsBody, buildMessagesBody,
           formatAnthropicMessages (ThinkingBlock preservation),
           formatAnthropicTools (input_schema), formatProviderConfig,
           formatPlugins, formatTrace
           Tests: system prompt extracted to top-level field in Anthropic format;
           tools use input_schema not parameters; ThinkingBlock signature intact;
           FallbackModels serialized as "models" array; ProviderConfig ZDR flag set

Step 20 — StreamHandler
           streaming.go: ParseChatSSE, ParseAnthropicSSE
           Tests: chat SSE assembled from multiple delta chunks;
           Anthropic thinking_delta accumulates, signature_delta closes block;
           [DONE] terminates loop; cache tokens captured from message_start;
           error event returns ErrProviderUnavailable

Step 21 — GuardrailsClient
           guardrails.go: Create, AssignKeys, List, Delete, ApplicationInit
           Tests: POST /guardrails serializes name/limit_usd/reset_interval correctly;
           402 from capped key → ErrInsufficientCredits → ErrorPlace fallback;
           ApplicationInit creates guardrail and assigns key in two calls

Step 22 — ActivityClient + TokenLedger sync
           activity.go: Fetch, SyncToLedger, SetDailyTotal
           TokenLedger: RecordWithCache, SetDailyTotal
           Tests: GET /activity parses ActivityRecord fields correctly;
           SyncToLedger aggregates UsageUSD and updates DailyTotal;
           Flow Intelligence ranking uses actual cost after first sync
```

---

## 23. Design Decisions Log

Extends the v1.1 log with v1.2 decisions.

| Decision | Value | Reasoning |
|---|---|---|
| Dual endpoint | `LLMEndpoint` enum on `LLMConfig` | Anthropic extended thinking and prompt caching are only available on `/messages`; all other providers use `/chat/completions` |
| FallbackModels | `[]string` on `LLMConfig` | Maps directly to OpenRouter `models` array; routing is handled server-side with no client logic changes |
| ProviderConfig per-transition | On `LLMConfig` not `CPN` | Different transitions have different requirements: financial report needs ZDR; real-time chat needs lowest latency; sharing a config would force the lowest common denominator |
| ReasoningConfig unified | `Effort`/`Summary`/`BudgetTokens` in one struct | Two different APIs (`reasoning` object vs `thinking` object) unified behind one struct; the executor picks the serialization format based on `LLMConfig.Endpoint` |
| CacheControlConfig at LLMConfig level | Applies to system prompt | Anthropic caches the system prompt (passed as a structured array with cache_control); the same TTL setting applies per-transition |
| ThinkingBlock carries Signature | `ThinkingBlock.Signature string` | Anthropic requires the signature to be passed back unchanged in multi-turn thinking scenarios; making it an explicit field prevents accidental omission |
| ParseAnthropicSSE thinking accumulation | Buffer until signature_delta | The Anthropic SSE spec sends thinking as incremental deltas; only the completed block (thinking text + signature) is meaningful to store |
| StreamHandler as separate struct | `streaming.go` | Two very different SSE formats; isolation enables clean unit testing with mock `io.ReadCloser` |
| GuardrailsClient at startup | Account-level daily cap | Defense in depth: per-transition `Budget` caps single calls; Guardrails cap the 24h total regardless of how many transitions fire |
| ActivityClient sync hourly | `SyncToLedger` on schedule | `/activity` returns daily aggregates; more frequent polling yields no new data and wastes management API calls |
| OpenRouterRetryOn taxonomy | Permanent 4xx never retried | 400/401/402/403/404/413/422 indicate problems with the request itself; retrying wastes credits and delays correct failure surfacing |
| RecordWithCache separate fields | `CacheReadTokens`, `CacheCreationTokens` | Anthropic cache_read tokens are billed at a discount; accurate cost estimation requires tracking them separately from regular input tokens |

---

## 24. What This Document Does Not Include (Future Work)

- Persistence: Session Store and Event Store in Redis/Postgres
- Multi-tenancy and authentication
- Rate limiting per channel
- Billing UI and per-client cost dashboards
- HITL review UI
- CPN topology serialization for persistence
- Distributed executor (sub-CPNs on separate machines)
- Formal verification via CPN Tools
- CPN visual editor
- OpenRouter OAuth/PKCE flow (`/auth/keys/code` + `/auth/keys`) for per-user key provisioning
- Embeddings endpoint (`/embeddings`) for semantic flow matching in Flow Intelligence
- Generation metadata endpoint (`/generation`) for per-request retroactive cost lookup
- ZDR endpoint filtering UI (`/endpoints/zdr`) for regulated environment configuration
- Multi-modal input: image, audio, video content blocks in `LLMMessage`
- Model fallback cost accounting (fallback model may have different pricing)
- `response-healing` plugin integration test with `NodeKindValidate`
- Anthropic `batch` service tier for offline/background processing
- Streaming non-blocking mode: send chunks to `Session.Stream` while executor continues firing other transitions
