package httpapi

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// SessionResponse is the JSON response for session operations.
type SessionResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Channel   string `json:"channel"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// SessionDetailResponse includes messages in addition to session info.
type SessionDetailResponse struct {
	SessionResponse
	Messages []MessageResponse `json:"messages"`
}

// MessageResponse is a single message in the session history.
type MessageResponse struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CPNID     string `json:"cpn_id,omitempty"`
	Timestamp string `json:"timestamp"`
}

// StatusResponse is a simple status response.
type StatusResponse struct {
	Status string `json:"status"`
}

// HealthResponse is the health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// VersionResponse is the version info response.
type VersionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

// ErrorResponse is the error response format.
type ErrorResponse struct {
	Error string `json:"error"`
}

// BalanceResponse is the billing balance response.
type BalanceResponse struct {
	LimitRemaining *float64 `json:"limit_remaining"`
	Usage          float64  `json:"usage"`
	UsageDaily     float64  `json:"usage_daily"`
	UsageWeekly    float64  `json:"usage_weekly"`
	UsageMonthly   float64  `json:"usage_monthly"`
	IsFreeTier     bool     `json:"is_free_tier"`
}
