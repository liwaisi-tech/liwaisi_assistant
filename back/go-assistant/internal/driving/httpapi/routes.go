package httpapi

import "net/http"

// RegisterRoutes registers all API routes on the given ServeMux.
// Uses Go 1.22+ enhanced routing with method patterns and path variables.
func RegisterRoutes(mux *http.ServeMux, h *Handlers) {
	// Health & info
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	mux.HandleFunc("GET /api/v1/version", handleVersion)

	// Billing
	mux.HandleFunc("GET /api/v1/billing/balance", h.HandleGetBalance)

	// Sessions
	mux.HandleFunc("POST /api/v1/sessions", h.HandleCreateSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.HandleGetSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages", h.HandleSendMessage)
	mux.HandleFunc("POST /api/v1/sessions/{id}/hitl/{transitionID}", h.HandleResolveHITL)
}
