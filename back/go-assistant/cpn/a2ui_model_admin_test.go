package cpn

// Table-driven tests for the locale-aware model-admin A2UI surface builders.
// Every test here enforces REQ-GAP-CPN-003 (surfaces match parent spec §4.6)
// and REQ-GAP-I18N-002/003 (localized literals swap en ↔ es cleanly).

import (
	"encoding/json"
	"strings"
	"testing"
)

// serializedSurface marshals a surface map and returns it prefixed with the
// A2UIMarker, exactly how the transition streams it on the wire. Every test
// asserts the marker prefix so a future refactor that drops the prefix would
// fail loudly (REQ-GAP-TEST-006).
func serializedSurface(t *testing.T, surface map[string]any) string {
	t.Helper()
	b, err := json.Marshal(surface)
	if err != nil {
		t.Fatalf("marshal surface: %v", err)
	}
	return A2UIMarker + string(b)
}

func TestLocalizedLiteral(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		locale  string
		wantSub string
	}{
		{"en_default", "models.list.title", "", "Registered models"},
		{"en_explicit", "models.list.title", "en", "Registered models"},
		{"es_prefix", "models.list.title", "es-CO", "Modelos registrados"},
		{"es_neutral", "models.list.title", "es", "Modelos registrados"},
		{"es_submit_form", "models.form.submit", "es-MX", "Registrar"},
		{"en_submit_form", "models.form.submit", "en-US", "Register"},
		{"unknown_key_returns_key", "models.nope.missing", "es", "models.nope.missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := localizedLiteral(tc.key, tc.locale)
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("localizedLiteral(%q, %q) = %q; want substring %q", tc.key, tc.locale, got, tc.wantSub)
			}
		})
	}
}

func TestBuildModelCardList(t *testing.T) {
	rows := []ModelCardListData{
		{ID: "google/gemma-4-31b-it", Name: "Gemma 4 31B", Provider: "google", Status: "default"},
		{ID: "anthropic/claude-opus-4-6", Name: "Claude Opus 4.6", Provider: "anthropic", Status: "active"},
	}
	cases := []struct {
		name      string
		locale    string
		wantTitle string
	}{
		{"en", "en", "Registered models"},
		{"es", "es", "Modelos registrados"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surface := BuildModelCardList(rows, tc.locale)
			// $$a2ui: marker on the wire.
			emitted := serializedSurface(t, surface)
			if !strings.HasPrefix(emitted, A2UIMarker) {
				t.Fatalf("missing A2UI marker prefix: %q", emitted[:min(30, len(emitted))])
			}
			// surfaceId follows the model-registry:list: prefix.
			sid, _ := surface["surfaceId"].(string)
			if !strings.HasPrefix(sid, "model-registry:list:") {
				t.Fatalf("surfaceId = %q, want prefix model-registry:list:", sid)
			}
			// header literal matches locale.
			comps := surface["components"].(map[string]any)
			header := comps["header"].(map[string]any)
			text := header["Text"].(map[string]any)["text"].(map[string]any)
			if lit, _ := text["literalString"].(string); lit != tc.wantTitle {
				t.Fatalf("header literal = %q, want %q", lit, tc.wantTitle)
			}
			// action buttons carry the expected semantic names.
			for _, wantID := range []string{"btn_inspect", "btn_toggle", "btn_setdefault", "btn_delete", "register_btn"} {
				if _, ok := comps[wantID]; !ok {
					t.Fatalf("missing component id %q", wantID)
				}
			}
		})
	}
}

