package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// ── Test Helper ─────────────────────────────────────────────────────────────

func mockActivityServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *ActivityClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewActivityClient("mgmt-api-key-secret", srv.URL)
	return srv, client
}

// ── Constructor ─────────────────────────────────────────────────────────────

func TestNewActivityClient(t *testing.T) {
	client := NewActivityClient("mgmt-key", "https://openrouter.ai/api/v1")

	if client.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "https://openrouter.ai/api/v1")
	}
	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}

	s := client.String()
	if s != "ActivityClient{base: https://openrouter.ai/api/v1}" {
		t.Errorf("String() = %q, unexpected", s)
	}
}

// ── Fetch: All Days ────────────────────────────────────────────────────────

func TestActivityClient_Fetch_AllDays(t *testing.T) {
	var gotMethod, gotPath, gotAuth string

	_, client := mockActivityServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"date":              "2026-03-27",
					"model":             "anthropic/claude-sonnet-4",
					"model_permaslug":   "anthropic/claude-sonnet-4",
					"endpoint_id":       "ep-1",
					"provider_name":     "anthropic",
					"usage_usd":         0.05,
					"byok_usage_usd":    0.0,
					"requests":          10,
					"prompt_tokens":     500,
					"completion_tokens": 200,
					"reasoning_tokens":  0,
				},
				{
					"date":              "2026-03-26",
					"model":             "openai/gpt-4o",
					"model_permaslug":   "openai/gpt-4o",
					"endpoint_id":       "ep-2",
					"provider_name":     "openai",
					"usage_usd":         0.10,
					"byok_usage_usd":    0.01,
					"requests":          5,
					"prompt_tokens":     300,
					"completion_tokens": 100,
					"reasoning_tokens":  50,
				},
			},
		})
	})

	records, err := client.Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/activity" {
		t.Errorf("path = %q, want /activity", gotPath)
	}
	if gotAuth != "Bearer mgmt-api-key-secret" {
		t.Errorf("auth = %q, want Bearer mgmt-api-key-secret", gotAuth)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Date != "2026-03-27" {
		t.Errorf("records[0].Date = %q, want 2026-03-27", records[0].Date)
	}
	if records[1].Model != "openai/gpt-4o" {
		t.Errorf("records[1].Model = %q, want openai/gpt-4o", records[1].Model)
	}
}

// ── Fetch: With Date Filter ────────────────────────────────────────────────

func TestActivityClient_Fetch_WithDate(t *testing.T) {
	var gotQuery string

	_, client := mockActivityServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"date":              "2026-03-27",
					"model":             "anthropic/claude-sonnet-4",
					"model_permaslug":   "anthropic/claude-sonnet-4",
					"endpoint_id":       "ep-1",
					"provider_name":     "anthropic",
					"usage_usd":         0.05,
					"byok_usage_usd":    0.0,
					"requests":          10,
					"prompt_tokens":     500,
					"completion_tokens": 200,
					"reasoning_tokens":  0,
				},
			},
		})
	})

	records, err := client.Fetch(context.Background(), "2026-03-27")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if gotQuery != "date=2026-03-27" {
		t.Errorf("query = %q, want date=2026-03-27", gotQuery)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
}

// ── Fetch: Empty Response ──────────────────────────────────────────────────

func TestActivityClient_Fetch_EmptyResponse(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{},
		})
	})

	records, err := client.Fetch(context.Background(), "2026-01-01")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(records) != 0 {
		t.Errorf("got %d records, want 0", len(records))
	}
}

// ── Fetch: Parses All Fields ───────────────────────────────────────────────

func TestActivityClient_Fetch_ParsesAllFields(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"date":              "2026-03-28",
					"model":             "anthropic/claude-opus-4",
					"model_permaslug":   "anthropic/claude-opus-4-2025-04-14",
					"endpoint_id":       "ep-messages",
					"provider_name":     "anthropic",
					"usage_usd":         1.23,
					"byok_usage_usd":    0.45,
					"requests":          42,
					"prompt_tokens":     10000,
					"completion_tokens": 5000,
					"reasoning_tokens":  2000,
				},
			},
		})
	})

	records, err := client.Fetch(context.Background(), "2026-03-28")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	rec := records[0]
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Date", rec.Date, "2026-03-28"},
		{"Model", rec.Model, "anthropic/claude-opus-4"},
		{"ModelPermaslug", rec.ModelPermaslug, "anthropic/claude-opus-4-2025-04-14"},
		{"EndpointID", rec.EndpointID, "ep-messages"},
		{"ProviderName", rec.ProviderName, "anthropic"},
		{"UsageUSD", rec.UsageUSD, 1.23},
		{"BYOKUsageUSD", rec.BYOKUsageUSD, 0.45},
		{"Requests", rec.Requests, 42},
		{"PromptTokens", rec.PromptTokens, 10000},
		{"CompletionTokens", rec.CompletionTokens, 5000},
		{"ReasoningTokens", rec.ReasoningTokens, 2000},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// ── Fetch: HTTP Error ──────────────────────────────────────────────────────

