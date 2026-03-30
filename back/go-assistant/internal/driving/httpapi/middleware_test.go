package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChain(t *testing.T) {
	var order []int
	mw := func(n int) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, n)
				next.ServeHTTP(w, r)
			})
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, 0)
	})

	chained := Chain(handler, mw(1), mw(2), mw(3))
	chained.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if len(order) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(order))
	}
	// First middleware listed should execute first (outermost).
	expected := []int{1, 2, 3, 0}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	var ctxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	RequestIDMiddleware(inner).ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("X-Request-ID header not set")
	}
	if len(headerID) != 32 {
		t.Errorf("expected 32 hex chars, got %d (%q)", len(headerID), headerID)
	}
	if ctxID != headerID {
		t.Errorf("context ID %q != header ID %q", ctxID, headerID)
	}
}

func TestRequestIDFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "with ID",
			ctx:  context.WithValue(context.Background(), requestIDKey, "abc123"),
			want: "abc123",
		},
		{
			name: "missing",
			ctx:  context.Background(),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequestIDFromContext(tt.ctx)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	LoggingMiddleware(logger)(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}
	if entry["method"] != "POST" {
		t.Errorf("method = %v, want POST", entry["method"])
	}
	if entry["path"] != "/test" {
		t.Errorf("path = %v, want /test", entry["path"])
	}
	// status is logged as a float64 from JSON
	if status, ok := entry["status"].(float64); !ok || int(status) != 201 {
		t.Errorf("status = %v, want 201", entry["status"])
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	RecoveryMiddleware(logger)(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "internal error" {
		t.Errorf("error = %q, want %q", resp["error"], "internal error")
	}

	if buf.Len() == 0 {
		t.Error("expected panic to be logged")
	}
}

func TestRecoveryMiddleware_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	RecoveryMiddleware(logger)(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestCORSMiddleware_AllowedOrigin(t *testing.T) {
	mw := CORSMiddleware([]string{"https://example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	mw(inner).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Allow-Origin = %q, want %q", got, "https://example.com")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Errorf("Allow-Methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "X-Request-ID" {
		t.Errorf("Expose-Headers = %q", got)
	}
}

func TestCORSMiddleware_DisallowedOrigin(t *testing.T) {
	mw := CORSMiddleware([]string{"https://example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	mw(inner).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin should be empty for disallowed origin, got %q", got)
	}
}

func TestCORSMiddleware_Wildcard(t *testing.T) {
	mw := CORSMiddleware([]string{"*"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.com")
	mw(inner).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.com" {
		t.Errorf("Allow-Origin = %q, want %q", got, "https://anything.com")
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	mw := CORSMiddleware([]string{"https://example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for preflight")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	mw(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age = %q, want %q", got, "86400")
	}
}

func TestCORSMiddleware_LastEventID(t *testing.T) {
	mw := CORSMiddleware([]string{"*"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	mw(inner).ServeHTTP(rec, req)

	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	if allowed == "" {
		t.Fatal("Access-Control-Allow-Headers not set")
	}
	// Check that Last-Event-ID is included in allowed headers.
	if !containsHeader(allowed, "Last-Event-ID") {
		t.Errorf("Allow-Headers %q does not include Last-Event-ID", allowed)
	}
}

func containsHeader(headerValue, target string) bool {
	// Simple check: the header value is a comma-separated list.
	for _, part := range bytes.Split([]byte(headerValue), []byte(",")) {
		trimmed := bytes.TrimSpace(part)
		if string(trimmed) == target {
			return true
		}
	}
	return false
}
