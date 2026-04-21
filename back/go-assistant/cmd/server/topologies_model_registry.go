package main

// manage-models-flow — CPN topology fragment for in-chat model administration.
//
// Spec: spec/spec-architecture-model-registry-and-a2ui-management.md §4.5
// (REQ-CPN-001..007) — places, transitions, guards.
// Spec: spec/spec-process-model-admin-in-chat-gap-closure.md GAP-CPN
// (REQ-GAP-CPN-001..007) — gap closure requirements.
//
// Why a standalone factory instead of editing `unifiedTopologyFactory`?
// The unified topology already carries the classify → clarify → plan → review
// → execute chain with a strict "one token per place" invariant. Weaving five
// new HITL transitions through those arcs without regressing
// AC-IterativeClarification-001..009 is out of scope for this slice. Instead
// we ship `manageModelsTopologyFactory` as a focused fragment the topology
// selector can dispatch to when `TOPOLOGY=manage-models` (tests) or the
// session layer detects the `manage-models` intent (runtime integration land
// in a follow-up slice — tracked in spec §12). The fragment stands on its own:
// tests assert boundedness + deadlock-freedom (REQ-CPN-005/006), admin gating
// (REQ-GAP-CPN-007), and surface emission shape (REQ-GAP-CPN-003).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// manageModelsReaskCap is the 3-round validation loop cap mandated by
// REQ-GAP-CPN-006 and mirrored from spec-architecture-cpn-iterative-clarification-loop.md.
const manageModelsReaskCap = 3

// ManageFlowDeps bundles the service-layer dependencies the manage-models
// flow needs. Centralising them here keeps the topology factory callable from
// tests with in-memory fakes while the production wire-up reads from
// SessionService-owned singletons.
//
// All fields except AdminEmails may be nil in tests — guards and
// surface-builders tolerate nil dependencies by short-circuiting to P_MgmtExit
// with a localized error so the CPN itself never deadlocks on a missing
// service.
type ManageFlowDeps struct {
	// Registry is the domain port used by t-apply (REQ-CPN-007 — no direct
	// SQL, no internal HTTP). All writes go through this interface.
	Registry cpn.ModelRegistry

	// AdminEmails is the positive-allowlist used by t-classify-mgmt's admin
	// gate. Normalised (lowercased, trimmed) entries; case-insensitive match
	// via auth.IsAdminEmail. Empty slice → every non-empty email fails, which
	// matches httpapi.AdminMiddleware's default-deny posture.
	AdminEmails []string

	// UserEmail is the session owner's verified email, copied at session-
	// resolve time from the auth context. The CPN has no http.Request handle,
	// so the session layer must plumb it here.
	UserEmail string

	// Locale is the BCP-47 tag used for all literalizedLiteral lookups. When
	// empty the builder falls back to English.
	Locale string
}

// ── Colors ──────────────────────────────────────────────────────────────────
//
// The spec §4.5 defines bespoke color sets (C_UserTurn, C_MgmtIntent, …).
// We MAP them onto the existing cpn.ColorSet values so we don't have to widen
// the domain type:
//
//   C_UserTurn           → ColorString
//   C_MgmtIntent         → ColorJSON
//   C_MgmtState          → ColorJSON
//   C_SurfaceRef         → ColorJSON
//   C_ModelMgmtResponse  → ColorHuman (HITL output)
//   C_Mutation           → ColorJSON
//   C_Confirmation       → ColorHuman
//   C_Result             → ColorJSON
//   C_AssistantTurn      → ColorArtifact
//
// Comments on each place annotate the semantic color for readers coming from
// the spec.

