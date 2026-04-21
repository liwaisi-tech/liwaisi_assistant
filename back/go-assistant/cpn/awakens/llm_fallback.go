package awakens

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// SC-15 Model Fallback Chain.
//
// REQ-1501: primary + ordered fallbacks for awakening LLM transitions.
// REQ-1502: fallback only on provider 5xx / rate-limit / timeout; auth
// errors fail fast.
// REQ-1503: each retry emits awakening.llm.fallback_used with from/to models
// and a coarse reason class.

// AwakeningPrimaryModel is the primary slug for first-boot awakening LLM
// transitions. Kept in sync with session_service_awakening's pinned model.
const AwakeningPrimaryModel = "google/gemini-2.5-flash"

// AwakeningFallbackModels lists the ordered fallbacks tried when the primary
// surfaces a fallback-eligible error. Order is significant (REQ-1501).
var AwakeningFallbackModels = []string{
	"anthropic/claude-haiku-4-5",
	"openai/gpt-4o-mini",
}

// DefaultFallbackChain returns the primary + fallbacks in evaluation order.
func DefaultFallbackChain() FallbackChain {
	models := make([]string, 0, 1+len(AwakeningFallbackModels))
	models = append(models, AwakeningPrimaryModel)
	models = append(models, AwakeningFallbackModels...)
	return FallbackChain{Models: models}
}

// FallbackChain encapsulates an ordered list of model slugs to try.
// The first model is the primary; the remainder are fallbacks tried in order
// when the preceding attempt surfaces a fallback-eligible error.
type FallbackChain struct {
	Models []string
}

// Run invokes exec for each model in order. When exec returns nil, Run stops
// and returns nil. When exec returns a fallback-eligible error, the chain
// advances to the next model and emitter.FallbackUsed is invoked with the
// from/to models and a coarse reason class. Non-eligible errors (auth,
// context cancellation) short-circuit and are returned as-is.
func (c FallbackChain) Run(ctx context.Context, emitter *Emitter, exec func(ctx context.Context, model string) error) error {
	if len(c.Models) == 0 {
		return errors.New("awakens: fallback chain has no models")
	}
	var lastErr error
	for i, model := range c.Models {
		err := exec(ctx, model)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isFallbackEligible(err) {
			return err
		}
		if i == len(c.Models)-1 {
			break
		}
		next := c.Models[i+1]
		emitter.FallbackUsed(ctx, model, next, classifyFallbackReason(err))
	}
	return lastErr
}

// isFallbackEligible reports whether err is a transient provider-side failure
// that justifies trying the next model in the chain. 5xx, 429, request /
// edge timeouts, context deadline exceeded, and net.Error.Timeout() all
// qualify. Auth-style 4xx (401/403) and other permanent failures do not.
func isFallbackEligible(err error) bool {
	if err == nil {
		return false
	}
	// Auth and other permanent client failures — fail fast.
	if errors.Is(err, cpn.ErrUnauthorized) ||
		errors.Is(err, cpn.ErrForbidden) ||
		errors.Is(err, cpn.ErrInsufficientCredits) ||
		errors.Is(err, cpn.ErrBadRequest) ||
		errors.Is(err, cpn.ErrNotFound) ||
		errors.Is(err, cpn.ErrPayloadTooLarge) ||
		errors.Is(err, cpn.ErrUnprocessableEntity) {
		return false
	}
	// Explicit cancellation is not a provider fault.
	if errors.Is(err, context.Canceled) {
		return false
	}
	// Transient / provider-side failures — eligible.
	if errors.Is(err, cpn.ErrRateLimited) ||
		errors.Is(err, cpn.ErrRequestTimeout) ||
		errors.Is(err, cpn.ErrEdgeTimeout) ||
		errors.Is(err, cpn.ErrProviderOverloaded) ||
		errors.Is(err, cpn.ErrProviderUnavailable) ||
		errors.Is(err, cpn.ErrTimeout) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// String-match fallback for wrapped errors that lost sentinel identity.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporarily unavailable") {
		return true
	}
	if strings.Contains(msg, "status 5") || strings.Contains(msg, " 5xx") {
		return true
	}
	return false
}

// classifyFallbackReason buckets err into one of {auth, rate_limit, timeout,
// server_error, unknown} for the fallback_used event. Auth is included
// defensively even though it short-circuits before emission.
func classifyFallbackReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, cpn.ErrUnauthorized), errors.Is(err, cpn.ErrForbidden):
		return "auth"
	case errors.Is(err, cpn.ErrRateLimited):
		return "rate_limit"
	case errors.Is(err, cpn.ErrRequestTimeout),
		errors.Is(err, cpn.ErrEdgeTimeout),
		errors.Is(err, cpn.ErrTimeout),
		errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, cpn.ErrProviderOverloaded),
		errors.Is(err, cpn.ErrProviderUnavailable):
		return "server_error"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429"), strings.Contains(msg, "rate limit"):
		return "rate_limit"
	case strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "status 5"), strings.Contains(msg, " 5xx"), strings.Contains(msg, "temporarily unavailable"):
		return "server_error"
	}
	return "unknown"
}

// FallbackUsed → awakening.llm.fallback_used (REQ-1503). Emitted once per
// retry transition; `from` is the model that failed, `to` is the model the
// chain will attempt next, and `reason` is the classifyFallbackReason bucket.
func (e *Emitter) FallbackUsed(ctx context.Context, from, to, reason string) {
	e.emit(ctx, "llm.fallback_used", []slog.Attr{
		slog.String("from_model", from),
		slog.String("to_model", to),
		slog.String("reason", reason),
	})
}