func TestActivityClient_Fetch_HTTPError(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	})

	_, err := client.Fetch(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestActivityClient_Fetch_HTTPError_500(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	})

	_, err := client.Fetch(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}

// ── Fetch: Context Cancellation ────────────────────────────────────────────

func TestActivityClient_Fetch_ContextCancelled(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Fetch(ctx, "")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// ── SyncToLedger: Aggregates Usage ─────────────────────────────────────────

func TestSyncToLedger_AggregatesUsage(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"date": "2026-03-27", "model": "model-a", "usage_usd": 0.50},
				{"date": "2026-03-27", "model": "model-b", "usage_usd": 0.30},
				{"date": "2026-03-27", "model": "model-c", "usage_usd": 0.20},
			},
		})
	})

	ledger := NewTokenLedger()

	err := client.SyncToLedger(context.Background(), ledger, "2026-03-27")
	if err != nil {
		t.Fatalf("SyncToLedger() error = %v", err)
	}

	total, ok := ledger.GetDailyTotal("2026-03-27")
	if !ok {
		t.Fatal("GetDailyTotal returned false, expected true")
	}

	// 0.50 + 0.30 + 0.20 = 1.00
	const want = 1.0
	if diff := total - want; diff < -0.001 || diff > 0.001 {
		t.Errorf("total = %f, want %f", total, want)
	}
}

// ── SyncToLedger: Fetch Error ──────────────────────────────────────────────

func TestSyncToLedger_FetchError(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	})

	ledger := NewTokenLedger()

	err := client.SyncToLedger(context.Background(), ledger, "2026-03-27")
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}

	// Ledger should be unchanged.
	_, ok := ledger.GetDailyTotal("2026-03-27")
	if ok {
		t.Error("ledger should not have daily total after fetch error")
	}
}

// ── TokenLedger: SetDailyTotal / GetDailyTotal ─────────────────────────────

func TestTokenLedger_SetDailyTotal(t *testing.T) {
	ledger := NewTokenLedger()

	ledger.SetDailyTotal("2026-03-27", 1.50)

	total, ok := ledger.GetDailyTotal("2026-03-27")
	if !ok {
		t.Fatal("GetDailyTotal returned false")
	}
	if total != 1.50 {
		t.Errorf("total = %f, want 1.50", total)
	}

	// Overwrite with new value.
	ledger.SetDailyTotal("2026-03-27", 2.75)

	total, ok = ledger.GetDailyTotal("2026-03-27")
	if !ok {
		t.Fatal("GetDailyTotal returned false after overwrite")
	}
	if total != 2.75 {
		t.Errorf("total = %f, want 2.75 after overwrite", total)
	}
}

// ── TokenLedger: GetDailyTotal Not Synced ──────────────────────────────────

func TestTokenLedger_GetDailyTotal_NotSynced(t *testing.T) {
	ledger := NewTokenLedger()

	total, ok := ledger.GetDailyTotal("2099-01-01")
	if ok {
		t.Error("expected ok=false for unsynced date")
	}
	if total != 0 {
		t.Errorf("total = %f, want 0", total)
	}
}

// ── TokenLedger: Concurrent SetDailyTotal ──────────────────────────────────

func TestTokenLedger_Concurrent_SetDailyTotal(t *testing.T) {
	ledger := NewTokenLedger()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()
			ledger.SetDailyTotal("2026-03-27", float64(i))
			ledger.GetDailyTotal("2026-03-27")
			// Also mix Record and SetDailyTotal to test mutex interaction.
			ledger.Record("session-concurrent", 10, 5, 0.01)
		}()
	}

	wg.Wait()

	// Verify we can read without panic — exact value is non-deterministic.
	_, ok := ledger.GetDailyTotal("2026-03-27")
	if !ok {
		t.Error("expected daily total to be set after concurrent writes")
	}

	rec := ledger.Get("session-concurrent")
	if rec == nil {
		t.Fatal("expected session record after concurrent writes")
	}
	if rec.Calls != goroutines {
		t.Errorf("calls = %d, want %d", rec.Calls, goroutines)
	}
}

// ── Fetch: Auth Header ─────────────────────────────────────────────────────

func TestActivityClient_AuthHeader(t *testing.T) {
	var gotAuth string

	_, client := mockActivityServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})

	_, err := client.Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotAuth != "Bearer mgmt-api-key-secret" {
		t.Errorf("auth = %q, want Bearer mgmt-api-key-secret", gotAuth)
	}
}

// ── SyncToLedger: Empty Activity ───────────────────────────────────────────

func TestSyncToLedger_EmptyActivity(t *testing.T) {
	_, client := mockActivityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})

	ledger := NewTokenLedger()

	err := client.SyncToLedger(context.Background(), ledger, "2026-03-27")
	if err != nil {
		t.Fatalf("SyncToLedger() error = %v", err)
	}

	total, ok := ledger.GetDailyTotal("2026-03-27")
	if !ok {
		t.Fatal("expected daily total to be set even for empty activity")
	}
	if total != 0 {
		t.Errorf("total = %f, want 0 for empty activity", total)
	}
}
