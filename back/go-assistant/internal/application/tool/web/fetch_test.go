package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func TestWebFetchHandler(t *testing.T) {
	html := mustReadFixture(t, "article.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second
	pipeline := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	reg := tool.NewRegistry()
	registerWebFetch(reg, pipeline)

	tests := []struct {
		name    string
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "successful fetch",
			args: `{"url": "` + srv.URL + `"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var fr FetchResult
				if err := json.Unmarshal([]byte(result), &fr); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if fr.StatusCode != 200 {
					t.Errorf("status = %d, want 200", fr.StatusCode)
				}
				if fr.Markdown == "" {
					t.Error("expected non-empty markdown")
				}
			},
		},
		{
			name: "fetch with raw=true",
			args: `{"url": "` + srv.URL + `", "raw": true}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var fr FetchResult
				if err := json.Unmarshal([]byte(result), &fr); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if fr.StatusCode != 200 {
					t.Errorf("status = %d, want 200", fr.StatusCode)
				}
			},
		},
		{
			name:    "empty URL",
			args:    `{"url": ""}`,
			wantErr: "URL is required",
		},
		{
			name:    "missing URL field",
			args:    `{}`,
			wantErr: "URL is required",
		},
		{
			name:    "invalid scheme",
			args:    `{"url": "ftp://example.com/file"}`,
			wantErr: "unsupported scheme",
		},
		{
			name:    "malformed JSON args",
			args:    `{invalid`,
			wantErr: "parsing web_fetch arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reg.Execute(context.Background(), "web_fetch", json.RawMessage(tt.args))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestWebFetchHandler_ResultIsValidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second
	pipeline := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	reg := tool.NewRegistry()
	registerWebFetch(reg, pipeline)

	result, err := reg.Execute(context.Background(), "web_fetch", json.RawMessage(`{"url": "`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fr FetchResult
	if err := json.Unmarshal([]byte(result), &fr); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, result)
	}
	if fr.Markdown != "hello" {
		t.Errorf("markdown = %q, want %q", fr.Markdown, "hello")
	}
}
