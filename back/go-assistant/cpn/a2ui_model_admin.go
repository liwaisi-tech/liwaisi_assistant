package cpn

// Locale-aware A2UI surface builders for the manage-models-flow topology.
//
// Spec: spec/spec-architecture-model-registry-and-a2ui-management.md §4.6
// (REQ-A2UI-001..006) — authoritative component trees.
// Spec: spec/spec-process-model-admin-in-chat-gap-closure.md GAP-CPN
// (REQ-GAP-CPN-003, REQ-GAP-I18N-003) — no new catalog entries; backend MUST
// localize surface literals via a `locale` hint pulled from the session.
//
// Component trees mirror §4.6 byte-for-byte except for user-facing literals,
// which are routed through localizedLiteral(key, locale) so Spanish and English
// sessions each get native copy. The JSON keys and primitive types ("Card",
// "Column", "List", "Button", "TextField", "MultipleChoice", "CheckBox",
// "Modal", "Text", "Row") are v0.8 A2UI primitives already wired in
// front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx — no new
// extensions are introduced here (REQ-A2UI-002).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Locale + literal resolution ─────────────────────────────────────────────

// localizedLiteral returns the user-facing string for the given semantic key
// in the requested locale. The CPN builder chooses `"es"` when the session's
// RegionalVariant starts with `"es"` (matches `"es"`, `"es-CO"`, `"es-MX"`,
// etc.), `"en"` otherwise. Unknown keys fall back to the English copy to keep
// surfaces renderable even if a translation is missing — a missing literal
// would break A2UI parsing, so we never emit an empty string.
func localizedLiteral(key, locale string) string {
	table := literalsEN
	if strings.HasPrefix(strings.ToLower(locale), "es") {
		table = literalsES
	}
	if v, ok := table[key]; ok {
		return v
	}
	if v, ok := literalsEN[key]; ok {
		return v
	}
	return key
}

// literalsEN / literalsES are the authoritative backend i18n table for the
// manage-models surfaces. Keys match the namespacing used in
// front/react-assistant/src/i18n/locales/{en,es}/chat.json so the two layers
// stay in visual lockstep even though the backend emits the literals directly
// on the wire (§4.6 design).
var literalsEN = map[string]string{
	// ModelCardList
	"models.list.title":               "Registered models",
	"models.list.filter.label":        "Search",
	"models.list.actions.set_default": "Set default",
	"models.list.actions.inspect":     "Details",
	"models.list.actions.toggle":      "Enable/Disable",
	"models.list.actions.delete":      "Delete",
	"models.list.actions.register":    "Register a new model",
	// RegisterModelForm
	"models.form.title":               "Register a new model",
	"models.form.intro":               "Fill in the canonical information. The license will be reviewed separately.",
	"models.form.fields.registry_id":  "registry_id (vendor/family-version)",
	"models.form.fields.vendor":       "vendor",
	"models.form.fields.family":       "family",
	"models.form.fields.version":      "version",
	"models.form.fields.display_name": "display_name",
	"models.form.fields.hf_id":        "hugging_face_id (optional)",
	"models.form.fields.context":      "context_length (tokens)",
	"models.form.fields.adapter":      "primary route adapter",
	"models.form.fields.route_id":     "provider_model_id",
	"models.form.fields.enrich":       "Enrich license from Hugging Face (if hugging_face_id set)",
	"models.form.submit":              "Register",
	"models.form.cancel":              "Cancel",
	"models.form.adapter.openrouter":  "OpenRouter",
	"models.form.adapter.anthropic":   "Anthropic direct",
	"models.form.adapter.openai":      "OpenAI direct",
	"models.form.adapter.google":      "Google Gemini",
	"models.form.adapter.selfhosted":  "Self-hosted",
	// ConfirmStateChange
	"models.confirm.entry":            "Confirm",
	"models.confirm.title_delete":     "Delete this model?",
	"models.confirm.title_setdefault": "Promote this model to default?",
	"models.confirm.title_license":    "Apply this license decision?",
	"models.confirm.title_toggle":     "Change this model's state?",
	"models.confirm.title_register":   "Register this model?",
	"models.confirm.before_label":     "Before",
	"models.confirm.after_label":      "After",
	"models.confirm.irreversible":     "This action cannot be undone.",
	"models.confirm.accept":           "Confirm",
	"models.confirm.cancel":           "Cancel",
	// LicenseReviewCard
	"models.license.title":            "License review",
	"models.license.body_placeholder": "Read the license terms carefully before approving.",
	"models.license.accept_terms":     "I've read and accept the terms",
	"models.license.approve":          "Approve license",
	"models.license.cancel":           "Cancel",
	// DefaultSelector
	"models.default.title":  "Choose the default model",
	"models.default.submit": "Set as default",
	"models.default.cancel": "Cancel",
	// Replay (frozen after apply)
	"models.replay.title":         "Action applied",
	"models.replay.body":          "This surface is no longer interactive.",
	"models.replay.disabled_hint": "Completed",
	// Errors / admin gate
	"models.errors.admin_only":     "Model management is available to admins only.",
	"models.errors.validation_cap": "I couldn't validate the form after 3 attempts. You can resume whenever you like.",
}