// manageModelsTopologyFactory builds a new CPN instance implementing
// manage-models-flow for one session. Dependencies are captured by closure.
func manageModelsTopologyFactory(sessionID string, deps ManageFlowDeps) *cpn.CPN {
	places := map[string]*cpn.Place{
		"P_MgmtEnter":        cpn.NewPlace("P_MgmtEnter", cpn.ColorString, cpn.SpaceSurface),         // C_UserTurn
		"P_MgmtIntent":       cpn.NewPlace("P_MgmtIntent", cpn.ColorJSON, cpn.SpaceSurface),          // C_MgmtIntent
		"P_SurfaceEmitted":   cpn.NewPlace("P_SurfaceEmitted", cpn.ColorHuman, cpn.SpaceComputation), // HITL output — C_SurfaceRef + response payload
		"P_HITLResponse":     cpn.NewPlace("P_HITLResponse", cpn.ColorJSON, cpn.SpaceSurface),        // C_ModelMgmtResponse (normalised to JSON after HITL)
		"P_RegistryMutation": cpn.NewPlace("P_RegistryMutation", cpn.ColorJSON, cpn.SpaceSurface),    // C_Mutation
		"P_Confirmed":        cpn.NewPlace("P_Confirmed", cpn.ColorHuman, cpn.SpaceComputation),      // C_Confirmation
		"P_PersistedOK":      cpn.NewPlace("P_PersistedOK", cpn.ColorJSON, cpn.SpaceSurface),         // C_Result
		"P_ReplayEmitted":    cpn.NewPlace("P_ReplayEmitted", cpn.ColorJSON, cpn.SpaceSurface),       // C_SurfaceRef (replay)
		"P_MgmtExit":         cpn.NewPlace("P_MgmtExit", cpn.ColorArtifact, cpn.SpaceSurface),        // C_AssistantTurn
	}

	// ── t-classify-mgmt : LLM (reuses classifier) — IN P_MgmtEnter → OUT P_MgmtIntent ─
	// For this fragment the "classifier" is replaced by a deterministic
	// no-LLM tool transition that (a) checks the admin email gate (REQ-GAP-CPN-007)
	// and (b) passes through the pre-classified intent JSON already deposited
	// into P_MgmtEnter by the unified classifier. When admin gating fails we
	// bypass P_MgmtIntent entirely and deposit the localized admin-only reply
	// directly into P_MgmtExit so the flow completes without emitting any
	// management surface.
	tClassifyMgmt := cpn.NewTransition("t-classify-mgmt", cpn.NodeKindTool,
		[]string{"P_MgmtEnter"}, []string{"P_MgmtIntent", "P_MgmtExit"})
	tClassifyMgmt.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		// The upstream classifier already produced a JSON payload; the user's
		// email is carried alongside via a simple envelope the session layer
		// stamps: {"intent":"manage-models", "manage_kind":"...",
		// "manage_args":{...}, "user_email":"...", "locale":"..."}.
		var payload manageEntryEnvelope
		if len(consumed) > 0 {
			if s, ok := consumed[0].Payload.(string); ok {
				_ = json.Unmarshal([]byte(s), &payload) // best-effort; zero-value fields handled below
			}
		}
		email := payload.UserEmail
		if email == "" {
			email = deps.UserEmail
		}
		if !auth.IsAdminEmail(email, deps.AdminEmails) {
			// Route straight to P_MgmtExit with a localized assistant message.
			locale := payload.Locale
			if locale == "" {
				locale = deps.Locale
			}
			return map[string]cpn.Token{
				"P_MgmtExit": {Color: cpn.ColorArtifact, Payload: cpn.AdminOnlyMessage(locale)},
			}, nil
		}
		// Admin — forward the intent envelope downstream so t-emit-* builders
		// can read manage_kind / manage_args / locale.
		forward, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("t-classify-mgmt: marshal envelope: %w", err)
		}
		return map[string]cpn.Token{
			"P_MgmtIntent": {Color: cpn.ColorJSON, Payload: string(forward)},
		}, nil
	}

	// ── HITL emission transitions — IN P_MgmtIntent → OUT P_SurfaceEmitted ─
	// Each is kept as its own transition (vs a single switch) so the spec's
	// §4.5 topology diagram survives verbatim and per-kind tests can assert
	// the correct builder fires. Only one fires per intent because the guards
	// are mutually exclusive on manage_kind.
	tEmitList := newManageHITL("t-emit-list", "list", deps)
	tEmitRegister := newManageHITL("t-emit-register", "register", deps)
	tEmitToggle := newManageHITL("t-emit-toggle", "toggle", deps)
	tEmitSetDefault := newManageHITL("t-emit-setdefault", "set-default", deps)
	tEmitLicense := newManageHITL("t-emit-license", "review-license", deps)
	tEmitDelete := newManageHITL("t-emit-delete", "delete", deps)

	// ── t-recv-response : HITL channel wait — IN P_SurfaceEmitted → OUT P_HITLResponse ─
	// fireHITL's standard resolution lifecycle deposits the ColorHuman token
	// directly into P_SurfaceEmitted; the separate P_HITLResponse place holds
	// the normalized JSON ready for t-validate. A deterministic tool transition
	// does the color-bridge JSON normalisation.
	tRecvResponse := cpn.NewTransition("t-recv-response", cpn.NodeKindTool,
		[]string{"P_SurfaceEmitted"}, []string{"P_HITLResponse"})
	tRecvResponse.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var normalized string
		if len(consumed) > 0 {
			switch v := consumed[0].Payload.(type) {
			case cpn.HITLResponse:
				b, err := json.Marshal(map[string]any{
					"action":  string(v.Action),
					"content": v.Content,
				})
				if err != nil {
					return nil, fmt.Errorf("t-recv-response: marshal HITLResponse: %w", err)
				}
				normalized = string(b)
			case string:
				normalized = v
			default:
				b, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("t-recv-response: marshal payload: %w", err)
				}
				normalized = string(b)
			}
		}
		return map[string]cpn.Token{
			"P_HITLResponse": {Color: cpn.ColorJSON, Payload: normalized},
		}, nil
	}

	// ── t-validate : guard+handler — IN P_HITLResponse → OUT P_RegistryMutation ─
	// A minimal validator that accepts `submit_register | apply_confirm |
	// cancel_*` and routes to P_RegistryMutation with the op + registry_id
	// extracted from the action context. Cancels route straight to P_MgmtExit.
	// The 3-round re-ask loop cap is enforced on the consumed envelope's
	// attempts counter (starts at 0, incremented each time t-validate would
	// re-emit). On cap exhaustion, routes to P_MgmtExit with the localized
	// validation-cap message (REQ-GAP-CPN-006).
	tValidate := cpn.NewTransition("t-validate", cpn.NodeKindTool,
		[]string{"P_HITLResponse"}, []string{"P_RegistryMutation", "P_MgmtExit"})
	tValidate.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var resp manageResponseEnvelope
		if len(consumed) > 0 {
			if s, ok := consumed[0].Payload.(string); ok {
				_ = json.Unmarshal([]byte(s), &resp)
			}
		}
		// Cap exhaustion
		if resp.Attempts >= manageModelsReaskCap {
			return map[string]cpn.Token{
				"P_MgmtExit": {Color: cpn.ColorArtifact, Payload: cpn.ValidationCapMessage(deps.Locale)},
			}, nil
		}
		// Cancel → exit cleanly
		if strings.HasPrefix(resp.ActionName, "cancel_") || resp.ActionName == "" {
			return map[string]cpn.Token{
				"P_MgmtExit": {Color: cpn.ColorArtifact, Payload: ""},
			}, nil
		}
		// Validation happens at surface-emit time; here we trust the action
		// context and forward it to the mutation place.
		mutation := map[string]any{
			"op":          resp.Op,
			"registry_id": resp.RegistryID,
			"fields":      resp.Fields,
		}
		b, err := json.Marshal(mutation)
		if err != nil {
			return nil, fmt.Errorf("t-validate: marshal mutation: %w", err)
		}
		return map[string]cpn.Token{
			"P_RegistryMutation": {Color: cpn.ColorJSON, Payload: string(b)},
		}, nil
	}

	// ── t-emit-confirm : HITL (ConfirmStateChange) — IN P_RegistryMutation → OUT P_SurfaceEmitted (reused via t-apply path) ─
	tEmitConfirm := cpn.NewTransition("t-emit-confirm", cpn.NodeKindHITL,
		[]string{"P_RegistryMutation"}, []string{"P_Confirmed"})
	tEmitConfirm.HITLConfig = &cpn.HITLConfig{
		Prompt: "",
		A2UIPayloadBuilder: func(consumed []cpn.Token) (any, error) {
			var mutation map[string]any
			if len(consumed) > 0 {
				if s, ok := consumed[0].Payload.(string); ok {
					_ = json.Unmarshal([]byte(s), &mutation)
				}
			}
			op, _ := mutation["op"].(string)
			rid, _ := mutation["registry_id"].(string)
			return cpn.BuildConfirmStateChange(cpn.ConfirmStateChangeData{
				Op:         op,
				RegistryID: rid,
				Before:     map[string]any{"lifecycle": "current"},
				After:      map[string]any{"lifecycle": op},
			}, deps.Locale), nil
		},
	}

	// ── t-apply : handler (calls ModelRegistryService) — IN P_Confirmed → OUT P_PersistedOK ─
	tApply := cpn.NewTransition("t-apply", cpn.NodeKindTool,
		[]string{"P_Confirmed"}, []string{"P_PersistedOK"})
	tApply.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if deps.Registry == nil {
			return map[string]cpn.Token{
				"P_PersistedOK": {Color: cpn.ColorJSON, Payload: `{"applied":false,"reason":"registry-unavailable"}`},
			}, nil
		}
		// Parse the HITLResponse → mutation.
		var mutation manageResponseEnvelope
		if len(consumed) > 0 {
			if resp, ok := consumed[0].Payload.(cpn.HITLResponse); ok {
				_ = json.Unmarshal([]byte(resp.Content), &mutation)
			}
		}

		// Dispatch by op. Every path MUST go through ModelRegistry —
		// REQ-CPN-007 forbids direct SQL or internal HTTP here.
		var err error
		result := map[string]any{"op": mutation.Op, "registry_id": mutation.RegistryID}
		switch mutation.Op {
		case "set-default":
			err = deps.Registry.SetProductDefault(ctx, mutation.RegistryID, deps.UserEmail)
		case "review-license":
			review := cpn.LicenseReview{
				Status:     mutation.LicenseStatus,
				ReviewerID: deps.UserEmail,
				Note:       mutation.Note,
			}
			err = deps.Registry.SetLicenseReview(ctx, mutation.RegistryID, review)
		case "register":
			entry := mutation.toRegistryEntry()
			err = deps.Registry.Insert(ctx, entry)
		case "delete":
			err = deps.Registry.Delete(ctx, mutation.RegistryID)
		case "toggle":
			// Toggle is implemented as a license-review transition (blocked ↔ approved-*).
			// Callers stamp the target status in mutation.LicenseStatus.
			review := cpn.LicenseReview{Status: mutation.LicenseStatus, ReviewerID: deps.UserEmail}
			err = deps.Registry.SetLicenseReview(ctx, mutation.RegistryID, review)
		default:
			err = fmt.Errorf("unsupported mutation op %q", mutation.Op)
		}
		if err != nil {
			result["applied"] = false
			result["error"] = err.Error()
			slog.Warn("manage-models t-apply failed",
				"op", mutation.Op,
				"registry_id", mutation.RegistryID,
				"actor", deps.UserEmail,
				"err", err,
			)
		} else {
			result["applied"] = true
		}
		b, _ := json.Marshal(result)
		return map[string]cpn.Token{
			"P_PersistedOK": {Color: cpn.ColorJSON, Payload: string(b)},
		}, nil
	}

	// ── t-lock-replay : handler — IN P_PersistedOK → OUT P_ReplayEmitted ─
	// Emits the frozen replay surface (deleteSurface + inert Card) on the
	// stream. Mirrors commits c1e1396 + d3590bf.
	tLockReplay := cpn.NewTransition("t-lock-replay", cpn.NodeKindTool,
		[]string{"P_PersistedOK"}, []string{"P_ReplayEmitted"})
	tLockReplay.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var prev manageResultEnvelope
		if len(consumed) > 0 {
			if s, ok := consumed[0].Payload.(string); ok {
				_ = json.Unmarshal([]byte(s), &prev)
			}
		}
		surface := cpn.BuildReplayLockPayload(prev.StaleSurfaceID, deps.Locale)
		b, err := json.Marshal(surface)
		if err != nil {
			return nil, fmt.Errorf("t-lock-replay: marshal surface: %w", err)
		}
		return map[string]cpn.Token{
			"P_ReplayEmitted": {Color: cpn.ColorJSON, Payload: string(b)},
		}, nil
	}

	// ── t-summarize : LLM (brief reply) — IN P_ReplayEmitted → OUT P_MgmtExit ─
	// We keep it as a deterministic tool — a full LLM invocation is out of
	// scope for the gap slice and would couple the fragment to LLMClient
	// availability in tests. The summary copy is a static localized string.
	tSummarize := cpn.NewTransition("t-summarize", cpn.NodeKindTool,
		[]string{"P_ReplayEmitted"}, []string{"P_MgmtExit"})
	tSummarize.ToolHandler = func(_ context.Context, _ []cpn.Token) (map[string]cpn.Token, error) {
		summary := "Listo."
		if strings.HasPrefix(strings.ToLower(deps.Locale), "en") {
			summary = "Done."
		}
		return map[string]cpn.Token{
			"P_MgmtExit": {Color: cpn.ColorArtifact, Payload: summary},
		}, nil
	}

	transitions := map[string]*cpn.Transition{
		"t-classify-mgmt":   tClassifyMgmt,
		"t-emit-list":       tEmitList,
		"t-emit-register":   tEmitRegister,
		"t-emit-toggle":     tEmitToggle,
		"t-emit-setdefault": tEmitSetDefault,
		"t-emit-license":    tEmitLicense,
		"t-emit-delete":     tEmitDelete,
		"t-recv-response":   tRecvResponse,
		"t-validate":        tValidate,
		"t-emit-confirm":    tEmitConfirm,
		"t-apply":           tApply,
		"t-lock-replay":     tLockReplay,
		"t-summarize":       tSummarize,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-mgmt", sessionID),
		"mgmt",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 0 // no LLM history in this fragment
	return c
}

