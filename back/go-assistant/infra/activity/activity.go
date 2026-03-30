package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
)

// ── Client ─────────────────────────────────────────────────────────

// Client fetches authoritative usage from OpenRouter /activity.
// Requires a management API key (same key used by GuardrailsClient).
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
			Timeout: 30 * time.Second,
		},
	}
}

// String implements fmt.Stringer. Redacts the API key.
func (c *Client) String() string {
	return fmt.Sprintf("Client{base: %s}", c.BaseURL)
}

// ── Record ─────────────────────────────────────────────────────────

// Record is one row from /activity, grouped by (date, model, endpoint).
type Record struct {
	// Date is the UTC date in YYYY-MM-DD format.
	Date string `json:"date"`

	// Model is the model identifier used.
	Model string `json:"model"`

	// ModelPermaslug is the permanent slug for the model.
	ModelPermaslug string `json:"model_permaslug"`

	// EndpointID is the endpoint identifier.
	EndpointID string `json:"endpoint_id"`

	// ProviderName is the provider that served the request.
	ProviderName string `json:"provider_name"`

	// UsageUSD is the total usage cost in USD.
	UsageUSD float64 `json:"usage_usd"`

	// BYOKUsageUSD is usage cost when using bring-your-own-key.
	BYOKUsageUSD float64 `json:"byok_usage_usd"`

	// Requests is the number of requests made.
	Requests int `json:"requests"`

	// PromptTokens is the number of prompt tokens consumed.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the number of completion tokens generated.
	CompletionTokens int `json:"completion_tokens"`

	// ReasoningTokens is the number of reasoning tokens used.
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ── JSON wire type ─────────────────────────────────────────────────────────

// activityResponse is the JSON envelope from GET /activity.
type activityResponse struct {
	Data []Record `json:"data"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────

// Fetch retrieves activity for the last 30 completed UTC days.
// date: optional YYYY-MM-DD filter; empty string = all 30 days.
func (c *Client) Fetch(ctx context.Context, date string) ([]Record, error) {
	u, err := url.Parse(c.BaseURL + "/activity")
	if err != nil {
		return nil, fmt.Errorf("activity: parse base URL: %w", err)
	}
	if date != "" {
		q := u.Query()
		q.Set("date", date)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("activity: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("activity: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Cap body read at 1 MB to prevent OOM from misbehaving servers.
	const maxBodySize = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("activity: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activity: status %d: %w", resp.StatusCode, mapHTTPStatusToError(resp.StatusCode))
	}

	var envelope activityResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("activity: decode response: %w", err)
	}

	return envelope.Data, nil
}

// ── SyncToLedger ───────────────────────────────────────────────────────────

// SyncToLedger reconciles TokenLedger estimates with authoritative /activity data.
// Aggregates UsageUSD from all records for the date and updates DailyTotal.
// Designed for hourly cron execution.
func (c *Client) SyncToLedger(ctx context.Context, ledger *openrouter.TokenLedger, date string) error {
	records, err := c.Fetch(ctx, date)
	if err != nil {
		return fmt.Errorf("sync to ledger: %w", err)
	}

	var totalUSD float64
	for i := range records {
		totalUSD += records[i].UsageUSD
	}

	ledger.SetDailyTotal(date, totalUSD)
	return nil
}

// ── HTTP Error Mapping ───────────────────────────────────────────────────────

// mapHTTPStatusToError maps OpenRouter HTTP status codes to sentinel errors.
func mapHTTPStatusToError(status int) error {
	switch status {
	case 400:
		return cpn.ErrBadRequest
	case 401:
		return cpn.ErrUnauthorized
	case 402:
		return cpn.ErrInsufficientCredits
	case 403:
		return cpn.ErrForbidden
	case 404:
		return cpn.ErrNotFound
	case 408:
		return cpn.ErrRequestTimeout
	case 413:
		return cpn.ErrPayloadTooLarge
	case 422:
		return cpn.ErrUnprocessableEntity
	case 429:
		return cpn.ErrRateLimited
	case 524:
		return cpn.ErrEdgeTimeout
	case 529:
		return cpn.ErrProviderOverloaded
	default:
		if status >= 500 && status < 600 {
			return cpn.ErrProviderUnavailable
		}
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
}