var literalsES = map[string]string{
	// ModelCardList
	"models.list.title":               "Modelos registrados",
	"models.list.filter.label":        "Buscar",
	"models.list.actions.set_default": "Por defecto",
	"models.list.actions.inspect":     "Detalles",
	"models.list.actions.toggle":      "Activar/Desactivar",
	"models.list.actions.delete":      "Eliminar",
	"models.list.actions.register":    "Registrar un modelo nuevo",
	// RegisterModelForm
	"models.form.title":               "Registrar un modelo nuevo",
	"models.form.intro":               "Completa la información canónica. La licencia se revisará por separado.",
	"models.form.fields.registry_id":  "registry_id (vendor/family-version)",
	"models.form.fields.vendor":       "vendor",
	"models.form.fields.family":       "family",
	"models.form.fields.version":      "version",
	"models.form.fields.display_name": "display_name",
	"models.form.fields.hf_id":        "hugging_face_id (opcional)",
	"models.form.fields.context":      "context_length (tokens)",
	"models.form.fields.adapter":      "adaptador de ruta principal",
	"models.form.fields.route_id":     "provider_model_id",
	"models.form.fields.enrich":       "Enriquecer licencia desde Hugging Face (si hugging_face_id está presente)",
	"models.form.submit":              "Registrar",
	"models.form.cancel":              "Cancelar",
	"models.form.adapter.openrouter":  "OpenRouter",
	"models.form.adapter.anthropic":   "Anthropic directo",
	"models.form.adapter.openai":      "OpenAI directo",
	"models.form.adapter.google":      "Google Gemini",
	"models.form.adapter.selfhosted":  "Self-hosted",
	// ConfirmStateChange
	"models.confirm.entry":            "Confirmar",
	"models.confirm.title_delete":     "¿Eliminar este modelo?",
	"models.confirm.title_setdefault": "¿Promover este modelo a por defecto?",
	"models.confirm.title_license":    "¿Aplicar esta decisión de licencia?",
	"models.confirm.title_toggle":     "¿Cambiar el estado de este modelo?",
	"models.confirm.title_register":   "¿Registrar este modelo?",
	"models.confirm.before_label":     "Antes",
	"models.confirm.after_label":      "Después",
	"models.confirm.irreversible":     "Esta acción no se puede deshacer.",
	"models.confirm.accept":           "Confirmar",
	"models.confirm.cancel":           "Cancelar",
	// LicenseReviewCard
	"models.license.title":            "Revisión de licencia",
	"models.license.body_placeholder": "Lee los términos con cuidado antes de aprobar.",
	"models.license.accept_terms":     "He leído y acepto los términos",
	"models.license.approve":          "Aprobar licencia",
	"models.license.cancel":           "Cancelar",
	// DefaultSelector
	"models.default.title":  "Elige el modelo por defecto",
	"models.default.submit": "Fijar como por defecto",
	"models.default.cancel": "Cancelar",
	// Replay (frozen after apply)
	"models.replay.title":         "Acción aplicada",
	"models.replay.body":          "Esta superficie ya no es interactiva.",
	"models.replay.disabled_hint": "Completado",
	// Errors / admin gate
	"models.errors.admin_only":     "La gestión de modelos está disponible sólo para administradores.",
	"models.errors.validation_cap": "No pude validar el formulario después de 3 intentos. Puedes retomarlo cuando quieras.",
}

