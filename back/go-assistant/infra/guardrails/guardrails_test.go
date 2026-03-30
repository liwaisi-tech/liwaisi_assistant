package guardrails

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Test Helper ─────────────────────────────────────────────────────────────

func mockGuardrailsServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewClient("mgmt-api-key-secret", srv.URL)
	return srv, client
}

// ── Constructor ─────────────────────────────────────────────────────────────

func TestNewClient(t *testing.T) {
	client := NewClient("mgmt-key", "https://openrouter.ai/api/v1")

	if client.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "https://openrouter.ai/api/v1")
	}
	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}

	// Verify apiKey is set correctly via String() — key must NOT appear.
	s := client.String()
	if s != "Client{base: https://openrouter.ai/api/v1}" {
		t.Errorf("String() = %q, unexpected", s)
	}
}

// ── Create ──────────────────────────────────────────────────────────────────

func TestGuardrailsClient_Create(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath, gotAuth string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "gr-123",
			"name":           "app-daily-limit",
			"description":    "Daily spending cap",
			"limit_usd":      50.0,
			"reset_interval": "daily",
			"enforce_zdr":    false,
			"created_at":     "2026-03-28T00:00:00Z",
		})
	})

	g, err := client.Create(context.Background(), &Guardrail{
		Name:          "app-daily-limit",
		Description:   "Daily spending cap",
		LimitUSD:      50.0,
		ResetInterval: "daily",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("Method = %q, want POST", gotMethod)
	}
	if gotPath != "/guardrails" {
		t.Errorf("Path = %q, want /guardrails", gotPath)
	}
	if gotAuth != "Bearer mgmt-api-key-secret" {
		t.Errorf("Authorization = %q, want Bearer mgmt-api-key-secret", gotAuth)
	}

	// Verify serialized body fields.
	if gotBody["name"] != "app-daily-limit" {
		t.Errorf("body.name = %v, want app-daily-limit", gotBody["name"])
	}
	if gotBody["limit_usd"] != 50.0 {
		t.Errorf("body.limit_usd = %v, want 50", gotBody["limit_usd"])
	}
	if gotBody["reset_interval"] != "daily" {
		t.Errorf("body.reset_interval = %v, want daily", gotBody["reset_interval"])
	}

	// Verify parsed response.
	if g.ID != "gr-123" {
		t.Errorf("ID = %q, want gr-123", g.ID)
	}
	if g.Name != "app-daily-limit" {
		t.Errorf("Name = %q, want app-daily-limit", g.Name)
	}
	if g.LimitUSD != 50.0 {
		t.Errorf("LimitUSD = %f, want 50", g.LimitUSD)
	}
}

// ── Update ──────────────────────────────────────────────────────────────────

func TestGuardrailsClient_Update(t *testing.T) {
	var gotMethod, gotPath string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "gr-123",
			"name":           "updated-limit",
			"limit_usd":      100.0,
			"reset_interval": "weekly",
			"created_at":     "2026-03-28T00:00:00Z",
			"updated_at":     "2026-03-28T12:00:00Z",
		})
	})

	g, err := client.Update(context.Background(), "gr-123", &Guardrail{
		Name:          "updated-limit",
		LimitUSD:      100.0,
		ResetInterval: "weekly",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("Method = %q, want PUT", gotMethod)
	}
	if gotPath != "/guardrails/gr-123" {
		t.Errorf("Path = %q, want /guardrails/gr-123", gotPath)
	}
	if g.Name != "updated-limit" {
		t.Errorf("Name = %q, want updated-limit", g.Name)
	}
	if g.LimitUSD != 100.0 {
		t.Errorf("LimitUSD = %f, want 100", g.LimitUSD)
	}
	if g.UpdatedAt == nil {
		t.Error("UpdatedAt is nil, want non-nil")
	}
}

// ── List ────────────────────────────────────────────────────────────────────

func TestGuardrailsClient_List(t *testing.T) {
	var gotMethod, gotPath string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gr-1", "name": "limit-a", "limit_usd": 10.0, "created_at": "2026-03-28T00:00:00Z"},
				{"id": "gr-2", "name": "limit-b", "limit_usd": 20.0, "created_at": "2026-03-28T00:00:00Z"},
			},
		})
	})

	guardrails, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("Method = %q, want GET", gotMethod)
	}
	if gotPath != "/guardrails" {
		t.Errorf("Path = %q, want /guardrails", gotPath)
	}
	if len(guardrails) != 2 {
		t.Fatalf("len(guardrails) = %d, want 2", len(guardrails))
	}
	if guardrails[0].ID != "gr-1" {
		t.Errorf("guardrails[0].ID = %q, want gr-1", guardrails[0].ID)
	}
	if guardrails[1].LimitUSD != 20.0 {
		t.Errorf("guardrails[1].LimitUSD = %f, want 20", guardrails[1].LimitUSD)
	}
}

// ── Delete ──────────────────────────────────────────────────────────────────

func TestGuardrailsClient_Delete(t *testing.T) {
	var gotMethod, gotPath string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	err := client.Delete(context.Background(), "gr-456")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("Method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/guardrails/gr-456" {
		t.Errorf("Path = %q, want /guardrails/gr-456", gotPath)
	}
}

// ── AssignKeys ──────────────────────────────────────────────────────────────

func TestGuardrailsClient_AssignKeys(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"assigned": 2,
		})
	})

	n, err := client.AssignKeys(context.Background(), "gr-123", []string{"hash-a", "hash-b"})
	if err != nil {
		t.Fatalf("AssignKeys() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("Method = %q, want POST", gotMethod)
	}
	if gotPath != "/guardrails/gr-123/keys" {
		t.Errorf("Path = %q, want /guardrails/gr-123/keys", gotPath)
	}

	hashes, ok := gotBody["key_hashes"].([]any)
	if !ok {
		t.Fatalf("body.key_hashes not a []any")
	}
	if len(hashes) != 2 {
		t.Fatalf("len(key_hashes) = %d, want 2", len(hashes))
	}
	if hashes[0] != "hash-a" {
		t.Errorf("key_hashes[0] = %v, want hash-a", hashes[0])
	}
	if n != 2 {
		t.Errorf("assigned = %d, want 2", n)
	}
}

