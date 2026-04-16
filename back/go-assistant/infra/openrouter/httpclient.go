package openrouter

import (
	"context"
	"crypto/tls"
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
//   - HTTP/1.1 PINNED (no HTTP/2). Go's net/http HTTP/2 transport buffers
//     SSE frames behind flow-control windows, so streaming deltas arrive in
//     big clumps or only at end-of-stream instead of token-by-token. HTTP/1.1
//     chunked transfer-encoding flushes each event immediately, which is
//     what the UI actually needs. Setting TLSNextProto to an empty non-nil
//     map disables the automatic h2 upgrade Go performs on TLS connections.
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
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       dialContext,
		ForceAttemptHTTP2: false,
		// Empty (non-nil) TLSNextProto disables Go's automatic HTTP/2
		// upgrade on TLS connections. We want HTTP/1.1 + chunked for SSE.
		TLSNextProto: map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
		// DisableCompression=true stops the transport from advertising
		// Accept-Encoding: gzip. Go's automatic gzip handling wraps the
		// response body in a gzip.Reader whose DEFLATE window (~16 KB) must
		// fill before any decompressed bytes reach bufio.Scanner — which
		// means short SSE replies sit inside the decoder until EOF and then
		// appear to the UI all at once. For streaming to be token-by-token
		// the transport MUST speak identity encoding on this hop. The cost
		// is a few % extra egress, negligible vs. broken streaming UX.
		DisableCompression:    true,
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