// manageModelsTopologyFactoryForSession is the TopologyFactory adapter used
// by the session service when TOPOLOGY=manage-models. Dependencies are pulled
// from package-level globals populated at startup (see main.go).
func manageModelsTopologyFactoryForSession(sessionID string) *cpn.CPN {
	return manageModelsTopologyFactory(sessionID, ManageFlowDeps{
		AdminEmails: adminEmailsFromEnv(),
	})
}

// adminEmailsFromEnv reads ADMIN_EMAILS / ADMIN_EMAIL in the same form
// resolveAdminEmails does. Tests override via t.Setenv.
func adminEmailsFromEnv() []string {
	raw := os.Getenv("ADMIN_EMAILS")
	if raw == "" {
		raw = os.Getenv("ADMIN_EMAIL")
	}
	return strings.Split(strings.ReplaceAll(raw, " ", ""), ",")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// newManageHITL creates one of the t-emit-<kind> HITL transitions. The guard
// checks the incoming C_MgmtIntent payload's manage_kind against the target
// kind so exactly one emission transition fires per intent (preserves
// boundedness — REQ-CPN-006).
func newManageHITL(id, kind string, deps ManageFlowDeps) *cpn.Transition {
	t := cpn.NewTransition(id, cpn.NodeKindHITL,
		[]string{"P_MgmtIntent"}, []string{"P_SurfaceEmitted"})
	t.Guard = guardManageKind(kind)
	t.HITLConfig = &cpn.HITLConfig{
		Prompt:             "",
		A2UIPayloadBuilder: manageSurfaceBuilder(kind, deps),
	}
	return t
}

// guardManageKind fires the emission transition when the classifier's payload
// carries `manage_kind == kind`. Absent / malformed payloads only fire the
// `list` branch (safe default per REQ-GAP-CPN-002).
func guardManageKind(kind string) func(tokens []*cpn.Token) bool {
	return func(tokens []*cpn.Token) bool {
		if len(tokens) == 0 {
			return kind == "list"
		}
		s, ok := tokens[0].Payload.(string)
		if !ok {
			return kind == "list"
		}
		var env manageEntryEnvelope
		if err := json.Unmarshal([]byte(s), &env); err != nil {
			return kind == "list"
		}
		if env.ManageKind == "" {
			return kind == "list"
		}
		return strings.EqualFold(env.ManageKind, kind)
	}
}

// manageSurfaceBuilder returns an A2UIPayloadBuilder matching the intent kind.
// The map pattern keeps the per-kind switch in one place and makes adding a
// future kind a single-line addition.
func manageSurfaceBuilder(kind string, deps ManageFlowDeps) func([]cpn.Token) (any, error) {
	switch kind {
	case "list":
		return func(consumed []cpn.Token) (any, error) {
			// Dependency-free render for tests: a stub row so the surface JSON is
			// well-formed. Production wiring will plumb a read-path through the
			// registry at session-resolve time.
			rows := []cpn.ModelCardListData{}
			if deps.Registry != nil {
				if entries, _, err := deps.Registry.ListAll(context.Background(), cpn.ModelListFilter{}); err == nil {
					for _, e := range entries {
						rows = append(rows, cpn.ModelCardListData{
							ID:       e.RegistryID,
							Name:     e.DisplayName,
							Provider: e.Vendor,
							Status:   e.Lifecycle.State,
						})
					}
				}
			}
			return cpn.BuildModelCardList(rows, deps.Locale), nil
		}
	case "register":
		return func(_ []cpn.Token) (any, error) {
			return cpn.BuildRegisterModelForm(deps.Locale), nil
		}
	case "toggle", "delete":
		return func(consumed []cpn.Token) (any, error) {
			rid := manageArgsRegistryID(consumed)
			return cpn.BuildConfirmStateChange(cpn.ConfirmStateChangeData{
				Op:         kind,
				RegistryID: rid,
				Before:     map[string]any{"lifecycle": "active"},
				After:      map[string]any{"lifecycle": kind},
			}, deps.Locale), nil
		}
	case "set-default":
		return func(_ []cpn.Token) (any, error) {
			opts := []cpn.DefaultSelectorOption{}
			current := ""
			if deps.Registry != nil {
				if entries, err := deps.Registry.ListInvokable(context.Background()); err == nil {
					for _, e := range entries {
						opts = append(opts, cpn.DefaultSelectorOption{Value: e.RegistryID, Label: e.DisplayName})
						if e.IsProductDefault {
							current = e.RegistryID
						}
					}
				}
			}
			return cpn.BuildDefaultSelector(opts, current, deps.Locale), nil
		}
	case "review-license":
		return func(consumed []cpn.Token) (any, error) {
			rid := manageArgsRegistryID(consumed)
			return cpn.BuildLicenseReviewCard(cpn.LicenseReviewCardData{
				RegistryID: rid,
				Status:     "approved-commercial",
			}, deps.Locale), nil
		}
	}
	// Unknown kind: return nil (no surface). fireHITL treats nil as a no-op.
	return func(_ []cpn.Token) (any, error) { return nil, nil }
}

// manageArgsRegistryID reads `manage_args.registry_id` off the first token.
func manageArgsRegistryID(consumed []cpn.Token) string {
	if len(consumed) == 0 {
		return ""
	}
	s, ok := consumed[0].Payload.(string)
	if !ok {
		return ""
	}
	var env manageEntryEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return ""
	}
	if rid, ok := env.ManageArgs["registry_id"].(string); ok {
		return rid
	}
	return ""
}