// ── Surface id helpers ──────────────────────────────────────────────────────

// newSurfaceID returns a stable-but-unique surface id of the form
// `model-registry:<kind>:<RFC3339-safe timestamp>-<short random>`. It mirrors
// the literals shown in parent spec §4.6 (e.g. "model-registry:list:2026-04-17T12-00-00Z-7a").
// The tests assert the `model-registry:<kind>:` prefix, so the suffix format
// is free to evolve.
func newSurfaceID(kind string) string {
	ts := time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
	return fmt.Sprintf("model-registry:%s:%s", kind, ts)
}

// NewSurfaceIDForTest exports newSurfaceID for the cmd/server topology test —
// the helper is package-private inside cpn but the test needs to assert its
// uniqueness guarantee. Not intended for production use.
func NewSurfaceIDForTest(kind string) string { return newSurfaceID(kind) }

// ── ModelCardList ───────────────────────────────────────────────────────────

// ModelCardListData is the slim view-model consumed by BuildModelCardList.
// Keep this tight — the component tree reads every field via path-binding and
// the frontend ignores anything extra, so adding fields here without wiring
// them into the tree is silent dead weight.
type ModelCardListData struct {
	ID       string   `json:"id"`       // registry_id
	Name     string   `json:"name"`     // display_name
	Provider string   `json:"provider"` // vendor
	Status   string   `json:"status"`   // "default" | "active" | "registered" | …
	PriceIn  string   `json:"price_in"`
	PriceOut string   `json:"price_out"`
	Ctx      string   `json:"ctx"`
	Caps     []string `json:"caps"`
}

// BuildModelCardList renders the §4.6.1 component tree populated with `models`.
// The admin clicks map to `inspect_model | toggle_model | set_default | delete_model`
// userActions that the `t-recv-response` transition consumes.
func BuildModelCardList(models []ModelCardListData, locale string) map[string]any {
	return map[string]any{
		"type":      "a2ui.v08",
		"surfaceId": newSurfaceID("list"),
		"catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"dataModel": map[string]any{
			"models": models,
			"filter": "",
		},
		"root": "root",
		"components": map[string]any{
			"root": map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"header", "filter", "list_models", "register_btn"}}}},
			"header": map[string]any{"Text": map[string]any{
				"text":      map[string]any{"literalString": localizedLiteral("models.list.title", locale)},
				"usageHint": "h2",
			}},
			"filter": map[string]any{"TextField": map[string]any{
				"label":         localizedLiteral("models.list.filter.label", locale),
				"text":          map[string]any{"path": "/filter"},
				"textFieldType": "shortText",
			}},
			"list_models": map[string]any{"List": map[string]any{
				"direction": "vertical",
				"children":  map[string]any{"template": map[string]any{"dataBinding": "/models", "componentId": "model_row"}},
			}},
			"model_row":      map[string]any{"Card": map[string]any{"child": "model_row_body"}},
			"model_row_body": map[string]any{"Row": map[string]any{"distribution": "spaceBetween", "alignment": "start", "children": map[string]any{"explicitList": []string{"model_meta", "model_actions"}}}},
			"model_meta":     map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"model_title", "model_sub", "model_caps"}}}},
			"model_title":    map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/name"}, "usageHint": "h3"}},
			"model_sub":      map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/provider"}, "usageHint": "caption"}},
			"model_caps":     map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/caps"}, "usageHint": "caption"}},
			"model_actions":  map[string]any{"Row": map[string]any{"distribution": "end", "children": map[string]any{"explicitList": []string{"btn_inspect", "btn_toggle", "btn_setdefault", "btn_delete"}}}},
			"btn_inspect":    buttonWithContext("txt_inspect", "inspect_model"),
			"btn_toggle":     buttonWithContext("txt_toggle", "toggle_model"),
			"btn_setdefault": buttonWithContext("txt_setdefault", "set_default"),
			"btn_delete":     buttonWithContext("txt_delete", "delete_model"),
			"txt_inspect":    map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.list.actions.inspect", locale)}}},
			"txt_toggle":     map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.list.actions.toggle", locale)}}},
			"txt_setdefault": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.list.actions.set_default", locale)}}},
			"txt_delete":     map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.list.actions.delete", locale)}}},
			"register_btn":   buttonNoContext("txt_register", "register_new", true),
			"txt_register":   map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.list.actions.register", locale)}}},
		},
	}
}

