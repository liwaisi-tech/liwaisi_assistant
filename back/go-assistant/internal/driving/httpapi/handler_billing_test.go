package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/billing"
)

type mockBillingFetcher struct {
	info *billing.KeyInfo
	err  error
}

func (m *mockBillingFetcher) Fetch(_ context.Context) (*billing.KeyInfo, error) {
	return m.info, m.err
}

func TestHandleGetBalance_HappyPath(t *testing.T) {
	limit := 100.0
	remaining := 75.50
	h := &Handlers{
		Logger: testLogger(),
		BillingFetcher: &mockBillingFetcher{
			info: &billing.KeyInfo{
				Limit:          &limit,
				LimitRemaining: &remaining,
				Usage:          24.50,
				UsageDaily:     5.25,
				UsageWeekly:    12.00,
				UsageMonthly:   24.50,
				IsFreeTier:     false,
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/balance", nil)
	rec := httptest.NewRecorder()
	h.HandleGetBalance(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"usage":24.5`) {
		t.Errorf("expected usage in body, got %s", body)
	}
}

func TestHandleGetBalance_NilFetcher(t *testing.T) {
	h := &Handlers{
		Logger:         testLogger(),
		BillingFetcher: nil,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/balance", nil)
	rec := httptest.NewRecorder()
	h.HandleGetBalance(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHandleGetBalance_UpstreamError(t *testing.T) {
	h := &Handlers{
		Logger: testLogger(),
		BillingFetcher: &mockBillingFetcher{
			err: errors.New("connection refused"),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/balance", nil)
	rec := httptest.NewRecorder()
	h.HandleGetBalance(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandleGetBalance_RateLimited(t *testing.T) {
	h := &Handlers{
		Logger: testLogger(),
		BillingFetcher: &mockBillingFetcher{
			err: cpn.ErrRateLimited,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/balance", nil)
	rec := httptest.NewRecorder()
	h.HandleGetBalance(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
}
