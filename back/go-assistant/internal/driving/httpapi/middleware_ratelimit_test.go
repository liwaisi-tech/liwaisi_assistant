package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// ── Sliding window bucket ───────────────────────────────────────────────────

func TestBucket_AllowWithinLimit(t *testing.T) {
	b := &bucket{}
	now := time.Now()
	limit := 5
	window := time.Minute

	for i := range limit {
		if !b.allow(limit, window, now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("request %d should be allowed within limit %d", i+1, limit)
		}
	}
}

func TestBucket_DenyExceedsLimit(t *testing.T) {
	b := &bucket{}
	now := time.Now()
	limit := 3
	window := time.Minute

	for i := range limit {
		b.allow(limit, window, now.Add(time.Duration(i)*time.Millisecond))
	}

	if b.allow(limit, window, now.Add(time.Duration(limit)*time.Millisecond)) {
		t.Fatal("request beyond limit should be denied")
	}
}

func TestBucket_WindowExpiry(t *testing.T) {
	b := &bucket{}
	limit := 2
	window := 100 * time.Millisecond
	now := time.Now()

	// Fill to capacity.
	b.allow(limit, window, now)
	b.allow(limit, window, now.Add(10*time.Millisecond))

	if b.allow(limit, window, now.Add(50*time.Millisecond)) {
		t.Fatal("should be denied while window is still active")
	}

	// After window expires, capacity should recover.
	future := now.Add(window + time.Millisecond)
	if !b.allow(limit, window, future) {
		t.Fatal("should be allowed after window expiry")
	}
}

func TestBucket_RetryAfter(t *testing.T) {
	b := &bucket{}
	limit := 2
	window := time.Minute
	now := time.Now()

	b.allow(limit, window, now)
	b.allow(limit, window, now.Add(time.Second))

	// Bucket is full; oldest is at `now`.
	oldest := b.oldest()
	retryAfter := oldest.Add(window).Sub(now.Add(2 * time.Second))

	// retryAfter should be approximately window - 2s = 58s.
	if retryAfter < 57*time.Second || retryAfter > 59*time.Second {
		t.Errorf("retryAfter = %v, expected ~58s", retryAfter)
	}
}

// ── SSE concurrent limiter ──────────────────────────────────────────────────

func TestSSE_AcquireWithinLimit(t *testing.T) {
	cfg := DefaultRateLimitConfig() // SSEMaxConcurrent = 3
	rl := newRateLimiter(cfg)
	defer close(rl.stopCleanup)

	for i := range 3 {
		if !rl.acquireSSE("user-1") {
			t.Fatalf("SSE acquire %d should succeed (limit=3)", i+1)
		}
	}
}

func TestSSE_AcquireDenied(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	rl := newRateLimiter(cfg)
	defer close(rl.stopCleanup)

	for range 3 {
		rl.acquireSSE("user-1")
	}

	if rl.acquireSSE("user-1") {
		t.Fatal("4th SSE connection should be denied")
	}
}

func TestSSE_ReleaseRestoresCapacity(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	rl := newRateLimiter(cfg)
	defer close(rl.stopCleanup)

	for range 3 {
		rl.acquireSSE("user-1")
	}

	rl.releaseSSE("user-1")

	if !rl.acquireSSE("user-1") {
		t.Fatal("acquire after release should succeed")
	}
}

// ── Rate limit middleware (HTTP integration) ────────────────────────────────

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimitMiddleware_PublicRouteBypassed(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  1,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  1,
		MessageRateLimit:  1,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	// Even after exhausting the general limit, health should pass.
	for range 5 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("health endpoint returned %d, want 200", rr.Code)
		}
	}
}

func TestRateLimitMiddleware_GeneralRateEnforced(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  2,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  10,
		MessageRateLimit:  100,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	for i := range 3 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if i < 2 && rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
		if i == 2 {
			if rr.Code != http.StatusTooManyRequests {
				t.Fatalf("request 3: expected 429, got %d", rr.Code)
			}
			if rr.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After header on 429 response")
			}
		}
	}
}

func TestRateLimitMiddleware_MessageRateEnforced(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  100,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  10,
		MessageRateLimit:  1,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	// First POST to /messages passes.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/123/messages", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first message: expected 200, got %d", rr.Code)
	}

	// Second POST to /messages is rate limited.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/123/messages", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second message: expected 429, got %d", rr2.Code)
	}
}

func TestRateLimitMiddleware_SSERateEnforced(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  100,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  1,
		MessageRateLimit:  100,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	// First SSE connection succeeds.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/123/events", nil).WithContext(t.Context())
	req1.RemoteAddr = "10.0.0.1:1234"
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first SSE: expected 200, got %d", rr1.Code)
	}

	// Second SSE connection denied (limit=1).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/456/events", nil).WithContext(t.Context())
	req2.RemoteAddr = "10.0.0.1:1234"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second SSE: expected 429, got %d", rr2.Code)
	}
}

func TestRateLimitMiddleware_UserKeyFromAuth(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  1,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  10,
		MessageRateLimit:  100,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	user := &auth.AuthenticatedUser{Sub: "user-abc"}

	// Two different IPs but same authenticated user — should share rate limit.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	req1 = req1.WithContext(auth.NewContext(req1.Context(), user))
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	req2 = req2.WithContext(auth.NewContext(req2.Context(), user))
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request (same user): expected 429, got %d", rr2.Code)
	}
}

func TestRateLimitMiddleware_FallsBackToIP(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  1,
		GeneralRateWindow: time.Minute,
		SSEMaxConcurrent:  10,
		MessageRateLimit:  100,
		MessageRateWindow: time.Minute,
	}
	handler := rateLimitMiddleware(cfg)(okHandler())

	// No auth context — should use IP as key.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}

	// Same IP, different port — same rate limit key.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req2.RemoteAddr = "10.0.0.1:9999"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request (same IP): expected 429, got %d", rr2.Code)
	}

	// Different IP — separate rate limit bucket, should pass.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req3.RemoteAddr = "10.0.0.2:1234"
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("request from different IP: expected 200, got %d", rr3.Code)
	}
}

// ── Cleanup goroutine ───────────────────────────────────────────────────────

func TestCleanup_RemovesExpiredEntries(t *testing.T) {
	cfg := RateLimitConfig{
		GeneralRateLimit:  100,
		GeneralRateWindow: time.Millisecond, // Very short window.
		SSEMaxConcurrent:  10,
		MessageRateLimit:  100,
		MessageRateWindow: time.Millisecond,
	}
	rl := newRateLimiter(cfg)
	defer close(rl.stopCleanup)

	// Make a request to populate the bucket.
	rl.allowGeneral("test-key")
	rl.allowMessage("test-key")

	// Verify buckets exist.
	rl.mu.Lock()
	if len(rl.generalBuckets) == 0 {
		t.Fatal("expected general bucket to exist")
	}
	if len(rl.messageBuckets) == 0 {
		t.Fatal("expected message bucket to exist")
	}
	rl.mu.Unlock()

	// Wait for window to expire, then run cleanup.
	time.Sleep(5 * time.Millisecond)
	rl.cleanup()

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.generalBuckets) != 0 {
		t.Errorf("expected general buckets to be cleaned up, got %d", len(rl.generalBuckets))
	}
	if len(rl.messageBuckets) != 0 {
		t.Errorf("expected message buckets to be cleaned up, got %d", len(rl.messageBuckets))
	}
}