// ── RegisterModelForm ───────────────────────────────────────────────────────

// BuildRegisterModelForm renders the §4.6.2 form with localized labels.
// Field paths and action name (`submit_register`) are identical across locales
// so the frontend renderer, server-side validator, and t-validate guard can
// all bind to the same JSON paths regardless of display language.
func BuildRegisterModelForm(locale string) map[string]any {
	return map[string]any{
		"type":      "a2ui.v08",
		"surfaceId": newSurfaceID("register"),
		"catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"dataModel": map[string]any{
			"form": map[string]any{
				"registry_id":            "",
				"vendor":                 "",
				"family":                 "",
				"version":                "",
				"display_name":           "",
				"hugging_face_id":        "",
				"context_length":         0,
				"primary_route_adapter":  "openrouter",
				"primary_route_model_id": "",
				"enable_enrichment":      true,
			},
		},
		"root": "root",
		"components": map[string]any{
			"root": map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{
				"title", "intro", "fld_registry_id", "fld_vendor", "fld_family", "fld_version", "fld_display_name",
				"fld_hf_id", "fld_ctx", "adapter_pick", "fld_route_id", "chk_enrich", "submit_row",
			}}}},
			"title":            map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.form.title", locale)}, "usageHint": "h2"}},
			"intro":            map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.form.intro", locale)}, "usageHint": "body"}},
			"fld_registry_id":  textField(localizedLiteral("models.form.fields.registry_id", locale), "/form/registry_id", "shortText", `^[a-z0-9][a-z0-9._-]*\/[a-z0-9][a-z0-9._-]*$`),
			"fld_vendor":       textField(localizedLiteral("models.form.fields.vendor", locale), "/form/vendor", "shortText", ""),
			"fld_family":       textField(localizedLiteral("models.form.fields.family", locale), "/form/family", "shortText", ""),
			"fld_version":      textField(localizedLiteral("models.form.fields.version", locale), "/form/version", "shortText", ""),
			"fld_display_name": textField(localizedLiteral("models.form.fields.display_name", locale), "/form/display_name", "shortText", ""),
			"fld_hf_id":        textField(localizedLiteral("models.form.fields.hf_id", locale), "/form/hugging_face_id", "shortText", ""),
			"fld_ctx":          textField(localizedLiteral("models.form.fields.context", locale), "/form/context_length", "number", ""),
			"adapter_pick": map[string]any{"MultipleChoice": map[string]any{
				"selections":           map[string]any{"path": "/form/primary_route_adapter"},
				"maxAllowedSelections": 1,
				"filterable":           false,
				"options": []map[string]any{
					{"label": localizedLiteral("models.form.adapter.openrouter", locale), "value": "openrouter"},
					{"label": localizedLiteral("models.form.adapter.anthropic", locale), "value": "anthropic"},
					{"label": localizedLiteral("models.form.adapter.openai", locale), "value": "openai"},
					{"label": localizedLiteral("models.form.adapter.google", locale), "value": "google-gemini"},
					{"label": localizedLiteral("models.form.adapter.selfhosted", locale), "value": "self-hosted"},
				},
			}},
			"fld_route_id": textField(localizedLiteral("models.form.fields.route_id", locale), "/form/primary_route_model_id", "shortText", ""),
			"chk_enrich":   map[string]any{"CheckBox": map[string]any{"label": localizedLiteral("models.form.fields.enrich", locale), "value": map[string]any{"path": "/form/enable_enrichment"}}},
			"submit_row":   map[string]any{"Row": map[string]any{"distribution": "end", "children": map[string]any{"explicitList": []string{"btn_cancel", "btn_submit"}}}},
			"btn_submit": map[string]any{"Button": map[string]any{
				"child":   "txt_submit",
				"primary": true,
				"action": map[string]any{
					"name": "submit_register",
					"context": []map[string]any{
						{"key": "registry_id", "value": map[string]any{"path": "/form/registry_id"}},
						{"key": "vendor", "value": map[string]any{"path": "/form/vendor"}},
						{"key": "family", "value": map[string]any{"path": "/form/family"}},
						{"key": "version", "value": map[string]any{"path": "/form/version"}},
						{"key": "display_name", "value": map[string]any{"path": "/form/display_name"}},
						{"key": "hugging_face_id", "value": map[string]any{"path": "/form/hugging_face_id"}},
						{"key": "context_length", "value": map[string]any{"path": "/form/context_length"}},
						{"key": "primary_route_adapter", "value": map[string]any{"path": "/form/primary_route_adapter"}},
						{"key": "primary_route_model_id", "value": map[string]any{"path": "/form/primary_route_model_id"}},
						{"key": "enable_enrichment", "value": map[string]any{"path": "/form/enable_enrichment"}},
					},
				},
			}},
			"btn_cancel": buttonNoContextName("txt_cancel", "cancel_register", false),
			"txt_submit": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.form.submit", locale)}}},
			"txt_cancel": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.form.cancel", locale)}}},
		},
	}
}

