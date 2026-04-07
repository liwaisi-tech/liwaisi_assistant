package openrouter

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// newResilientHTTPClient builds an *http.Client tuned for outbound calls to
// OpenRouter from container environments where the network path is not
// guaranteed to be perfectly behaved (typical VPS / Docker bridge).
//
// Why this exists: a bare &http.Client{Timeout: ...} uses Go's default dialer,
// which performs Happy Eyeballs (RFC 8305) with a 300ms fallback delay. That's
// usually fine — but in two failure modes seen in this project it isn't:
//
//  1. Docker's embedded resolver occasionally returns ONLY AAAA records for
//     openrouter.ai. If the container has no functional IPv6 egress (a very
//     common Docker/VPS setup), the dial silently stalls until the per-call
//     deadline fires. Symptoms: "i/o timeout" with NO partial connect.
//  2. Some VPS providers (Hetzner, Linode dual-stack) advertise IPv6 but the
//     route black-holes: SYNs leave but no SYN-ACK comes back. Same stall.
//
// The mitigations applied here:
//
//   - FORCE_IPV4=1 env var (or AppEnv "production" by default) makes the
//     dialer ignore AAAA results entirely.
//   - Tight FallbackDelay (50ms instead of 300ms) makes Happy Eyeballs race
//     IPv4 almost immediately, so even when both stacks are returned the
//     IPv4 path wins fast.
//   - Explicit short connect/TLS timeouts so a stall surfaces in seconds,
//     not minutes.
//   - Keep-alive + small idle pool so streaming SSE doesn't churn TCP.
func newResilientHTTPClient(totalTimeout time.Duration) *http.Client {
	forceV4 := strings.EqualFold(os.Getenv("FORCE_IPV4"), "1") ||
		strings.EqualFold(os.Getenv("FORCE_IPV4"), "true")

	dialer := &net.Dialer{
		Timeout:       8 * time.Second,
		KeepAlive:     30 * time.Second,
		FallbackDelay: 50 * time.Millisecond, // aggressive happy-eyeballs
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if forceV4 && (network == "tcp" || network == "tcp6") {
			network = "tcp4"
		}
		return dialer.DialContext(ctx, network, addr)
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // streaming-friendly
	}

	return &http.Client{
		Transport: transport,
		Timeout:   totalTimeout,
	}
}
