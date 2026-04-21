package httpapi

// GAP-8 Skill Manifest HTTP handlers.
//
// Spec: spec-architecture-skill-manifest.md §3 REQ-010, REQ-011.
//
// Both endpoints are wired behind AdminMiddleware in routes.go.
// The full JSON manifest is returned — the Markdown compact form
// is for LLM prompt injection only and is not exposed via HTTP.

import (
	"encoding/json"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// HandleAdminListSkills serves GET /api/v1/admin/skills.
// Query params: kind=builtin|flow|tool|host_capability, origin=, deprecated=true|false|all.
func (h *Handlers) HandleAdminListSkills(w http.ResponseWriter, r *http.Request) {
	if h.SkillManifest == nil {
		http.Error(w, `{"error":"skill manifest not configured"}`, http.StatusServiceUnavailable)
		return
	}

	m, err := h.SkillManifest.Build(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to build skill manifest"}`, http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	kindFilter := q.Get("kind")
	originFilter := q.Get("origin")
	deprecatedFilter := q.Get("deprecated") // "true" | "false" | "all" (default: "false")

	if deprecatedFilter == "" {
		deprecatedFilter = "false"
	}

	filtered := make([]app.Skill, 0, len(m.Skills))
	for _, sk := range m.Skills {
		if kindFilter != "" && string(sk.Kind) != kindFilter {
			continue
		}
		if originFilter != "" && sk.Origin != originFilter {
			continue
		}
		switch deprecatedFilter {
		case "true":
			if !sk.Deprecated {
				continue
			}
		case "false":
			if sk.Deprecated {
				continue
			}
			// "all" passes everything through
		}
		filtered = append(filtered, sk)
	}

	if filtered == nil {
		filtered = []app.Skill{}
	}

	resp := struct {
		BuiltAt string      `json:"built_at"`
		Skills  []app.Skill `json:"skills"`
		Total   int         `json:"total"`
	}{
		BuiltAt: m.BuiltAt.UTC().Format("2006-01-02T15:04:05Z"),
		Skills:  filtered,
		Total:   len(filtered),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleAdminGetSkill serves GET /api/v1/admin/skills/{id}.
func (h *Handlers) HandleAdminGetSkill(w http.ResponseWriter, r *http.Request) {
	if h.SkillManifest == nil {
		http.Error(w, `{"error":"skill manifest not configured"}`, http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"missing skill id"}`, http.StatusBadRequest)
		return
	}

	m, err := h.SkillManifest.Build(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to build skill manifest"}`, http.StatusInternalServerError)
		return
	}

	for _, sk := range m.Skills {
		if sk.ID == id {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sk)
			return
		}
	}

	http.Error(w, `{"error":"skill not found"}`, http.StatusNotFound)
}