// ── ConfirmStateChange ──────────────────────────────────────────────────────

// ConfirmStateChangeData packs the diff the user will see before approving a
// mutation. `op` ∈ {register, toggle, set-default, review-license, delete}.
type ConfirmStateChangeData struct {
	Op         string         `json:"op"`
	RegistryID string         `json:"registry_id"`
	Before     map[string]any `json:"before"`
	After      map[string]any `json:"after"`
}

// BuildConfirmStateChange renders §4.6.4 using the op-specific title from
// literalsEN/ES. The "before/after" columns render whatever scalar fields the
// caller populated; the frontend reads them via path bindings.
func BuildConfirmStateChange(d ConfirmStateChangeData, locale string) map[string]any {
	titleKey := "models.confirm.title_setdefault"
	confirmLabelKey := "models.confirm.accept"
	switch d.Op {
	case "delete":
		titleKey = "models.confirm.title_delete"
		confirmLabelKey = "models.list.actions.delete"
	case "review-license":
		titleKey = "models.confirm.title_license"
	case "toggle":
		titleKey = "models.confirm.title_toggle"
	case "register":
		titleKey = "models.confirm.title_register"
	}

	// Pick a before/after field to display — prefer lifecycle; fall back to the
	// first key. Keeps the component tree static (parent spec §4.6.4) while
	// letting the caller drive the payload shape.
	beforeField, afterField := "lifecycle", "lifecycle"
	for k := range d.Before {
		beforeField = k
		break
	}
	for k := range d.After {
		afterField = k
		break
	}

	return map[string]any{
		"type":      "a2ui.v08",
		"surfaceId": newSurfaceID("confirm"),
		"catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"dataModel": map[string]any{
			"op":          d.Op,
			"registry_id": d.RegistryID,
			"before":      d.Before,
			"after":       d.After,
		},
		"root": "modal",
		"components": map[string]any{
			"modal":       map[string]any{"Modal": map[string]any{"entryPointChild": "btn_entry", "contentChild": "panel"}},
			"btn_entry":   buttonNoContext("txt_entry", "open_confirm", false),
			"txt_entry":   map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.confirm.entry", locale)}}},
			"panel":       map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"title", "diff_row", "notice", "actions"}}}},
			"title":       map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral(titleKey, locale)}, "usageHint": "h3"}},
			"diff_row":    map[string]any{"Row": map[string]any{"distribution": "spaceBetween", "children": map[string]any{"explicitList": []string{"before_col", "after_col"}}}},
			"before_col":  map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"before_lbl", "before_val"}}}},
			"after_col":   map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"after_lbl", "after_val"}}}},
			"before_lbl":  map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.confirm.before_label", locale)}, "usageHint": "caption"}},
			"before_val":  map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/before/" + beforeField}}},
			"after_lbl":   map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.confirm.after_label", locale)}, "usageHint": "caption"}},
			"after_val":   map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/after/" + afterField}}},
			"notice":      map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.confirm.irreversible", locale)}, "usageHint": "caption"}},
			"actions":     map[string]any{"Row": map[string]any{"distribution": "end", "children": map[string]any{"explicitList": []string{"btn_cancel", "btn_confirm"}}}},
			"btn_cancel":  confirmButton("txt_cancel", "cancel_confirm", false),
			"btn_confirm": confirmButton("txt_confirm", "apply_confirm", true),
			"txt_cancel":  map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.confirm.cancel", locale)}}},
			"txt_confirm": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral(confirmLabelKey, locale)}}},
		},
	}
}

