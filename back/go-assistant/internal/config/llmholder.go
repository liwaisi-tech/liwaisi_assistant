package config

import (
	"context"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// LLMClientHolder wraps an LLMClient with atomic swap capability for hot-reload.
// It implements cpn.LLMClient so it is transparent to consumers.
type LLMClientHolder struct {
	mu     sync.RWMutex
	client cpn.LLMClient
}

// Compile-time interface check.
var _ cpn.LLMClient = (*LLMClientHolder)(nil)

// NewLLMClientHolder creates a holder with the initial client.
func NewLLMClientHolder(client cpn.LLMClient) *LLMClientHolder {
	return &LLMClientHolder{client: client}
}

// Swap replaces the underlying client atomically.
func (h *LLMClientHolder) Swap(newClient cpn.LLMClient) {
	h.mu.Lock()
	h.client = newClient
	h.mu.Unlock()
}

// Complete delegates to the current client.
func (h *LLMClientHolder) Complete(ctx context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.Complete(ctx, req)
}

// CompleteStream delegates to the current client.
func (h *LLMClientHolder) CompleteStream(ctx context.Context, req *cpn.LLMRequest, onChunk func(chunk string)) (cpn.LLMResponse, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.CompleteStream(ctx, req, onChunk)
}

// EstimateCost delegates to the current client.
func (h *LLMClientHolder) EstimateCost(req *cpn.LLMRequest) (float64, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.EstimateCost(req)
}
