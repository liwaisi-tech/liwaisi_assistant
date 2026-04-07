package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// HandleTestA2UI injects A2UI-formatted stream chunks into an active session,
// exercising the full SSE pipeline: broker → SSE → useChat → MessageBubble → A2UIRenderer.
//
// DEV-ONLY: This handler should NOT be registered in production.
//
// POST /api/v1/sessions/{id}/test-a2ui
//
// Optional query param: ?demo=form|card|progress|dashboard (default: dashboard)
func (h *Handlers) HandleTestA2UI(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	demo := r.URL.Query().Get("demo")
	if demo == "" {
		demo = "dashboard"
	}

	payload := buildA2UIDemo(demo)

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal A2UI payload")
		return
	}

	content := "$$a2ui:" + string(payloadJSON)

	// Send the A2UI content as a stream chunk through the real SSE broker.
	// First send a "thinking" chunk, then the A2UI content, then done.
	h.Broker.PublishStreamChunk(sessionID, cpn.StreamChunk{
		SessionID: sessionID,
		CPNID:     "a2ui-test",
		CPNRole:   "a2ui-demo",
		Content:   content,
		Done:      false,
	})

	// Small delay so the frontend accumulates the chunk before done signal.
	time.Sleep(100 * time.Millisecond)

	h.Broker.PublishStreamChunk(sessionID, cpn.StreamChunk{
		SessionID: sessionID,
		CPNID:     "a2ui-test",
		CPNRole:   "a2ui-demo",
		Content:   "",
		Done:      true,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"demo":   demo,
		"info":   "A2UI test payload injected into session SSE stream",
	})
}

// a2uiComponent is a simple struct for building test A2UI payloads.
type a2uiComponent struct {
	Type     string            `json:"type"`
	Props    map[string]any    `json:"props,omitempty"`
	Children []a2uiComponent   `json:"children,omitempty"`
}

type a2uiPayload struct {
	Components []a2uiComponent        `json:"components"`
	Data       map[string]any         `json:"data,omitempty"`
}

func buildA2UIDemo(demo string) a2uiPayload {
	switch demo {
	case "form":
		return a2uiPayload{
			Components: []a2uiComponent{
				{Type: "card", Props: map[string]any{"title": "HITL Review"}, Children: []a2uiComponent{
					{Type: "text", Props: map[string]any{"content": "The agent has prepared a plan. Please review and approve or request changes."}},
					{Type: "divider"},
					{Type: "form", Props: map[string]any{"id": "review-form"}, Children: []a2uiComponent{
						{Type: "text", Props: map[string]any{"content": "**Plan**: Build a REST API with Go using hexagonal architecture, PostgreSQL for persistence, and Redis for caching."}},
						{Type: "divider"},
						{Type: "button", Props: map[string]any{"label": "Approve Plan", "variant": "primary", "actionType": "approve"}},
						{Type: "button", Props: map[string]any{"label": "Reject", "variant": "danger", "actionType": "reject"}},
					}},
				}},
			},
		}

	case "card":
		return a2uiPayload{
			Components: []a2uiComponent{
				{Type: "text", Props: map[string]any{"content": "Here are the results of my analysis:"}},
				{Type: "card", Props: map[string]any{"title": "Architecture Analysis"}, Children: []a2uiComponent{
					{Type: "badge", Props: map[string]any{"label": "CPN Engine", "variant": "info"}},
					{Type: "badge", Props: map[string]any{"label": "Hexagonal", "variant": "info"}},
					{Type: "text", Props: map[string]any{"content": "The codebase follows a clean hexagonal architecture with a Coloured Petri Net execution engine at its core."}},
					{Type: "list", Props: map[string]any{
						"items": []string{
							"Domain layer: cpn/ — zero external deps",
							"Application layer: internal/app/ — SessionService orchestrator",
							"Driving adapters: httpapi/ (REST+SSE), a2a/ (JSON-RPC)",
							"Driven adapters: openrouter/, googleauth/, billing/",
						},
					}},
				}},
				{Type: "alert", Props: map[string]any{"variant": "info", "message": "All 155 tests passing. No critical issues found."}},
			},
		}

	case "progress":
		return a2uiPayload{
			Components: []a2uiComponent{
				{Type: "card", Props: map[string]any{"title": "Task Execution"}, Children: []a2uiComponent{
					{Type: "text", Props: map[string]any{"content": "Building REST API endpoints..."}},
					{Type: "progress", Props: map[string]any{"value": 0.65, "label": "Step 3 of 5"}},
					{Type: "divider"},
					{Type: "list", Props: map[string]any{
						"items": []string{
							"✓ Database schema created",
							"✓ Repository layer implemented",
							"◉ HTTP handlers in progress...",
							"○ Middleware setup",
							"○ Integration tests",
						},
					}},
				}},
			},
		}

	case "dashboard":
		return a2uiPayload{
			Components: []a2uiComponent{
				{Type: "text", Props: map[string]any{"content": "## Session Dashboard"}},
				{Type: "card", Props: map[string]any{"title": "CPN Execution Status"}, Children: []a2uiComponent{
					{Type: "badge", Props: map[string]any{"label": "RUNNING", "variant": "info"}},
					{Type: "progress", Props: map[string]any{"value": 0.42, "label": "Execution 42% complete"}},
				}},
				{Type: "card", Props: map[string]any{"title": "Agent Metrics"}, Children: []a2uiComponent{
					{Type: "text", Props: map[string]any{"content": "**Tokens used**: 12,847 input / 3,291 output"}},
					{Type: "text", Props: map[string]any{"content": "**Cost**: $0.0234 USD"}},
					{Type: "text", Props: map[string]any{"content": "**Transitions fired**: 7"}},
				}},
				{Type: "alert", Props: map[string]any{"variant": "warn", "message": "HITL gate pending — human review required for plan approval."}},
				{Type: "card", Props: map[string]any{"title": "Quick Actions"}, Children: []a2uiComponent{
					{Type: "button", Props: map[string]any{"label": "Approve Plan", "variant": "primary", "actionType": "approve"}},
					{Type: "button", Props: map[string]any{"label": "View Topology", "variant": "secondary", "actionType": "view-topology"}},
					{Type: "button", Props: map[string]any{"label": "Cancel Execution", "variant": "danger", "actionType": "cancel"}},
				}},
			},
		}

	default:
		return a2uiPayload{
			Components: []a2uiComponent{
				{Type: "text", Props: map[string]any{"content": "A2UI test: unknown demo '" + demo + "'"}},
			},
		}
	}
}
