package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Client ────────────────────────────────────────────────────────

// Client manages account-level spending guardrails via the
// OpenRouter management API. Requires a management API key (separate
// from the regular API key used by OpenRouterClient).
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

// ── Guardrail Type ──────────────────────────────────────────────────────────

// Guardrail represents an account-level spending guardrail.
type Guardrail struct {
	// ID is the guardrail identifier assigned by OpenRouter.
	ID string

	// Name is a human-readable label for this guardrail.
	Name string

	// Description provides additional context about the guardrail.
	Description string

	// LimitUSD is the spending limit; reset by ResetInterval.
	LimitUSD float64

	// ResetInterval controls how often the limit resets: "daily", "weekly", "monthly", or "".
	ResetInterval string

	// AllowedProviders restricts to specific provider slugs. nil = all allowed.
	AllowedProviders []string

	// AllowedModels restricts to specific model slugs. nil = all allowed.
	AllowedModels []string

	// EnforceZDR restricts the guardrail to ZDR endpoints only.
	EnforceZDR bool

	// CreatedAt is when the guardrail was created.
	CreatedAt time.Time

	// UpdatedAt is when the guardrail was last updated.
	UpdatedAt *time.Time
}

// ── JSON wire types ─────────────────────────────────────────────────────────

// guardrailJSON is the JSON wire format for a single guardrail.
type guardrailJSON struct {
	ID               string   `json:"id,omitempty"`
	Name             string   `json:"name,omitempty"`
	Description      string   `json:"description,omitempty"`
	LimitUSD         float64  `json:"limit_usd"`
	ResetInterval    string   `json:"reset_interval,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	EnforceZDR       bool     `json:"enforce_zdr"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

// guardrailListJSON wraps the list endpoint response.
type guardrailListJSON struct {
	Data []guardrailJSON `json:"data"`
}

// keysRequestJSON is the request body for AssignKeys / UnassignKeys.
type keysRequestJSON struct {
	KeyHashes []string `json:"key_hashes"`
}

// keysResponseJSON is the response from the keys endpoints.
type keysResponseJSON struct {
	Assigned   int `json:"assigned"`
	Unassigned int `json:"unassigned"`
}

// ── Conversion helpers ──────────────────────────────────────────────────────

func guardrailToJSON(g *Guardrail) guardrailJSON {
	return guardrailJSON{
		Name:             g.Name,
		Description:      g.Description,
		LimitUSD:         g.LimitUSD,
		ResetInterval:    g.ResetInterval,
		AllowedProviders: g.AllowedProviders,
		AllowedModels:    g.AllowedModels,
		EnforceZDR:       g.EnforceZDR,
	}
}

func guardrailFromJSON(j *guardrailJSON) Guardrail {
	g := Guardrail{
		ID:               j.ID,
		Name:             j.Name,
		Description:      j.Description,
		LimitUSD:         j.LimitUSD,
		ResetInterval:    j.ResetInterval,
		AllowedProviders: j.AllowedProviders,
		AllowedModels:    j.AllowedModels,
		EnforceZDR:       j.EnforceZDR,
	}
	if j.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, j.CreatedAt); err == nil {
			g.CreatedAt = t
		}
	}
	if j.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, j.UpdatedAt); err == nil {
			g.UpdatedAt = &t
		}
	}
	return g
}

// ── CRUD Methods ────────────────────────────────────────────────────────────

// Create creates a new guardrail.
// POST /guardrails
func (c *Client) Create(ctx context.Context, g *Guardrail) (Guardrail, error) {
	body, err := json.Marshal(guardrailToJSON(g))
	if err != nil {
		return Guardrail{}, fmt.Errorf("marshal guardrail: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/guardrails", body)
	if err != nil {
		return Guardrail{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, resp.Body)
		return Guardrail{}, mapHTTPStatusToError(resp.StatusCode)
	}

	var j guardrailJSON
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return Guardrail{}, fmt.Errorf("decode guardrail response: %w", err)
	}
	return guardrailFromJSON(&j), nil
}

// Update updates an existing guardrail.
// PUT /guardrails/{id}
func (c *Client) Update(ctx context.Context, id string, g *Guardrail) (Guardrail, error) {
	body, err := json.Marshal(guardrailToJSON(g))
	if err != nil {
		return Guardrail{}, fmt.Errorf("marshal guardrail: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/guardrails/"+id, body)
	if err != nil {
		return Guardrail{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return Guardrail{}, mapHTTPStatusToError(resp.StatusCode)
	}

	var j guardrailJSON
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return Guardrail{}, fmt.Errorf("decode guardrail response: %w", err)
	}
	return guardrailFromJSON(&j), nil
}

// Delete removes a guardrail.
// DELETE /guardrails/{id}
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/guardrails/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return mapHTTPStatusToError(resp.StatusCode)
	}
	return nil
}

// List returns all guardrails for the account.
// GET /guardrails
func (c *Client) List(ctx context.Context) ([]Guardrail, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/guardrails", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, mapHTTPStatusToError(resp.StatusCode)
	}

	var listResp guardrailListJSON
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode guardrails list: %w", err)
	}

	gs := make([]Guardrail, len(listResp.Data))
	for i := range listResp.Data {
		gs[i] = guardrailFromJSON(&listResp.Data[i])
	}
	return gs, nil
}

// ── Key Management ──────────────────────────────────────────────────────────

// AssignKeys assigns API key hashes to a guardrail.
// POST /guardrails/{id}/keys
// Returns the number of keys assigned.
func (c *Client) AssignKeys(ctx context.Context, guardrailID string, keyHashes []string) (int, error) {
	body, err := json.Marshal(keysRequestJSON{KeyHashes: keyHashes})
	if err != nil {
		return 0, fmt.Errorf("marshal key hashes: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/guardrails/"+guardrailID+"/keys", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, mapHTTPStatusToError(resp.StatusCode)
	}

	var keysResp keysResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		return 0, fmt.Errorf("decode keys response: %w", err)
	}
	return keysResp.Assigned, nil
}

// UnassignKeys removes API key hashes from a guardrail.
// DELETE /guardrails/{id}/keys
// Returns the number of keys unassigned.
func (c *Client) UnassignKeys(ctx context.Context, guardrailID string, keyHashes []string) (int, error) {
	body, err := json.Marshal(keysRequestJSON{KeyHashes: keyHashes})
	if err != nil {
		return 0, fmt.Errorf("marshal key hashes: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, "/guardrails/"+guardrailID+"/keys", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, mapHTTPStatusToError(resp.StatusCode)
	}

	var keysResp keysResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		return 0, fmt.Errorf("decode keys response: %w", err)
	}
	return keysResp.Unassigned, nil
}

// ── ApplicationInit ─────────────────────────────────────────────────────────

// ApplicationInit is a startup helper that creates a daily spending guardrail
// and assigns the application API key to it. Makes exactly 2 API calls:
// Create + AssignKeys.
func ApplicationInit(ctx context.Context, client *Client, dailyLimitUSD float64, apiKeyHash string) error {
	g, err := client.Create(ctx, &Guardrail{
		Name:          "app-daily-limit",
		LimitUSD:      dailyLimitUSD,
		ResetInterval: "daily",
	})
	if err != nil {
		return fmt.Errorf("create daily guardrail: %w", err)
	}

	_, err = client.AssignKeys(ctx, g.ID, []string{apiKeyHash})
	if err != nil {
		return fmt.Errorf("assign key to guardrail %s: %w", g.ID, err)
	}

	return nil
}

// ── HTTP helper ─────────────────────────────────────────────────────────────

// doRequest builds and executes an HTTP request with the management API key.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	return resp, nil
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
