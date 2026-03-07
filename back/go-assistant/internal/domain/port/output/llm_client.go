package output

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ChatRequest holds the parameters for a chat completion request.
type ChatRequest struct {
	Model       string
	Messages    []entity.Message
	Temperature float64
	MaxTokens   int
	Tools       []valueobject.ToolDefinition
	ToolChoice  valueobject.ToolChoice
}

// ChatResponse holds the result of a non-streaming chat completion.
type ChatResponse struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ToolCalls        []valueobject.ToolCall
	FinishReason     string
}

// LLMClient defines the output port for LLM provider interactions.
type LLMClient interface {
	// Complete sends a chat completion request and returns the full response.
	Complete(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// CompleteStream sends a chat completion request and returns a channel
	// that emits content deltas as they arrive via SSE.
	CompleteStream(ctx context.Context, req *ChatRequest) (<-chan valueobject.StreamChunk, error)
}
