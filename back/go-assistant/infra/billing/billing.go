package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Client ─────────────────────────────────────────────────────────

// Client fetches API key info from OpenRouter /key endpoint.
type Client struct {
	// apiKey is the management API key. Never logged or included in errors.
	apiKey string

	// BaseURL is the OpenRouter API base (e.g. "https://openrouter.ai/api/v1").
	BaseURL string

	// HTTPClient is the HTTP client for API calls. Injectable for testing.
	HTTPClient *http.Client
}

// NewClient returns a configured client with sensible defaults.
//
// The HTTP client is tuned for fragile container egress paths: it forces a
// short happy-eyeballs fallback so an IPv6 black-hole (a recurring failure
// mode on Docker/VPS networks where AAAA records resolve but IPv6 routing
// drops packets) cannot stall the dial for the full deadline. Set FORCE_IPV4=1
// to bypass IPv6 entirely.
func NewClient(apiKey, baseURL string) *Client {
	forceV4 := strings.EqualFold(os.Getenv("FORCE_IPV4"), "1") ||
		strings.EqualFold(os.Getenv("FORCE_IPV4"), "true")

	dialer := &net.Dialer{
		Timeout:       8 * time.Second,
		KeepAlive:     30 * time.Second,
		FallbackDelay: 50 * time.Millisecond,
	}
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if forceV4 && (network == "tcp" || network == "tcp6") {
			network = "tcp4"
		}
		return dialer.DialContext(ctx, network, addr)
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	return &Client{
		apiKey:  apiKey,
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// isTransientNetErr reports whether the error is a connection-level failure
// that is safe to retry on an idempotent GET (dial timeout, connection reset,
// EOF before headers, etc). It deliberately does NOT retry on HTTP-level
// errors — those are handled by the caller.
func isTransientNetErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false // caller cancelled — do not retry
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "connection refused")
}

// String implements fmt.Stringer. Redacts the API key.
func (c *Client) String() string {
	return fmt.Sprintf("billing.Client{base: %s}", c.BaseURL)
}

// ── KeyInfo ───────────────────────────────────────────────────────────

// KeyInfo holds the response fields from GET /key.
type KeyInfo struct {
	Label          string   `json:"label"`
	Limit          *float64 `json:"limit"`
	LimitRemaining *float64 `json:"limit_remaining"`
	Usage          float64  `json:"usage"`
	UsageDaily     float64  `json:"usage_daily"`
	UsageWeekly    float64  `json:"usage_weekly"`
	UsageMonthly   float64  `json:"usage_monthly"`
	IsFreeTier     bool     `json:"is_free_tier"`
}

// keyEnvelope is the raw OpenRouter response.
type keyEnvelope struct {
	Data KeyInfo `json:"data"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────

// Fetch retrieves the current API key info from OpenRouter.
func (c *Client) Fetch(ctx context.Context) (*KeyInfo, error) {
	url := c.BaseURL + "/key"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("billing: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	// One-shot retry on transient network errors. The /key endpoint is a
	// pure GET so retrying is safe and never double-bills. We do NOT retry
	// HTTP-level failures (4xx/5xx) — those are surfaced to the caller.
	resp, err := c.HTTPClient.Do(req)
	if err != nil && isTransientNetErr(err) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("billing: request failed: %w", err)
		case <-time.After(200 * time.Millisecond):
		}
		retryReq, rerr := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if rerr == nil {
			retryReq.Header = req.Header.Clone()
			resp, err = c.HTTPClient.Do(retryReq)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("billing: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Cap body read at 1 MB to prevent OOM from misbehaving servers.
	const maxBodySize = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("billing: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing: status %d: %w", resp.StatusCode, mapHTTPStatusToError(resp.StatusCode))
	}

	var env keyEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("billing: decode response: %w", err)
	}

	return &env.Data, nil
}

// ── HTTP Error Mapping ───────────────────────────────────────────────────────

// mapHTTPStatusToError maps OpenRouter HTTP status codes to sentinel errors.
func mapHTTPStatusToError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return cpn.ErrUnauthorized
	case http.StatusForbidden:
		return cpn.ErrForbidden
	case http.StatusTooManyRequests:
		return cpn.ErrRateLimited
	default:
		if status >= 500 && status < 600 {
			return cpn.ErrProviderUnavailable
		}
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
}
