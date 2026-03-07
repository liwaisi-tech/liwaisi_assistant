package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newTestPipeline(cfg *Config, client *http.Client) *Pipeline {
	return &Pipeline{client: client, cfg: cfg}
}

func testPipeline(t *testing.T) *Pipeline {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second
	return newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})
}

func TestPipeline_FetchArticle(t *testing.T) {
	html := mustReadFixture(t, "article.html")
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})

	p := testPipeline(t)
	result, err := p.Fetch(context.Background(), srv.URL+"/post/go-concurrency", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if result.Error != "" {
		t.Errorf("unexpected error in result: %s", result.Error)
	}
	if result.Title == "" {
		t.Error("expected non-empty title")
	}
	if !strings.Contains(result.Markdown, "Goroutines") {
		t.Error("expected markdown to contain 'Goroutines'")
	}
	if result.WordCount == 0 {
		t.Error("expected non-zero word count")
	}
	if result.Truncated {
		t.Error("expected truncated to be false")
	}
}

func TestPipeline_FetchRawMode(t *testing.T) {
	html := mustReadFixture(t, "index.html")
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})

	p := testPipeline(t)
	result, err := p.Fetch(context.Background(), srv.URL+"/blog", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if !strings.Contains(result.Markdown, "All Blog Posts") {
		t.Error("expected markdown to contain 'All Blog Posts'")
	}
	if !strings.Contains(result.Markdown, "Go Generics") {
		t.Error("expected raw mode to preserve listing content")
	}
}

func TestPipeline_FetchMinimalHTML(t *testing.T) {
	html := mustReadFixture(t, "minimal.html")
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(html)
	})

	p := testPipeline(t)
	result, err := p.Fetch(context.Background(), srv.URL, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if !strings.Contains(result.Markdown, "Hello, world!") {
		t.Errorf("expected markdown to contain 'Hello, world!', got %q", result.Markdown)
	}
}

func TestPipeline_FetchMalformedHTML(t *testing.T) {
	html := mustReadFixture(t, "malformed.html")
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(html)
	})

	p := testPipeline(t)
	result, err := p.Fetch(context.Background(), srv.URL, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if result.Markdown == "" {
		t.Error("expected non-empty markdown even for malformed HTML")
	}
}

func TestPipeline_NonHTMLContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "plain text",
			contentType: "text/plain",
			body:        "Hello world plain text",
		},
		{
			name:        "JSON",
			contentType: "application/json",
			body:        `{"key": "value", "count": 42}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = w.Write([]byte(tt.body))
			})

			p := testPipeline(t)
			result, err := p.Fetch(context.Background(), srv.URL, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Markdown != tt.body {
				t.Errorf("markdown = %q, want %q", result.Markdown, tt.body)
			}
		})
	}
}

func TestPipeline_HTTPErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{name: "404 Not Found", statusCode: 404, wantErr: "HTTP 404"},
		{name: "500 Internal Server Error", statusCode: 500, wantErr: "HTTP 500"},
		{name: "403 Forbidden", statusCode: 403, wantErr: "HTTP 403"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte("error page"))
			})

			p := testPipeline(t)
			result, err := p.Fetch(context.Background(), srv.URL, false)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if result.StatusCode != tt.statusCode {
				t.Errorf("status = %d, want %d", result.StatusCode, tt.statusCode)
			}
			if !strings.Contains(result.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", result.Error, tt.wantErr)
			}
		})
	}
}

func TestPipeline_BodySizeLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBodySize = 100
	cfg.Timeout = 5 * time.Second
	p := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	bigBody := strings.Repeat("x", 500)
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(bigBody))
	})

	result, err := p.Fetch(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Markdown) > 100 {
		t.Errorf("body should be limited to 100 bytes, got %d", len(result.Markdown))
	}
}

func TestPipeline_ContentTruncation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxContentLength = 50
	cfg.Timeout = 5 * time.Second
	p := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	longText := strings.Repeat("word ", 100)
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(longText))
	})

	result, err := p.Fetch(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Truncated {
		t.Error("expected truncated to be true")
	}
	if !strings.Contains(result.Markdown, "[Content truncated...]") {
		t.Error("expected truncation marker")
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	p := testPipeline(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := p.Fetch(ctx, srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error in result for canceled context")
	}
}

func TestPipeline_InvalidURL(t *testing.T) {
	p := testPipeline(t)
	_, err := p.Fetch(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestPipeline_RelativeURLResolution(t *testing.T) {
	html := `<html><body><article><p>See <a href="/docs/guide">the guide</a> for details.</p></article></body></html>`
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	})

	p := testPipeline(t)
	result, err := p.Fetch(context.Background(), srv.URL+"/page", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Markdown, srv.URL+"/docs/guide") {
		t.Errorf("expected absolute URL in markdown, got: %s", result.Markdown)
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"  spaces   between   words  ", 3},
		{"one\ntwo\tthree", 3},
	}
	for _, tt := range tests {
		got := countWords(tt.input)
		if got != tt.want {
			t.Errorf("countWords(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPipeline_BodySizeExactBoundary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBodySize = 50
	cfg.Timeout = 5 * time.Second
	p := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	exactBody := strings.Repeat("a", 50)
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(exactBody))
	})

	result, err := p.Fetch(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Markdown != exactBody {
		t.Errorf("body at exact limit should be preserved, got len %d want %d", len(result.Markdown), len(exactBody))
	}
}

func TestNewPipeline(t *testing.T) {
	cfg := DefaultConfig()
	p := NewPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if p.client == nil {
		t.Fatal("expected non-nil HTTP client")
	}
	if p.cfg != cfg {
		t.Error("expected pipeline config to match provided config")
	}
}

func TestPipeline_ContentTruncation_NoTruncationWhenUnderLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxContentLength = 1000
	cfg.Timeout = 5 * time.Second
	p := newTestPipeline(cfg, &http.Client{Timeout: cfg.Timeout})

	shortText := "short content"
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(shortText))
	})

	result, err := p.Fetch(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Truncated {
		t.Error("expected truncated to be false for short content")
	}
	if result.Markdown != shortText {
		t.Errorf("markdown = %q, want %q", result.Markdown, shortText)
	}
}

func TestIsHTMLContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"application/xhtml+xml", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isHTMLContentType(tt.ct)
		if got != tt.want {
			t.Errorf("isHTMLContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}