func TestBuildRegisterModelForm(t *testing.T) {
	cases := []struct {
		name      string
		locale    string
		wantTitle string
		wantSubmit string
	}{
		{"en", "en", "Register a new model", "Register"},
		{"es", "es-CO", "Registrar un modelo nuevo", "Registrar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surface := BuildRegisterModelForm(tc.locale)
			emitted := serializedSurface(t, surface)
			if !strings.HasPrefix(emitted, A2UIMarker) {
				t.Fatal("expected $$a2ui: prefix")
			}
			sid, _ := surface["surfaceId"].(string)
			if !strings.HasPrefix(sid, "model-registry:register:") {
				t.Fatalf("surfaceId = %q, want prefix model-registry:register:", sid)
			}
			comps := surface["components"].(map[string]any)
			title := comps["title"].(map[string]any)["Text"].(map[string]any)["text"].(map[string]any)
			if lit, _ := title["literalString"].(string); lit != tc.wantTitle {
				t.Fatalf("title literal = %q, want %q", lit, tc.wantTitle)
			}
			submit := comps["txt_submit"].(map[string]any)["Text"].(map[string]any)["text"].(map[string]any)
			if lit, _ := submit["literalString"].(string); lit != tc.wantSubmit {
				t.Fatalf("txt_submit literal = %q, want %q", lit, tc.wantSubmit)
			}
			// All 7 TextFields + MultipleChoice + CheckBox + 2 Buttons present.
			for _, id := range []string{
				"fld_registry_id", "fld_vendor", "fld_family", "fld_version",
				"fld_display_name", "fld_hf_id", "fld_ctx",
				"adapter_pick", "fld_route_id", "chk_enrich",
				"btn_submit", "btn_cancel",
			} {
				if _, ok := comps[id]; !ok {
					t.Fatalf("missing component %q", id)
				}
			}
			// The submit action context carries all 10 fields per REQ-GAP-REG-004.
			btn := comps["btn_submit"].(map[string]any)["Button"].(map[string]any)
			action := btn["action"].(map[string]any)
			if name, _ := action["name"].(string); name != "submit_register" {
				t.Fatalf("btn_submit action name = %q, want submit_register", name)
			}
			ctx := action["context"].([]map[string]any)
			if len(ctx) != 10 {
				t.Fatalf("submit_register context len = %d, want 10", len(ctx))
			}
		})
	}
}

func TestBuildConfirmStateChange(t *testing.T) {
	cases := []struct {
		name      string
		op        string
		locale    string
		wantTitle string
	}{
		{"delete_en", "delete", "en", "Delete this model?"},
		{"delete_es", "delete", "es", "¿Eliminar este modelo?"},
		{"setdefault_es", "set-default", "es", "¿Promover este modelo a por defecto?"},
		{"license_en", "review-license", "en", "Apply this license decision?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surface := BuildConfirmStateChange(ConfirmStateChangeData{
				Op:         tc.op,
				RegistryID: "anthropic/claude-opus-4-6",
				Before:     map[string]any{"lifecycle": "active"},
				After:      map[string]any{"lifecycle": "removed"},
			}, tc.locale)
			emitted := serializedSurface(t, surface)
			if !strings.HasPrefix(emitted, A2UIMarker) {
				t.Fatal("missing $$a2ui: marker")
			}
			sid, _ := surface["surfaceId"].(string)
			if !strings.HasPrefix(sid, "model-registry:confirm:") {
				t.Fatalf("surfaceId = %q, want confirm: prefix", sid)
			}
			comps := surface["components"].(map[string]any)
			title := comps["title"].(map[string]any)["Text"].(map[string]any)["text"].(map[string]any)
			if lit, _ := title["literalString"].(string); lit != tc.wantTitle {
				t.Fatalf("title literal = %q, want %q", lit, tc.wantTitle)
			}
		})
	}
}

func TestBuildLicenseReviewCard(t *testing.T) {
	surface := BuildLicenseReviewCard(LicenseReviewCardData{
		RegistryID: "google/gemma-4-31b-it",
		Body:       "These are the license terms.",
		Status:     LicenseApprovedCommercial,
	}, "en")
	emitted := serializedSurface(t, surface)
	if !strings.HasPrefix(emitted, A2UIMarker) {
		t.Fatal("expected $$a2ui: prefix")
	}
	sid, _ := surface["surfaceId"].(string)
	if !strings.HasPrefix(sid, "model-registry:license:") {
		t.Fatalf("surfaceId = %q, want license: prefix", sid)
	}
	comps := surface["components"].(map[string]any)
	// Must carry both accept_chk and btn_approve.
	for _, id := range []string{"accept_chk", "btn_approve", "btn_cancel"} {
		if _, ok := comps[id]; !ok {
			t.Fatalf("missing component %q", id)
		}
	}
	// Approve button action name is accept_license.
	btn := comps["btn_approve"].(map[string]any)["Button"].(map[string]any)
	action := btn["action"].(map[string]any)
	if name, _ := action["name"].(string); name != "accept_license" {
		t.Fatalf("btn_approve action = %q, want accept_license", name)
	}
}

