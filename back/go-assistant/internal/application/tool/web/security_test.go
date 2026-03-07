package web

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr string
	}{
		{
			name:   "valid http URL",
			rawURL: "http://example.com/page",
		},
		{
			name:   "valid https URL",
			rawURL: "https://example.com/page?q=1",
		},
		{
			name:   "valid https with port",
			rawURL: "https://example.com:8080/path",
		},
		{
			name:    "empty URL",
			rawURL:  "",
			wantErr: "URL is required",
		},
		{
			name:    "no scheme",
			rawURL:  "example.com/page",
			wantErr: "has no scheme",
		},
		{
			name:    "file scheme",
			rawURL:  "file:///etc/passwd",
			wantErr: "unsupported scheme",
		},
		{
			name:    "ftp scheme",
			rawURL:  "ftp://files.example.com/data",
			wantErr: "unsupported scheme",
		},
		{
			name:    "data scheme",
			rawURL:  "data:text/html,<h1>hi</h1>",
			wantErr: "unsupported scheme",
		},
		{
			name:    "javascript scheme",
			rawURL:  "javascript:alert(1)",
			wantErr: "unsupported scheme",
		},
		{
			name:    "http with no host",
			rawURL:  "http://",
			wantErr: "has no host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := validateURL(tt.rawURL)
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
			if u == nil {
				t.Fatal("expected non-nil URL")
			}
			if u.String() != tt.rawURL {
				t.Errorf("URL mismatch: got %q, want %q", u.String(), tt.rawURL)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// 0.0.0.0/8 range
		{name: "0.0.0.0 (0/8)", ip: "0.0.0.0", private: true},
		{name: "0.255.255.255 (0/8)", ip: "0.255.255.255", private: true},

		// IPv4 private ranges
		{name: "10.0.0.1 (10/8)", ip: "10.0.0.1", private: true},
		{name: "10.255.255.255 (10/8)", ip: "10.255.255.255", private: true},
		{name: "172.16.0.1 (172.16/12)", ip: "172.16.0.1", private: true},
		{name: "172.31.255.255 (172.16/12)", ip: "172.31.255.255", private: true},
		{name: "172.15.0.1 (not private)", ip: "172.15.0.1", private: false},
		{name: "172.32.0.1 (not private)", ip: "172.32.0.1", private: false},
		{name: "192.168.0.1 (192.168/16)", ip: "192.168.0.1", private: true},
		{name: "192.168.255.255 (192.168/16)", ip: "192.168.255.255", private: true},
		{name: "127.0.0.1 (loopback)", ip: "127.0.0.1", private: true},
		{name: "127.255.255.255 (loopback)", ip: "127.255.255.255", private: true},
		{name: "169.254.1.1 (link-local)", ip: "169.254.1.1", private: true},

		// IPv6
		{name: "::1 (loopback)", ip: "::1", private: true},
		{name: "fe80::1 (link-local)", ip: "fe80::1", private: true},
		{name: "fc00::1 (ULA)", ip: "fc00::1", private: true},
		{name: "fd00::1 (ULA)", ip: "fd00::1", private: true},

		// Public IPs
		{name: "8.8.8.8 (public)", ip: "8.8.8.8", private: false},
		{name: "1.1.1.1 (public)", ip: "1.1.1.1", private: false},
		{name: "93.184.216.34 (public)", ip: "93.184.216.34", private: false},
		{name: "2606:4700::1 (public IPv6)", ip: "2606:4700::1", private: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestNewSafeTransport(t *testing.T) {
	transport := NewSafeTransport(5 * time.Second)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.DialContext == nil {
		t.Error("expected custom DialContext to be set")
	}
}

func TestNewSafeTransport_BlocksPrivateIPs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: NewSafeTransport(5 * time.Second),
		Timeout:   5 * time.Second,
	}

	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error connecting to loopback via safe transport, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("expected error about private/loopback, got: %v", err)
	}
}
