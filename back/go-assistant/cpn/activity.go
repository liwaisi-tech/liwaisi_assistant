package cpn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ── ActivityClient ─────────────────────────────────────────────────────────

// ActivityClient fetches authoritative usage from OpenRouter /activity.
// Requires a management API key (same key used by GuardrailsClient).
type ActivityClient struct {
	// apiKey is the management API key. Never logged or included in errors.
	apiKey string

	// BaseURL is the OpenRouter API base (e.g. "https://openrouter.ai/api/v1").
	BaseURL string

	// HTTPClient is the HTTP client for API calls. Injectable for testing.
	HTTPClient *http.Client
}

// NewActivityClient returns a configured client with sensible defaults.
func NewActivityClient(apiKey, baseURL string) *ActivityClient {
	return &ActivityClient{
		apiKey:  apiKey,
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// String implements fmt.Stringer. Redacts the API key.
func (c *ActivityClient) String() string {
	return fmt.Sprintf("ActivityClient{base: %s}", c.BaseURL)
}

// ── ActivityRecord ─────────────────────────────────────────────────────────

// ActivityRecord is one row from /activity, grouped by (date, model, endpoint).
type ActivityRecord struct {
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
	Data []ActivityRecord `json:"data"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────

// Fetch retrieves activity for the last 30 completed UTC days.
// date: optional YYYY-MM-DD filter; empty string = all 30 days.
func (c *ActivityClient) Fetch(ctx context.Context, date string) ([]ActivityRecord, error) {
	url := c.BaseURL + "/activity"
	if date != "" {
		url += "?date=" + date
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("activity: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("activity: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("activity: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activity: unexpected status %d: %s", resp.StatusCode, string(body))
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
func (c *ActivityClient) SyncToLedger(ctx context.Context, ledger *TokenLedger, date string) error {
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
