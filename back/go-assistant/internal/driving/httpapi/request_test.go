package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateSessionRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateSessionRequest
		wantErr string
	}{
		{
			name:    "valid web",
			req:     CreateSessionRequest{UserID: "u1", Channel: "web"},
			wantErr: "",
		},
		{
			name:    "valid whatsapp",
			req:     CreateSessionRequest{UserID: "u1", Channel: "whatsapp"},
			wantErr: "",
		},
		{
			name:    "valid telegram",
			req:     CreateSessionRequest{UserID: "u1", Channel: "telegram"},
			wantErr: "",
		},
		{
			name:    "missing user_id",
			req:     CreateSessionRequest{Channel: "web"},
			wantErr: "user_id is required",
		},
		{
			name:    "missing channel",
			req:     CreateSessionRequest{UserID: "u1"},
			wantErr: "channel is required",
		},
		{
			name:    "invalid channel",
			req:     CreateSessionRequest{UserID: "u1", Channel: "sms"},
			wantErr: "channel must be one of: web, whatsapp, telegram",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSendMessageRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     SendMessageRequest
		wantErr string
	}{
		{
			name:    "valid",
			req:     SendMessageRequest{Content: "hello"},
			wantErr: "",
		},
		{
			name:    "empty content",
			req:     SendMessageRequest{},
			wantErr: "content is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveHITLRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ResolveHITLRequest
		wantErr string
	}{
		{
			name:    "approve",
			req:     ResolveHITLRequest{Action: "approve"},
			wantErr: "",
		},
		{
			name:    "reject",
			req:     ResolveHITLRequest{Action: "reject"},
			wantErr: "",
		},
		{
			name:    "revise with content",
			req:     ResolveHITLRequest{Action: "revise", Content: "try again"},
			wantErr: "",
		},
		{
			name:    "invalid action",
			req:     ResolveHITLRequest{Action: "cancel"},
			wantErr: "action must be one of: approve, reject, revise",
		},
		{
			name:    "revise without content",
			req:     ResolveHITLRequest{Action: "revise"},
			wantErr: "content is required when action is revise",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid",
			body:    `{"content":"hello"}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			body:    `{invalid`,
			wantErr: true,
		},
		{
			name:    "unknown fields",
			body:    `{"content":"hello","extra":"field"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			var dst SendMessageRequest
			err := decodeJSON(w, req, &dst)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && dst.Content != "hello" {
				t.Errorf("content = %q, want %q", dst.Content, "hello")
			}
		})
	}

	t.Run("oversized body", func(t *testing.T) {
		// Create a body larger than maxRequestBodySize (1MB).
		big := bytes.Repeat([]byte("x"), maxRequestBodySize+1)
		body := append([]byte(`{"content":"`), big...)
		body = append(body, '"', '}')
		req, _ := http.NewRequest(http.MethodPost, "/", io.NopCloser(bytes.NewReader(body)))
		w := httptest.NewRecorder()
		var dst SendMessageRequest
		err := decodeJSON(w, req, &dst)
		if err == nil {
			t.Error("expected error for oversized body")
		}
	})
}