func TestBuildDefaultSelector(t *testing.T) {
	opts := []DefaultSelectorOption{
		{Value: "google/gemma-4-31b-it", Label: "Gemma 4 31B"},
		{Value: "anthropic/claude-opus-4-6", Label: "Claude Opus 4.6"},
	}
	surface := BuildDefaultSelector(opts, "google/gemma-4-31b-it", "es")
	emitted := serializedSurface(t, surface)
	if !strings.HasPrefix(emitted, A2UIMarker) {
		t.Fatal("missing A2UI marker")
	}
	sid, _ := surface["surfaceId"].(string)
	if !strings.HasPrefix(sid, "model-registry:default:") {
		t.Fatalf("surfaceId = %q, want default: prefix", sid)
	}
	comps := surface["components"].(map[string]any)
	title := comps["title"].(map[string]any)["Text"].(map[string]any)["text"].(map[string]any)
	if lit, _ := title["literalString"].(string); !strings.Contains(lit, "por defecto") {
		t.Fatalf("title = %q, expected Spanish 'por defecto'", lit)
	}
	picker := comps["picker"].(map[string]any)["MultipleChoice"].(map[string]any)
	if got, _ := picker["maxAllowedSelections"].(int); got != 1 {
		t.Fatalf("maxAllowedSelections = %v, want 1", picker["maxAllowedSelections"])
	}
	if got, _ := picker["filterable"].(bool); !got {
		t.Fatalf("picker filterable = %v, want true", picker["filterable"])
	}
}

func TestBuildReplayLockPayload(t *testing.T) {
	surface := BuildReplayLockPayload("model-registry:list:stale-id", "en")
	emitted := serializedSurface(t, surface)
	if !strings.HasPrefix(emitted, A2UIMarker) {
		t.Fatal("missing A2UI marker")
	}
	sid, _ := surface["surfaceId"].(string)
	if !strings.HasPrefix(sid, "model-registry:replay:") {
		t.Fatalf("surfaceId = %q, want replay: prefix", sid)
	}
	// deleteSurfaceIds carries the stale id so the frontend can tear down the
	// prior interactive surface (c1e1396 / d3590bf pattern).
	deletions, _ := surface["deleteSurfaceIds"].([]string)
	if len(deletions) != 1 || deletions[0] != "model-registry:list:stale-id" {
		t.Fatalf("deleteSurfaceIds = %v, want [model-registry:list:stale-id]", deletions)
	}
	// The replay button has no `action` — inert on re-hydration.
	comps := surface["components"].(map[string]any)
	hint := comps["hint"].(map[string]any)["Button"].(map[string]any)
	if _, hasAction := hint["action"]; hasAction {
		t.Fatal("hint Button must not carry an action (stale-surface lock)")
	}
}

func TestAdminOnlyMessageLocales(t *testing.T) {
	if got := AdminOnlyMessage("en"); !strings.Contains(got, "admins only") {
		t.Fatalf("en admin-only = %q, want 'admins only' substring", got)
	}
	if got := AdminOnlyMessage("es-CO"); !strings.Contains(got, "administradores") {
		t.Fatalf("es admin-only = %q, want 'administradores' substring", got)
	}
}

func TestValidationCapMessageLocales(t *testing.T) {
	if got := ValidationCapMessage("en"); !strings.Contains(got, "3 attempts") {
		t.Fatalf("en cap = %q, want '3 attempts'", got)
	}
	if got := ValidationCapMessage("es"); !strings.Contains(got, "3 intentos") {
		t.Fatalf("es cap = %q, want '3 intentos'", got)
	}
}

func TestLocaleFromConsumed(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"with_locale", `{"intent":"manage-models","locale":"es-CO"}`, "es-CO"},
		{"no_locale", `{"intent":"manage-models"}`, ""},
		{"non_string_payload", 42, ""},
		{"bad_json", `{broken`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := localeFromConsumed([]Token{{Payload: tc.payload}})
			if got != tc.want {
				t.Fatalf("locale = %q, want %q", got, tc.want)
			}
		})
	}
}

