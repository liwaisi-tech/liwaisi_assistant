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
	mux.HandleFunc("GET /api/v1/sessions", h.HandleListSessions)
	mux.HandleFunc("POST /api/v1/sessions", h.HandleCreateSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.HandleGetSession)
	mux.HandleFunc("PATCH /api/v1/sessions/{id}", h.HandleUpdateSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.HandleDeleteSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)
	mux.HandleFunc("GET /api/v1/sessions/{id}/execution", h.HandleGetSessionExecution)
	mux.HandleFunc("POST /api/v1/sessions/{id}/fork", h.HandleForkSession)
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages", h.HandleSendMessage)
	mux.HandleFunc("POST /api/v1/sessions/{id}/hitl/{transitionID}", h.HandleResolveHITL)

	// Flows
	mux.HandleFunc("GET /api/v1/flows", h.HandleListFlows)
	mux.HandleFunc("GET /api/v1/flows/{hash}", h.HandleGetFlow)
	mux.HandleFunc("GET /api/v1/flows/{hash}/executions", h.HandleGetFlowExecutions)

	// Personality
	mux.HandleFunc("GET /api/v1/personality", h.HandleGetPersonality)
	mux.HandleFunc("PATCH /api/v1/personality/principles/{kind}", h.HandleUpdatePrinciple)
	mux.HandleFunc("PUT /api/v1/personality/hierarchy", h.HandleSetHierarchy)
	mux.HandleFunc("DELETE /api/v1/personality", h.HandleResetPersonality)
	mux.HandleFunc("POST /api/v1/personality/preview", h.HandlePreviewPersonality)

	// Tools
	mux.HandleFunc("GET /api/v1/tools", h.HandleListTools)
	mux.HandleFunc("GET /api/v1/tools/{name...}", h.HandleGetTool)
}