// ── LicenseReviewCard ───────────────────────────────────────────────────────

// LicenseReviewCardData is the minimum the surface needs to render a review
// screen. `Status` is one of the cpn.License* constants — parent spec §4
// license-state machine.
type LicenseReviewCardData struct {
	RegistryID string `json:"registry_id"`
	Body       string `json:"body"`   // license text
	Status     string `json:"status"` // target status on approve
}

// BuildLicenseReviewCard renders §4.6.3 — mirrors the HF gated-model consent
// pattern. The Modal's contentChild shows the license body in a scrollable
// Text, plus a CheckBox "I've read and accept the terms" and an Approve
// Button. The action name is `accept_license` so the t-validate guard can
// dispatch by name.
func BuildLicenseReviewCard(d LicenseReviewCardData, locale string) map[string]any {
	body := d.Body
	if strings.TrimSpace(body) == "" {
		body = localizedLiteral("models.license.body_placeholder", locale)
	}
	return map[string]any{
		"type":      "a2ui.v08",
		"surfaceId": newSurfaceID("license"),
		"catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"dataModel": map[string]any{
			"registry_id": d.RegistryID,
			"body":        body,
			"status":      d.Status,
			"accepted":    false,
		},
		"root": "modal",
		"components": map[string]any{
			"modal":      map[string]any{"Modal": map[string]any{"entryPointChild": "btn_entry", "contentChild": "panel"}},
			"btn_entry":  buttonNoContext("txt_entry", "open_license", false),
			"txt_entry":  map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.license.title", locale)}}},
			"panel":      map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"title", "body_txt", "accept_chk", "actions"}}}},
			"title":      map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.license.title", locale)}, "usageHint": "h3"}},
			"body_txt":   map[string]any{"Text": map[string]any{"text": map[string]any{"path": "/body"}, "usageHint": "body"}},
			"accept_chk": map[string]any{"CheckBox": map[string]any{"label": localizedLiteral("models.license.accept_terms", locale), "value": map[string]any{"path": "/accepted"}}},
			"actions":    map[string]any{"Row": map[string]any{"distribution": "end", "children": map[string]any{"explicitList": []string{"btn_cancel", "btn_approve"}}}},
			"btn_cancel": confirmButton("txt_cancel", "cancel_license", false),
			"btn_approve": map[string]any{"Button": map[string]any{
				"child":   "txt_approve",
				"primary": true,
				"action": map[string]any{
					"name": "accept_license",
					"context": []map[string]any{
						{"key": "registry_id", "value": map[string]any{"path": "/registry_id"}},
						{"key": "license_status", "value": map[string]any{"path": "/status"}},
						{"key": "accepted", "value": map[string]any{"path": "/accepted"}},
					},
				},
			}},
			"txt_cancel":  map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.license.cancel", locale)}}},
			"txt_approve": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.license.approve", locale)}}},
		},
	}
}