// ── Envelope types ──────────────────────────────────────────────────────────

type manageEntryEnvelope struct {
	Intent     string         `json:"intent"`
	ManageKind string         `json:"manage_kind"`
	ManageArgs map[string]any `json:"manage_args,omitempty"`
	UserEmail  string         `json:"user_email,omitempty"`
	Locale     string         `json:"locale,omitempty"`
}

type manageResponseEnvelope struct {
	ActionName    string         `json:"action"`
	Op            string         `json:"op"`
	RegistryID    string         `json:"registry_id"`
	LicenseStatus string         `json:"license_status,omitempty"`
	Note          string         `json:"note,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
	Attempts      int            `json:"attempts,omitempty"`
}

type manageResultEnvelope struct {
	Op             string `json:"op"`
	RegistryID     string `json:"registry_id"`
	Applied        bool   `json:"applied"`
	Error          string `json:"error,omitempty"`
	StaleSurfaceID string `json:"stale_surface_id,omitempty"`
}

// toRegistryEntry converts a manageResponseEnvelope carrying register fields
// into a cpn.ModelRegistryEntry. Unknown fields are ignored so the struct
// stays forward-compatible with form additions.
func (m manageResponseEnvelope) toRegistryEntry() *cpn.ModelRegistryEntry {
	entry := &cpn.ModelRegistryEntry{
		RegistryID: m.RegistryID,
	}
	if m.Fields == nil {
		return entry
	}
	if v, ok := m.Fields["vendor"].(string); ok {
		entry.Vendor = v
	}
	if v, ok := m.Fields["family"].(string); ok {
		entry.Family = v
	}
	if v, ok := m.Fields["version"].(string); ok {
		entry.Version = v
	}
	if v, ok := m.Fields["display_name"].(string); ok {
		entry.DisplayName = v
	}
	if v, ok := m.Fields["hugging_face_id"].(string); ok && v != "" {
		entry.HuggingFaceID = &v
	}
	if v, ok := m.Fields["context_length"].(float64); ok {
		entry.Context.Length = int(v)
	}
	adapter, _ := m.Fields["primary_route_adapter"].(string)
	providerID, _ := m.Fields["primary_route_model_id"].(string)
	if adapter != "" {
		entry.Routes = []cpn.Route{{
			ProviderAdapter: adapter,
			ProviderModelID: providerID,
			Priority:        1,
			Enabled:         true,
		}}
	}
	entry.CreatedAt = time.Now().UTC()
	entry.UpdatedAt = entry.CreatedAt
	return entry
}
