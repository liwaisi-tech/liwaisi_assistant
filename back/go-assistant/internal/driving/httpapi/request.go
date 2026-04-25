package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// maxRequestBodySize is the maximum allowed request body size (1MB).
const maxRequestBodySize = 1 << 20

// CreateSessionRequest is the request body for POST /api/v1/sessions.
type CreateSessionRequest struct {
	UserID  string `json:"user_id"`
	Channel string `json:"channel"`
	// FlowHash optionally binds the session to a non-default CPN
	// (e.g. tool-creator). When empty the server builds the default unified
	// topology. The hash must exist in the flows table AND in the server's
	// FlowBuilders map for the resolution to succeed.
	FlowHash string `json:"flow_hash,omitempty"`
}

// Validate checks that required fields are present.
func (r *CreateSessionRequest) Validate() error {
	if r.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if r.Channel == "" {
		return fmt.Errorf("channel is required")
	}
	switch r.Channel {
	case "web", "whatsapp", "telegram":
		// valid
	default:
		return fmt.Errorf("channel must be one of: web, whatsapp, telegram")
	}
	return nil
}

// SendMessageRequest is the request body for POST /api/v1/sessions/{id}/messages.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// Validate checks that required fields are present.
func (r *SendMessageRequest) Validate() error {
	if r.Content == "" {
		return fmt.Errorf("content is required")
	}
	return nil
}

// ResolveHITLRequest is the request body for POST /api/v1/sessions/{id}/hitl/{transitionID}.
type ResolveHITLRequest struct {
	Action  string `json:"action"`
	Content string `json:"content,omitempty"`
}

// Validate checks that required fields are present.
func (r *ResolveHITLRequest) Validate() error {
	switch r.Action {
	case "approve", "reject", "revise", "submit":
		// valid
	default:
		return fmt.Errorf("action must be one of: approve, reject, revise, submit")
	}
	if r.Action == "revise" && r.Content == "" {
		return fmt.Errorf("content is required when action is revise")
	}
	if r.Action == "submit" && r.Content == "" {
		return fmt.Errorf("content is required when action is submit")
	}
	return nil
}

// decodeJSON reads and decodes a JSON request body with size limiting.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}