// ── DefaultSelector ─────────────────────────────────────────────────────────

// DefaultSelectorOption is one option in the default-picker dropdown.
type DefaultSelectorOption struct {
	Value string `json:"value"` // registry_id
	Label string `json:"label"` // display_name
}

// BuildDefaultSelector renders §4.6.5 — a single MultipleChoice with
// `maxAllowedSelections: 1`, `filterable: true`, bound to `/selected_default`,
// plus a submit button that emits `submit_set_default`.
func BuildDefaultSelector(options []DefaultSelectorOption, current string, locale string) map[string]any {
	opts := make([]map[string]any, 0, len(options))
	for _, o := range options {
		opts = append(opts, map[string]any{"label": o.Label, "value": o.Value})
	}
	return map[string]any{
		"type":      "a2ui.v08",
		"surfaceId": newSurfaceID("default"),
		"catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"dataModel": map[string]any{
			"selected_default": current,
		},
		"root": "root",
		"components": map[string]any{
			"root":       map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"title", "picker", "submit_row"}}}},
			"title":      map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.default.title", locale)}, "usageHint": "h3"}},
			"picker":     map[string]any{"MultipleChoice": map[string]any{"selections": map[string]any{"path": "/selected_default"}, "maxAllowedSelections": 1, "filterable": true, "options": opts}},
			"submit_row": map[string]any{"Row": map[string]any{"distribution": "end", "children": map[string]any{"explicitList": []string{"btn_cancel", "btn_submit"}}}},
			"btn_submit": map[string]any{"Button": map[string]any{
				"child":   "txt_submit",
				"primary": true,
				"action": map[string]any{
					"name":    "submit_set_default",
					"context": []map[string]any{{"key": "registry_id", "value": map[string]any{"path": "/selected_default"}}},
				},
			}},
			"btn_cancel": buttonNoContextName("txt_cancel", "cancel_set_default", false),
			"txt_submit": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.default.submit", locale)}}},
			"txt_cancel": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.default.cancel", locale)}}},
		},
	}
}

// ── Replay / stale-surface lock (t-lock-replay) ─────────────────────────────

// BuildReplayLockPayload returns the inert Card that the t-lock-replay
// transition emits AFTER the mutation has been applied. Buttons have no
// `action` wiring so re-hydration on session reload leaves them disabled —
// this is the server-side half of the stale-surface guard landed in commits
// c1e1396 and d3590bf on the frontend.
//
// staleSurfaceID is the id of the original interactive surface; the client
// uses it to emit a `deleteSurface` before rendering this replay card,
// tearing down any live bubble that shared the id.
func BuildReplayLockPayload(staleSurfaceID, locale string) map[string]any {
	return map[string]any{
		"type":             "a2ui.v08",
		"surfaceId":        newSurfaceID("replay"),
		"catalogId":        "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
		"staleSurfaceId":   staleSurfaceID,
		"deleteSurfaceIds": []string{staleSurfaceID},
		"dataModel":        map[string]any{},
		"root":             "root",
		"components": map[string]any{
			"root":  map[string]any{"Card": map[string]any{"child": "body"}},
			"body":  map[string]any{"Column": map[string]any{"children": map[string]any{"explicitList": []string{"title", "msg", "hint"}}}},
			"title": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.replay.title", locale)}, "usageHint": "h3"}},
			"msg":   map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.replay.body", locale)}, "usageHint": "body"}},
			// hint is a Button with no `action` — renders disabled per
			// A2UIMessageRenderer's fallback rule. Mirrors commits c1e1396 +
			// d3590bf frozen-replay pattern.
			"hint":     map[string]any{"Button": map[string]any{"child": "hint_txt"}},
			"hint_txt": map[string]any{"Text": map[string]any{"text": map[string]any{"literalString": localizedLiteral("models.replay.disabled_hint", locale)}}},
		},
	}
}

