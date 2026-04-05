package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// RateLimitConfig configures rate limits per endpoint category.
type RateLimitConfig struct {
	SSEMaxConcurrent  int
	MessageRateLimit  int
	MessageRateWindow time.Duration
	GeneralRateLimit  int
	GeneralRateWindow time.Duration
}

// DefaultRateLimitConfig returns sensible defaults.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		SSEMaxConcurrent:  3,
		MessageRateLimit:  10,
		MessageRateWindow: time.Minute,
		GeneralRateLimit:  60,
		GeneralRateWindow: time.Minute,
	}
}

// bucket tracks request timestamps for sliding-window rate limiting.
type bucket struct {
	timestamps []time.Time
}

// allow checks whether a new request is allowed under the given limit and window.
// It prunes expired timestamps and appends the current one if allowed.
func (b *bucket) allow(limit int, window time.Duration, now time.Time) bool {
	cutoff := now.Add(-window)
	// Prune expired entries.
	valid := b.timestamps[:0]
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	b.timestamps = valid

	if len(b.timestamps) >= limit {
		return false
	}
	b.timestamps = append(b.timestamps, now)
	return true
}

// oldest returns the oldest timestamp in the bucket, used for Retry-After calculation.
func (b *bucket) oldest() time.Time {
	if len(b.timestamps) == 0 {
		return time.Time{}
	}
	return b.timestamps[0]
}

// rateLimiter manages per-user rate limiting state.
type rateLimiter struct {
	mu             sync.Mutex
	messageBuckets map[string]*bucket
	generalBuckets map[string]*bucket
	sseConns       map[string]int
	cfg            RateLimitConfig
	stopCleanup    chan struct{}
}

// newRateLimiter creates a rate limiter and starts a background cleanup goroutine.
func newRateLimiter(cfg RateLimitConfig) *rateLimiter {
	rl := &rateLimiter{
		messageBuckets: make(map[string]*bucket),
		generalBuckets: make(map[string]*bucket),
		sseConns:       make(map[string]int),
		cfg:            cfg,
		stopCleanup:    make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically removes expired bucket entries every 5 minutes.
func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// cleanup removes empty buckets and zero-connection SSE entries.
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, b := range rl.messageBuckets {
		cutoff := now.Add(-rl.cfg.MessageRateWindow)
		valid := b.timestamps[:0]
		for _, ts := range b.timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		b.timestamps = valid
		if len(b.timestamps) == 0 {
			delete(rl.messageBuckets, key)
		}
	}
	for key, b := range rl.generalBuckets {
		cutoff := now.Add(-rl.cfg.GeneralRateWindow)
		valid := b.timestamps[:0]
		for _, ts := range b.timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		b.timestamps = valid
		if len(b.timestamps) == 0 {
			delete(rl.generalBuckets, key)
		}
	}
	for key, count := range rl.sseConns {
		if count <= 0 {
			delete(rl.sseConns, key)
		}
	}
}

// allowMessage checks whether a message request is allowed for the given key.
func (rl *rateLimiter) allowMessage(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.messageBuckets[key]
	if !ok {
		b = &bucket{}
		rl.messageBuckets[key] = b
	}
	now := time.Now()
	if b.allow(rl.cfg.MessageRateLimit, rl.cfg.MessageRateWindow, now) {
		return true, 0
	}
	retryAfter := b.oldest().Add(rl.cfg.MessageRateWindow).Sub(now)
	return false, retryAfter
}

// allowGeneral checks whether a general request is allowed for the given key.
func (rl *rateLimiter) allowGeneral(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.generalBuckets[key]
	if !ok {
		b = &bucket{}
		rl.generalBuckets[key] = b
	}
	now := time.Now()
	if b.allow(rl.cfg.GeneralRateLimit, rl.cfg.GeneralRateWindow, now) {
		return true, 0
	}
	retryAfter := b.oldest().Add(rl.cfg.GeneralRateWindow).Sub(now)
	return false, retryAfter
}

// acquireSSE attempts to acquire an SSE connection slot. Returns false if at limit.
func (rl *rateLimiter) acquireSSE(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.sseConns[key] >= rl.cfg.SSEMaxConcurrent {
		return false
	}
	rl.sseConns[key]++
	return true
}

// releaseSSE releases an SSE connection slot.
func (rl *rateLimiter) releaseSSE(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.sseConns[key] > 0 {
		rl.sseConns[key]--
	}
}

// rateLimitPublicPaths are routes that bypass rate limiting.
var rateLimitPublicPaths = map[string]bool{
	"/api/v1/health":  true,
	"/api/v1/version": true,
}

// rateLimitMiddleware returns HTTP middleware that enforces rate limits.
func rateLimitMiddleware(cfg RateLimitConfig) func(http.Handler) http.Handler {
	rl := newRateLimiter(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass public routes.
			if rateLimitPublicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Extract user key from auth context, fallback to IP.
			key := userKeyFromRequest(r)

			// SSE endpoints: check concurrent connection limit.
			if strings.Contains(r.URL.Path, "/events") {
				if !rl.acquireSSE(key) {
					writeTooManyRequests(w, 10*time.Second) // suggest retry after 10s for SSE
					return
				}
				// Release on connection close.
				done := r.Context().Done()
				go func() {
					<-done
					rl.releaseSSE(key)
				}()
				next.ServeHTTP(w, r)
				return
			}

			// Message endpoints: POST to paths containing /messages.
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/messages") {
				allowed, retryAfter := rl.allowMessage(key)
				if !allowed {
					writeTooManyRequests(w, retryAfter)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// General rate limit for all other endpoints.
			allowed, retryAfter := rl.allowGeneral(key)
			if !allowed {
				writeTooManyRequests(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userKeyFromRequest extracts a rate-limit key from the request.
// Uses auth user Sub if available, otherwise falls back to client IP.
func userKeyFromRequest(r *http.Request) string {
	if user := auth.UserFromContext(r.Context()); user != nil && user.Sub != "" {
		return "user:" + user.Sub
	}
	// Fallback to IP for dev mode or unauthenticated requests.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return "ip:" + ip
}

// writeTooManyRequests writes a 429 response with Retry-After header.
func writeTooManyRequests(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strings.TrimRight(strings.TrimRight(retryAfter.Truncate(time.Second).String(), "0"), "."))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":         "rate limit exceeded",
		"retry_after_s": seconds,
	})
}
