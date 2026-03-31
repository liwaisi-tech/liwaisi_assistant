package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
func NewClient(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
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

	resp, err := c.HTTPClient.Do(req)
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