// ── Admin-only denial reply ─────────────────────────────────────────────────

// AdminOnlyMessage returns the localized plain-text assistant reply emitted
// when a non-admin user hits the manage-models entry. No A2UI surface is
// emitted — REQ-GAP-CPN-007.
func AdminOnlyMessage(locale string) string {
	return localizedLiteral("models.errors.admin_only", locale)
}

// ValidationCapMessage returns the localized "I gave up after 3 rounds"
// summary emitted when t-reask exhausts its loop cap (REQ-GAP-CPN-006).
func ValidationCapMessage(locale string) string {
	return localizedLiteral("models.errors.validation_cap", locale)
}

// ── Small builder helpers ────────────────────────────────────────────────────

func textField(label, path, kind, regex string) map[string]any {
	tf := map[string]any{
		"label":         label,
		"text":          map[string]any{"path": path},
		"textFieldType": kind,
	}
	if regex != "" {
		tf["validationRegexp"] = regex
	}
	return map[string]any{"TextField": tf}
}

func buttonWithContext(childID, actionName string) map[string]any {
	btn := map[string]any{
		"child": childID,
		"action": map[string]any{
			"name":    actionName,
			"context": []map[string]any{{"key": "registry_id", "value": map[string]any{"path": "/id"}}},
		},
	}
	return map[string]any{"Button": btn}
}

func buttonNoContext(childID, actionName string, primary bool) map[string]any {
	btn := map[string]any{
		"child":  childID,
		"action": map[string]any{"name": actionName, "context": []map[string]any{}},
	}
	if primary {
		btn["primary"] = true
	}
	return map[string]any{"Button": btn}
}

// buttonNoContextName is an alias that keeps the `btn_cancel` callsites
// visually symmetric with buttonWithContext without confusing the reader.
func buttonNoContextName(childID, actionName string, primary bool) map[string]any {
	return buttonNoContext(childID, actionName, primary)
}

// confirmButton is a ConfirmStateChange-specific helper that wires both `op`
// and `registry_id` into the action context so t-apply/t-validate can route
// by op without re-decoding the surface.
func confirmButton(childID, actionName string, primary bool) map[string]any {
	btn := map[string]any{
		"child": childID,
		"action": map[string]any{
			"name": actionName,
			"context": []map[string]any{
				{"key": "op", "value": map[string]any{"path": "/op"}},
				{"key": "registry_id", "value": map[string]any{"path": "/registry_id"}},
			},
		},
	}
	if primary {
		btn["primary"] = true
	}
	return map[string]any{"Button": btn}
}

// ── A2UIPayloadBuilder adapters ─────────────────────────────────────────────

// These builders adapt the BuildXxx functions above to the
// HITLConfig.A2UIPayloadBuilder signature so topologies_model_registry.go can
// plug them into the manage-models-flow transitions without duplicating glue
// code.
//
// Each builder reads the locale from its first string-payload consumed token
// (a best-effort hint written by the classifier or session layer) and falls
// back to "en". The management-flow topology always provides a
// C_MgmtState token in `consumed` carrying `{"locale":"..."}` so the
// literal-selection stays deterministic.

// localeFromConsumed best-effort extracts a BCP-47 tag from the first
// JSON-payload consumed token. Tolerates arbitrary envelope shapes: any
// top-level `locale` string field wins. Empty / invalid input yields `""`.
func localeFromConsumed(consumed []Token) string {
	for i := range consumed {
		s, ok := consumed[i].Payload.(string)
		if !ok {
			continue
		}
		var env struct {
			Locale string `json:"locale"`
		}
		if err := json.Unmarshal([]byte(s), &env); err == nil && env.Locale != "" {
			return env.Locale
		}
	}
	return ""
}