// ── UnassignKeys ────────────────────────────────────────────────────────────

func TestGuardrailsClient_UnassignKeys(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unassigned": 1,
		})
	})

	n, err := client.UnassignKeys(context.Background(), "gr-123", []string{"hash-a"})
	if err != nil {
		t.Fatalf("UnassignKeys() error = %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("Method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/guardrails/gr-123/keys" {
		t.Errorf("Path = %q, want /guardrails/gr-123/keys", gotPath)
	}

	hashes, ok := gotBody["key_hashes"].([]any)
	if !ok {
		t.Fatalf("body.key_hashes not a []any")
	}
	if len(hashes) != 1 {
		t.Fatalf("len(key_hashes) = %d, want 1", len(hashes))
	}
	if n != 1 {
		t.Errorf("unassigned = %d, want 1", n)
	}
}

// ── 402 → ErrInsufficientCredits ────────────────────────────────────────────

func TestGuardrailsClient_402_InsufficientCredits(t *testing.T) {
	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	})

	_, err := client.Create(context.Background(), &Guardrail{
		Name:     "will-fail",
		LimitUSD: 10.0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, cpn.ErrInsufficientCredits) {
		t.Errorf("error = %v, want ErrInsufficientCredits", err)
	}
}

// ── ApplicationInit — Two Calls ─────────────────────────────────────────────

func TestApplicationInit_TwoCalls(t *testing.T) {
	var callCount atomic.Int32

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)

		w.Header().Set("Content-Type", "application/json")

		switch n {
		case 1:
			// First call: Create guardrail
			if r.Method != http.MethodPost {
				t.Errorf("call 1: Method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/guardrails" {
				t.Errorf("call 1: Path = %q, want /guardrails", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "gr-init",
				"name":           "app-daily-limit",
				"limit_usd":      25.0,
				"reset_interval": "daily",
				"created_at":     "2026-03-28T00:00:00Z",
			})
		case 2:
			// Second call: AssignKeys
			if r.Method != http.MethodPost {
				t.Errorf("call 2: Method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/guardrails/gr-init/keys" {
				t.Errorf("call 2: Path = %q, want /guardrails/gr-init/keys", r.URL.Path)
			}

			body, _ := io.ReadAll(r.Body)
			var reqBody map[string]any
			_ = json.Unmarshal(body, &reqBody)

			hashes, ok := reqBody["key_hashes"].([]any)
			if !ok || len(hashes) != 1 || hashes[0] != "app-key-hash" {
				t.Errorf("call 2: key_hashes = %v, want [app-key-hash]", reqBody["key_hashes"])
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"assigned": 1,
			})
		default:
			t.Errorf("unexpected call %d", n)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	err := ApplicationInit(context.Background(), client, 25.0, "app-key-hash")
	if err != nil {
		t.Fatalf("ApplicationInit() error = %v", err)
	}

	if c := callCount.Load(); c != 2 {
		t.Errorf("API calls = %d, want exactly 2", c)
	}
}

// ── ApplicationInit — Create Fails ──────────────────────────────────────────

func TestApplicationInit_CreateFails(t *testing.T) {
	var callCount atomic.Int32

	_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := ApplicationInit(context.Background(), client, 25.0, "app-key-hash")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if c := callCount.Load(); c != 1 {
		t.Errorf("API calls = %d, want exactly 1 (AssignKeys should not be called)", c)
	}
}

// ── Auth Header ─────────────────────────────────────────────────────────────

func TestGuardrailsClient_AuthHeader(t *testing.T) {
	tests := []struct {
		name   string
		method func(context.Context, *Client) error
	}{
		{
			name: "Create",
			method: func(ctx context.Context, c *Client) error {
				_, err := c.Create(ctx, &Guardrail{Name: "test"})
				return err
			},
		},
		{
			name: "List",
			method: func(ctx context.Context, c *Client) error {
				_, err := c.List(ctx)
				return err
			},
		},
		{
			name: "Delete",
			method: func(ctx context.Context, c *Client) error {
				return c.Delete(ctx, "gr-1")
			},
		},
		{
			name: "AssignKeys",
			method: func(ctx context.Context, c *Client) error {
				_, err := c.AssignKeys(ctx, "gr-1", []string{"h"})
				return err
			},
		},
		{
			name: "UnassignKeys",
			method: func(ctx context.Context, c *Client) error {
				_, err := c.UnassignKeys(ctx, "gr-1", []string{"h"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			_, client := mockGuardrailsServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				// Return valid JSON for each endpoint type.
				switch {
				case r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
				case r.Method == http.MethodDelete && r.URL.Path == "/guardrails/gr-1":
					w.WriteHeader(http.StatusNoContent)
				default:
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id": "gr-1", "name": "test", "created_at": "2026-03-28T00:00:00Z",
						"assigned": 1, "unassigned": 1,
					})
				}
			})

			_ = tt.method(context.Background(), client)

			if gotAuth != "Bearer mgmt-api-key-secret" {
				t.Errorf("%s: Authorization = %q, want Bearer mgmt-api-key-secret", tt.name, gotAuth)
			}
		})
	}
}
