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

	// User profile & onboarding
	mux.HandleFunc("GET /api/v1/user/profile", h.HandleGetProfile)
	mux.HandleFunc("PUT /api/v1/user/preferences", h.HandleUpdatePreferences)
	mux.HandleFunc("POST /api/v1/user/onboarding/complete", h.HandleCompleteOnboarding)

	// Models
	mux.HandleFunc("GET /api/v1/models", h.HandleGetModels)

	// Admin config (admin-only, except status which is public)
	if h.AdminEmail != "" {
		adminAuth := AdminMiddleware(h.AdminEmail)
		mux.Handle("GET /api/v1/admin/config", adminAuth(http.HandlerFunc(h.HandleListConfig)))
		mux.Handle("PUT /api/v1/admin/config/{key}", adminAuth(http.HandlerFunc(h.HandleSetConfig)))
		mux.Handle("DELETE /api/v1/admin/config/{key}", adminAuth(http.HandlerFunc(h.HandleDeleteConfig)))
	}
	mux.HandleFunc("GET /api/v1/admin/config/status", h.HandleConfigStatus)

	// Waitlist (public, no auth required)
	mux.HandleFunc("POST /api/v1/waitlist", h.HandleWaitlist)

	// Dev-only: A2UI test endpoint — injects A2UI content through real SSE pipeline
	mux.HandleFunc("POST /api/v1/sessions/{id}/test-a2ui", h.HandleTestA2UI)
}
