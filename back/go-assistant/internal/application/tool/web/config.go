package web

import "time"

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxBodySize      = 5 * 1024 * 1024 // 5MB
	defaultMaxContentLength = 100000          // ~25k tokens
	defaultUserAgent        = "liwaisi-agent/1.0 (+https://github.com/liwaisi-tech/liwaisi_assistant)"
	defaultAccept           = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

// Config holds web fetch pipeline settings.
type Config struct {
	Timeout          time.Duration
	MaxBodySize      int64
	MaxContentLength int
	UserAgent        string
	Accept           string
}

// DefaultConfig returns a Config with production-ready defaults.
func DefaultConfig() *Config {
	return &Config{
		Timeout:          defaultTimeout,
		MaxBodySize:      defaultMaxBodySize,
		MaxContentLength: defaultMaxContentLength,
		UserAgent:        defaultUserAgent,
		Accept:           defaultAccept,
	}
}

// Option configures a Config.
type Option func(*Config)

// WithTimeout overrides the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) { c.Timeout = d }
}

// WithMaxBodySize overrides the maximum HTTP response body size in bytes.
func WithMaxBodySize(n int64) Option {
	return func(c *Config) { c.MaxBodySize = n }
}

// WithMaxContentLength overrides the maximum markdown content length in characters.
func WithMaxContentLength(n int) Option {
	return func(c *Config) { c.MaxContentLength = n }
}

// WithUserAgent overrides the User-Agent header sent with requests.
func WithUserAgent(ua string) Option {
	return func(c *Config) { c.UserAgent = ua }
}
