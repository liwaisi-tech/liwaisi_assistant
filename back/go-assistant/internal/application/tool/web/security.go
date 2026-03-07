// Package web provides web content fetching tools for the agent.
package web

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// privateRanges contains all private, loopback, and link-local IP ranges
// that must be blocked to prevent SSRF attacks.
var privateRanges []net.IPNet //nolint:gochecknoglobals // static lookup table

func init() { //nolint:gochecknoinits // one-time CIDR parse for SSRF protection
	cidrs := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fe80::/10",
		"fc00::/7",
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		privateRanges = append(privateRanges, *ipNet)
	}
}

// validateURL parses rawURL and ensures it uses an allowed scheme (http/https)
// and has a non-empty host. Returns the parsed URL or an error with a
// descriptive message suitable for LLM self-correction.
func validateURL(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("URL is required — provide a fully-qualified URL starting with http:// or https://")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("malformed URL %q: %w", rawURL, err)
	}

	switch u.Scheme {
	case "http", "https":
	case "":
		return nil, fmt.Errorf("URL %q has no scheme — it must start with http:// or https://", rawURL)
	default:
		return nil, fmt.Errorf("unsupported scheme %q — only http:// and https:// are allowed", u.Scheme)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("URL %q has no host — provide a valid hostname", rawURL)
	}

	return u, nil
}

// isPrivateIP reports whether ip falls within a private, loopback,
// or link-local range.
func isPrivateIP(ip net.IP) bool {
	for i := range privateRanges {
		if privateRanges[i].Contains(ip) {
			return true
		}
	}
	return false
}

// NewSafeTransport creates an *http.Transport with SSRF protection.
// The custom DialContext resolves DNS and rejects connections to
// private/loopback/link-local IP addresses before the TCP handshake.
func NewSafeTransport(timeout time.Duration) *http.Transport {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("invalid address %q: %w", address, err)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("cannot parse IP from resolved address %q", host)
			}
			if isPrivateIP(ip) {
				return fmt.Errorf("connections to private/loopback addresses are not allowed (resolved to %s)", ip)
			}
			return nil
		},
	}

	return &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
	}
}
