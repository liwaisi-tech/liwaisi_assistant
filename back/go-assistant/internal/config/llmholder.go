package config

import (
	"context"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// LLMClientHolder wraps a cpn.LLMClient with a read-write mutex so the
// underlying client can be swapped atomically (e.g. when the API key changes).
// It implements cpn.LLMClient itself, so callers are unaware of the indirection.
type LLMClientHolder struct {
	mu     sync.RWMutex
	client cpn.LLMClient
}

// Compile-time interface assertion.
var _ cpn.LLMClient = (*LLMClientHolder)(nil)

// NewLLMClientHolder creates a holder with the initial client.
func NewLLMClientHolder(client cpn.LLMClient) *LLMClientHolder {
	return &LLMClientHolder{client: client}
}

// Swap replaces the underlying LLM client. Blocks until all in-flight reads complete.
func (h *LLMClientHolder) Swap(newClient cpn.LLMClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = newClient
}

// Complete delegates to the current client under a read lock.
func (h *LLMClientHolder) Complete(ctx context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.Complete(ctx, req)
}

// CompleteStream delegates to the current client under a read lock.
func (h *LLMClientHolder) CompleteStream(ctx context.Context, req *cpn.LLMRequest, onChunk func(chunk string)) (cpn.LLMResponse, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.CompleteStream(ctx, req, onChunk)
}

// EstimateCost delegates to the current client under a read lock.
func (h *LLMClientHolder) EstimateCost(req *cpn.LLMRequest) (float64, error) {
	h.mu.RLock()
	c := h.client
	h.mu.RUnlock()
	return c.EstimateCost(req)
}
