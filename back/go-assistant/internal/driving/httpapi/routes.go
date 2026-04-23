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
	mux.HandleFunc("POST /api/v1/sessions/{id}/tool-approvals", h.HandleToolApprovalDecision)

	// Flows
	mux.HandleFunc("GET /api/v1/flows", h.HandleListFlows)
	mux.HandleFunc("POST /api/v1/flows", h.HandleCreateFlow)
	mux.HandleFunc("GET /api/v1/flows/{hash}", h.HandleGetFlow)
	mux.HandleFunc("GET /api/v1/flows/{hash}/executions", h.HandleGetFlowExecutions)
	mux.HandleFunc("POST /api/v1/flows/{hash}/run", h.HandleRunFlow)

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
	if len(h.AdminEmails) > 0 {
		adminAuth := AdminMiddleware(h.AdminEmails)
		mux.Handle("GET /api/v1/admin/config", adminAuth(http.HandlerFunc(h.HandleListConfig)))
		mux.Handle("PUT /api/v1/admin/config/{key}", adminAuth(http.HandlerFunc(h.HandleSetConfig)))
		mux.Handle("DELETE /api/v1/admin/config/{key}", adminAuth(http.HandlerFunc(h.HandleDeleteConfig)))

		// Model registry admin CRUD (workstream B3).
		// Action endpoints use literal paths that win over the {registryID...}
		// wildcard because Go 1.22+ ServeMux prefers more-specific matches.
		mux.Handle("GET /api/v1/admin/models", adminAuth(http.HandlerFunc(h.HandleAdminListModels)))
		mux.Handle("POST /api/v1/admin/models", adminAuth(http.HandlerFunc(h.HandleAdminRegisterModel)))
		mux.Handle("POST /api/v1/admin/models/set-default", adminAuth(http.HandlerFunc(h.HandleAdminSetDefault)))
		mux.Handle("POST /api/v1/admin/models/license-review", adminAuth(http.HandlerFunc(h.HandleAdminLicenseReview)))
		mux.Handle("GET /api/v1/admin/models/{registryID...}", adminAuth(http.HandlerFunc(h.HandleAdminGetModel)))
		mux.Handle("PATCH /api/v1/admin/models/{registryID...}", adminAuth(http.HandlerFunc(h.HandleAdminUpdateModel)))
		mux.Handle("DELETE /api/v1/admin/models/{registryID...}", adminAuth(http.HandlerFunc(h.HandleAdminDeleteModel)))

		// Tool registry admin REST (GAP-3). Action endpoint uses a body
		// param because Go 1.22+ ServeMux only allows the {name...}
		// wildcard at the end of the path pattern.
		mux.Handle("GET /api/v1/admin/tools", adminAuth(http.HandlerFunc(h.HandleAdminListTools)))
		mux.Handle("GET /api/v1/admin/toolboxes", adminAuth(http.HandlerFunc(h.HandleAdminListToolboxes)))
		mux.Handle("POST /api/v1/admin/tools/deprecate", adminAuth(http.HandlerFunc(h.HandleAdminDeprecateTool)))
		mux.Handle("GET /api/v1/admin/tools/{qn...}", adminAuth(http.HandlerFunc(h.HandleAdminGetTool)))
		mux.Handle("DELETE /api/v1/admin/tools/{qn...}", adminAuth(http.HandlerFunc(h.HandleAdminDeleteTool)))

		// GAP-6 host gate admin endpoints.
		mux.Handle("GET /api/v1/admin/host/policy", adminAuth(http.HandlerFunc(h.HandleHostPolicyGet)))
		mux.Handle("PUT /api/v1/admin/host/policy", adminAuth(http.HandlerFunc(h.HandleHostPolicyPut)))
		mux.Handle("GET /api/v1/admin/host/gate-decisions", adminAuth(http.HandlerFunc(h.HandleGateDecisionsList)))
		mux.Handle("GET /api/v1/admin/host/first-run-ledger", adminAuth(http.HandlerFunc(h.HandleFirstRunList)))
		mux.Handle("POST /api/v1/admin/host/first-run-ledger/{sha}/revoke", adminAuth(http.HandlerFunc(h.HandleFirstRunRevoke)))

		// GAP-2 host capability admin endpoints.
		mux.Handle("GET /api/v1/admin/host/capabilities", adminAuth(http.HandlerFunc(h.HandleGetHostCapabilities)))
		mux.Handle("POST /api/v1/admin/host/capabilities/rediscover", adminAuth(http.HandlerFunc(h.HandlePostHostCapabilitiesRediscover)))

		// GAP-10 authored-artefact admin endpoints. Action endpoints use
		// literal paths so they win over the {id} wildcard (Go 1.22+
		// ServeMux prefers more-specific matches).
		mux.Handle("GET /api/v1/admin/artefacts", adminAuth(http.HandlerFunc(h.HandleAdminListArtefacts)))
		mux.Handle("POST /api/v1/admin/artefacts/purge", adminAuth(http.HandlerFunc(h.HandleAdminPurgeArtefacts)))
		mux.Handle("POST /api/v1/admin/artefacts/sets/{set_id}/rollback", adminAuth(http.HandlerFunc(h.HandleAdminRollbackArtefactSet)))
		mux.Handle("POST /api/v1/admin/artefacts/sets/{set_id}/restore", adminAuth(http.HandlerFunc(h.HandleAdminRestoreArtefactSet)))
		mux.Handle("GET /api/v1/admin/artefacts/{id}", adminAuth(http.HandlerFunc(h.HandleAdminGetArtefact)))

		// GAP-4 agent-authored flow admin endpoints. The {id}/reject
		// literal path wins over the {id} wildcard by spec.
		mux.Handle("GET /api/v1/admin/flows", adminAuth(http.HandlerFunc(h.HandleAdminListAuthoredFlows)))
		mux.Handle("POST /api/v1/admin/flows/{id}/reject", adminAuth(http.HandlerFunc(h.HandleAdminRejectAuthoredFlow)))
		mux.Handle("GET /api/v1/admin/flows/{id}", adminAuth(http.HandlerFunc(h.HandleAdminGetAuthoredFlow)))

		// GAP-8 skill manifest admin endpoints.
		mux.Handle("GET /api/v1/admin/skills", adminAuth(http.HandlerFunc(h.HandleAdminListSkills)))
		mux.Handle("GET /api/v1/admin/skills/{id}", adminAuth(http.HandlerFunc(h.HandleAdminGetSkill)))
	}
	mux.HandleFunc("GET /api/v1/admin/config/status", h.HandleConfigStatus)

	// Waitlist (public, no auth required)
	mux.HandleFunc("POST /api/v1/waitlist", h.HandleWaitlist)

	// Dev-only: A2UI test endpoint — injects A2UI content through real SSE pipeline
	mux.HandleFunc("POST /api/v1/sessions/{id}/test-a2ui", h.HandleTestA2UI)
}
