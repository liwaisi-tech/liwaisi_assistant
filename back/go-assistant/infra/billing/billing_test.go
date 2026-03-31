package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestFetch_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/key" {
			t.Errorf("expected /key, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"label": "my-key",
				"limit": 100.0,
				"limit_remaining": 75.50,
				"usage": 24.50,
				"usage_daily": 5.25,
				"usage_weekly": 12.00,
				"usage_monthly": 24.50,
				"is_free_tier": false
			}
		}`))
	}))
	defer srv.Close()

	client := NewClient("test-key", srv.URL)
	info, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Label != "my-key" {
		t.Errorf("expected label my-key, got %s", info.Label)
	}
	if info.Limit == nil || *info.Limit != 100.0 {
		t.Errorf("expected limit 100.0, got %v", info.Limit)
	}
	if info.LimitRemaining == nil || *info.LimitRemaining != 75.50 {
		t.Errorf("expected limit_remaining 75.50, got %v", info.LimitRemaining)
	}
	if info.Usage != 24.50 {
		t.Errorf("expected usage 24.50, got %f", info.Usage)
	}
	if info.UsageDaily != 5.25 {
		t.Errorf("expected usage_daily 5.25, got %f", info.UsageDaily)
	}
	if info.IsFreeTier {
		t.Error("expected is_free_tier false")
	}
}

func TestFetch_NullLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"label": "unlimited",
				"limit": null,
				"limit_remaining": null,
				"usage": 10.0,
				"usage_daily": 2.0,
				"usage_weekly": 5.0,
				"usage_monthly": 10.0,
				"is_free_tier": true
			}
		}`))
	}))
	defer srv.Close()

	client := NewClient("test-key", srv.URL)
	info, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Limit != nil {
		t.Errorf("expected nil limit, got %v", info.Limit)
	}
	if info.LimitRemaining != nil {
		t.Errorf("expected nil limit_remaining, got %v", info.LimitRemaining)
	}
	if !info.IsFreeTier {
		t.Error("expected is_free_tier true")
	}
}

func TestFetch_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()

	client := NewClient("bad-key", srv.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, cpn.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestFetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	client := NewClient("test-key", srv.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, cpn.ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestFetch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	client := NewClient("test-key", srv.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestFetch_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	client := NewClient("test-key", srv.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, cpn.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestString_RedactsKey(t *testing.T) {
	client := NewClient("secret-key", "https://openrouter.ai/api/v1")
	s := client.String()
	if s != "billing.Client{base: https://openrouter.ai/api/v1}" {
		t.Errorf("unexpected string: %s", s)
	}
	if strings.Contains(s, "secret-key") {
		t.Error("API key should be redacted")
	}
}
