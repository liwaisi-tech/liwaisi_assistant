package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/billing"
)

// BillingFetcher abstracts the billing client for testing.
type BillingFetcher interface {
	Fetch(ctx context.Context) (*billing.KeyInfo, error)
}

// HandleGetBalance returns the current OpenRouter API key balance.
func (h *Handlers) HandleGetBalance(w http.ResponseWriter, r *http.Request) {
	if h.BillingFetcher == nil {
		writeError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}

	info, err := h.BillingFetcher.Fetch(r.Context())
	if err != nil {
		h.Logger.Error("billing fetch failed", "error", err)

		if errors.Is(err, cpn.ErrRateLimited) {
			writeError(w, http.StatusTooManyRequests, "upstream rate limited")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to fetch balance")
		return
	}

	writeJSON(w, http.StatusOK, BalanceResponse{
		LimitRemaining: info.LimitRemaining,
		Usage:          info.Usage,
		UsageDaily:     info.UsageDaily,
		UsageWeekly:    info.UsageWeekly,
		UsageMonthly:   info.UsageMonthly,
		IsFreeTier:     info.IsFreeTier,
	})
}
