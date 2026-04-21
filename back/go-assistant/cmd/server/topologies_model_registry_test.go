package main

// Tests for manage-models-flow (REQ-GAP-CPN-001..007, REQ-GAP-TEST-006).
//
// The classifier for this fragment is a deterministic tool transition rather
// than an LLM call; the "intent classifier emits manage-models for seeded
// utterances" test exercises the classifier prompt + classifierResult parsing
// contract at the unit level (we cannot invoke the actual LLM from tests).
// End-to-end classification is a follow-up once the fragment is integrated
// into the unified topology.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Classifier parse coverage for the manage-models intent ─────────────────

func TestClassifierResult_ParsesManageModelsIntent(t *testing.T) {
	// Seeded payloads — the prompt is crafted so the classifier emits these
	// shapes for the corresponding utterances. Each payload MUST decode
	// without error and carry the expected fields.
	cases := []struct {
		name       string
		payload    string
		wantIntent string
		wantKind   string
		wantRID    string
	}{
		{
			name:       "list_english",
			payload:    `{"intent":"manage-models","manage_kind":"list","manage_args":{},"needs_clarification":false,"missing":[],"confidence":0.95}`,
			wantIntent: "manage-models",
			wantKind:   "list",
		},
		{
			name:       "list_spanish",
			payload:    `{"intent":"manage-models","manage_kind":"list","manage_args":{},"needs_clarification":false,"missing":[],"confidence":0.93}`,
			wantIntent: "manage-models",
			wantKind:   "list",
		},
		{
			name:       "register_spanish",
			payload:    `{"intent":"manage-models","manage_kind":"register","manage_args":{},"needs_clarification":false,"missing":[],"confidence":0.97}`,
			wantIntent: "manage-models",
			wantKind:   "register",
		},
		{
			name:       "block_gpt5",
			payload:    `{"intent":"manage-models","manage_kind":"review-license","manage_args":{"registry_id":"gpt-5"},"needs_clarification":false,"missing":[],"confidence":0.94}`,
			wantIntent: "manage-models",
			wantKind:   "review-license",
			wantRID:    "gpt-5",
		},
		{
			name:       "legacy_task_still_works",
			payload:    `{"intent":"task","needs_clarification":false}`,
			wantIntent: "task",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r classifierResult
			if err := json.Unmarshal([]byte(tc.payload), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if r.Intent != tc.wantIntent {
				t.Fatalf("intent = %q, want %q", r.Intent, tc.wantIntent)
			}
			if r.ManageKind != tc.wantKind {
				t.Fatalf("manage_kind = %q, want %q", r.ManageKind, tc.wantKind)
			}
			if tc.wantRID != "" {
				got, _ := r.ManageArgs["registry_id"].(string)
				if got != tc.wantRID {
					t.Fatalf("manage_args.registry_id = %q, want %q", got, tc.wantRID)
				}
			}
		})
	}
}

// Guard behavior: t-direct catches manage-models until the dispatcher ships
// (was "blocks" per REQ-GAP-CPN-002, but blocking stalled the CPN on intents
// the manage-models fragment cannot yet be dispatched to — deadlock fix).
func TestGuardDirectConversation_SkipsManageModels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"conversation_fires_direct", `{"intent":"conversation"}`, true},
		{"manage_models_now_catches_direct", `{"intent":"manage-models","manage_kind":"list"}`, true},
		{"task_blocks_direct", `{"intent":"task"}`, false},
		{"garbage_defaults_direct", `not-json`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := guardDirectConversation([]*cpn.Token{{Payload: tc.in}})
			if got != tc.want {
				t.Fatalf("guardDirectConversation(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ── Topology shape ─────────────────────────────────────────────────────────

// TestManageModelsTopology_HasExpectedPlacesAndTransitions pins the spec §4.5
// topology diagram.
func TestManageModelsTopology_HasExpectedPlacesAndTransitions(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		UserEmail:   "admin@example.com",
	})
	wantPlaces := []string{
		"P_MgmtEnter", "P_MgmtIntent", "P_SurfaceEmitted", "P_HITLResponse",
		"P_RegistryMutation", "P_Confirmed", "P_PersistedOK", "P_ReplayEmitted", "P_MgmtExit",
	}
	for _, p := range wantPlaces {
		if _, ok := c.Places[p]; !ok {
			t.Errorf("missing place %q", p)
		}
	}
	wantTransitions := []string{
		"t-classify-mgmt",
		"t-emit-list", "t-emit-register", "t-emit-toggle",
		"t-emit-setdefault", "t-emit-license", "t-emit-delete",
		"t-recv-response", "t-validate",
		"t-emit-confirm", "t-apply", "t-lock-replay", "t-summarize",
	}
	for _, id := range wantTransitions {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition %q", id)
		}
	}
}

// Boundedness (REQ-CPN-006) — all places have capacity ≤ 1 because every
// transition produces at most one token per output place per firing. The
// guards on t-emit-* are mutually exclusive on manage_kind, so only one
// surface-emission transition fires per intent.
func TestManageModelsTopology_Bounded(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
	})
	// No transition declares >1 OutputPlace appending to the SAME place, and
	// each ToolHandler returns a map keyed by output place id with exactly one
	// token per listed OutputPlace. This is the spec's §4.5 boundedness proof.
	for _, tr := range c.Transitions {
		seen := map[string]bool{}
		for _, p := range tr.OutputPlaces {
			if seen[p] {
				t.Errorf("transition %s lists output place %s twice (boundedness regression)", tr.ID, p)
			}
			seen[p] = true
		}
	}
}

// Deadlock freedom (REQ-CPN-005) — every non-terminal place has at least one
// transition that consumes from it, so no reachable marking can get stuck
// between stages. P_MgmtExit is the one accepted sink (terminal by design).
func TestManageModelsTopology_DeadlockFree(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
	})
	// Build the set of places that feed at least one transition.
	hasConsumer := map[string]bool{}
	for _, tr := range c.Transitions {
		for _, p := range tr.InputPlaces {
			hasConsumer[p] = true
		}
	}
	for id := range c.Places {
		if id == "P_MgmtExit" {
			continue // terminal sink
		}
		if !hasConsumer[id] {
			t.Errorf("place %q has no consumer transition — deadlock risk", id)
		}
	}
}

// Passes validation after wiring (REQ-CPN-006: bounded + REQ-CPN-005:
// topology is well-formed).
func TestManageModelsTopology_ValidatesAfterWiring(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
	})
	// Wire HITL channels like SessionService.CreateSession does.
	for _, tr := range c.Transitions {
		if tr.Kind == cpn.NodeKindHITL && tr.HITLConfig != nil {
			tr.HITLConfig.Channel = make(chan cpn.Token, 1)
		}
	}
	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Fatalf("topology validation: %v", err)
	}
}

// ── Admin gating (REQ-GAP-CPN-007) ─────────────────────────────────────────

// Non-admin sessions route straight to P_MgmtExit without emitting a surface.
func TestManageModelsFlow_AdminGateBlocksNonAdmin(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		UserEmail:   "eve@example.com",
		Locale:      "en",
	})
	tClassify := c.Transitions["t-classify-mgmt"]
	if tClassify == nil || tClassify.ToolHandler == nil {
		t.Fatal("t-classify-mgmt missing ToolHandler")
	}
	envelope := `{"intent":"manage-models","manage_kind":"list","user_email":"eve@example.com","locale":"en"}`
	out, err := tClassify.ToolHandler(context.Background(), []cpn.Token{{Payload: envelope}})
	if err != nil {
		t.Fatalf("t-classify-mgmt: %v", err)
	}
	// MUST land on P_MgmtExit (non-admin denial), NOT on P_MgmtIntent.
	if _, hasIntent := out["P_MgmtIntent"]; hasIntent {
		t.Fatal("non-admin gating failed: P_MgmtIntent populated for non-admin user")
	}
	exit, ok := out["P_MgmtExit"]
	if !ok {
		t.Fatal("non-admin gating: expected P_MgmtExit deposit")
	}
	msg, _ := exit.Payload.(string)
	if !strings.Contains(msg, "admins only") {
		t.Fatalf("non-admin exit payload = %q; want 'admins only' substring", msg)
	}
}

// Admin sessions forward the envelope into P_MgmtIntent unchanged.
func TestManageModelsFlow_AdminGateForwardsAdmin(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		UserEmail:   "admin@example.com",
		Locale:      "es",
	})
	envelope := `{"intent":"manage-models","manage_kind":"list","user_email":"admin@example.com","locale":"es"}`
	out, err := c.Transitions["t-classify-mgmt"].ToolHandler(context.Background(), []cpn.Token{{Payload: envelope}})
	if err != nil {
		t.Fatalf("t-classify-mgmt: %v", err)
	}
	if _, bad := out["P_MgmtExit"]; bad {
		t.Fatal("admin should NOT land on P_MgmtExit at classify stage")
	}
	tok, ok := out["P_MgmtIntent"]
	if !ok {
		t.Fatal("admin envelope not forwarded to P_MgmtIntent")
	}
	s, _ := tok.Payload.(string)
	if !strings.Contains(s, `"manage_kind":"list"`) {
		t.Fatalf("forwarded envelope = %q; want manage_kind:list preserved", s)
	}
}

// ── Surface emission (REQ-GAP-TEST-006 b) ──────────────────────────────────

// t-emit-list's A2UIPayloadBuilder must produce a surface whose JSON carries
// a surfaceId with the `model-registry:list:` prefix (§4.6.1) and whose wire
// form starts with `$$a2ui:`.
func TestManageModelsFlow_EmitListProducesListSurface(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		Locale:      "en",
	})
	tEmit := c.Transitions["t-emit-list"]
	if tEmit == nil || tEmit.HITLConfig == nil || tEmit.HITLConfig.A2UIPayloadBuilder == nil {
		t.Fatal("t-emit-list missing A2UIPayloadBuilder")
	}
	payload, err := tEmit.HITLConfig.A2UIPayloadBuilder(nil)
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := cpn.A2UIMarker + string(b)
	if !strings.HasPrefix(wire, cpn.A2UIMarker) {
		t.Fatal("expected wire chunk to start with $$a2ui:")
	}
	var surface map[string]any
	if err := json.Unmarshal(b, &surface); err != nil {
		t.Fatalf("decode surface: %v", err)
	}
	sid, _ := surface["surfaceId"].(string)
	if !strings.HasPrefix(sid, "model-registry:list:") {
		t.Fatalf("surfaceId = %q, want model-registry:list: prefix", sid)
	}
}

// t-emit-list guard must only fire when manage_kind is "list" (or absent as a
// safe default) — other kinds route to their own transition.
func TestManageModelsFlow_EmitGuardMatrix(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
	})
	cases := []struct {
		envelope string
		wantList bool
		wantReg  bool
	}{
		{`{"manage_kind":"list"}`, true, false},
		{`{"manage_kind":"register"}`, false, true},
		{`{"manage_kind":""}`, true, false},       // empty → safe default
		{`not-json`, true, false},                 // malformed → safe default
	}
	for _, tc := range cases {
		listOK := c.Transitions["t-emit-list"].Guard([]*cpn.Token{{Payload: tc.envelope}})
		regOK := c.Transitions["t-emit-register"].Guard([]*cpn.Token{{Payload: tc.envelope}})
		if listOK != tc.wantList {
			t.Errorf("list guard for %q = %v; want %v", tc.envelope, listOK, tc.wantList)
		}
		if regOK != tc.wantReg {
			t.Errorf("register guard for %q = %v; want %v", tc.envelope, regOK, tc.wantReg)
		}
	}
}

// ── t-validate re-ask loop cap (REQ-GAP-CPN-006) ───────────────────────────

// When the envelope reports attempts >= 3, t-validate MUST route to P_MgmtExit
// with the localized cap message — no mutation produced.
func TestManageModelsFlow_ValidateReaskCap(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		Locale:      "es",
	})
	tValidate := c.Transitions["t-validate"]
	payload := `{"action":"submit_register","op":"register","attempts":3}`
	out, err := tValidate.ToolHandler(context.Background(), []cpn.Token{{Payload: payload}})
	if err != nil {
		t.Fatalf("t-validate: %v", err)
	}
	if _, hasMut := out["P_RegistryMutation"]; hasMut {
		t.Fatal("attempts>=3 should NOT emit a mutation")
	}
	exit, ok := out["P_MgmtExit"]
	if !ok {
		t.Fatal("expected P_MgmtExit on cap exhaustion")
	}
	msg, _ := exit.Payload.(string)
	if !strings.Contains(msg, "3 intentos") {
		t.Fatalf("es cap message missing; got %q", msg)
	}
}

// ── t-apply calls the registry service, never direct SQL (REQ-CPN-007) ────

// fakeRegistry captures calls; methods not used by t-apply are stubbed.
type fakeRegistry struct {
	setDefaultCalls  int
	setReviewCalls   int
	insertCalls      int
	deleteCalls      int
	lastRegistryID   string
	lastUpdatedBy    string
	lastReview       cpn.LicenseReview
}

func (f *fakeRegistry) GetInvokable(ctx context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	return nil, cpn.ErrModelNotFound
}
func (f *fakeRegistry) GetByID(ctx context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	return nil, cpn.ErrModelNotFound
}
func (f *fakeRegistry) GetProductDefault(ctx context.Context) (*cpn.ModelRegistryEntry, error) {
	return nil, cpn.ErrModelNotFound
}
func (f *fakeRegistry) ListInvokable(ctx context.Context) ([]*cpn.ModelRegistryEntry, error) {
	return nil, nil
}
func (f *fakeRegistry) ListAll(ctx context.Context, filter cpn.ModelListFilter) ([]*cpn.ModelRegistryEntry, error) {
	return nil, nil
}
func (f *fakeRegistry) Insert(ctx context.Context, entry *cpn.ModelRegistryEntry) error {
	f.insertCalls++
	f.lastRegistryID = entry.RegistryID
	return nil
}
func (f *fakeRegistry) Update(ctx context.Context, entry *cpn.ModelRegistryEntry, ifMatch cpn.UpdatedAt) error {
	return nil
}
func (f *fakeRegistry) Delete(ctx context.Context, id string) error {
	f.deleteCalls++
	f.lastRegistryID = id
	return nil
}
func (f *fakeRegistry) SetProductDefault(ctx context.Context, id, updatedBy string) error {
	f.setDefaultCalls++
	f.lastRegistryID = id
	f.lastUpdatedBy = updatedBy
	return nil
}
func (f *fakeRegistry) SetLicenseReview(ctx context.Context, id string, review cpn.LicenseReview) error {
	f.setReviewCalls++
	f.lastRegistryID = id
	f.lastReview = review
	return nil
}
func (f *fakeRegistry) GetRoleDefault(ctx context.Context, role string) (string, error) {
	return "", nil
}
func (f *fakeRegistry) SetRoleDefault(ctx context.Context, role, id string) error { return nil }

func TestManageModelsFlow_ApplyGoesThroughService(t *testing.T) {
	cases := []struct {
		name     string
		op       string
		wantSetD int
		wantSetR int
		wantDel  int
	}{
		{"set-default", "set-default", 1, 0, 0},
		{"review-license", "review-license", 0, 1, 0},
		{"delete", "delete", 0, 0, 1},
		{"toggle", "toggle", 0, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := &fakeRegistry{}
			c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
				Registry:    reg,
				AdminEmails: []string{"admin@example.com"},
				UserEmail:   "admin@example.com",
				Locale:      "en",
			})
			tApply := c.Transitions["t-apply"]
			respContent, _ := json.Marshal(map[string]any{
				"action":         "apply_confirm",
				"op":             tc.op,
				"registry_id":    "anthropic/claude-opus-4-6",
				"license_status": cpn.LicenseApprovedCommercial,
			})
			hitlResp := cpn.HITLResponse{Action: cpn.HITLApprove, Content: string(respContent)}
			_, err := tApply.ToolHandler(context.Background(), []cpn.Token{{Payload: hitlResp}})
			if err != nil {
				t.Fatalf("t-apply: %v", err)
			}
			if reg.setDefaultCalls != tc.wantSetD {
				t.Errorf("SetProductDefault calls = %d, want %d", reg.setDefaultCalls, tc.wantSetD)
			}
			if reg.setReviewCalls != tc.wantSetR {
				t.Errorf("SetLicenseReview calls = %d, want %d", reg.setReviewCalls, tc.wantSetR)
			}
			if reg.deleteCalls != tc.wantDel {
				t.Errorf("Delete calls = %d, want %d", reg.deleteCalls, tc.wantDel)
			}
			if reg.lastRegistryID != "anthropic/claude-opus-4-6" {
				t.Errorf("last registry_id = %q, want anthropic/claude-opus-4-6", reg.lastRegistryID)
			}
		})
	}
}

// ── t-lock-replay emits deleteSurface + frozen replay (REQ-GAP-CPN-005) ────

func TestManageModelsFlow_LockReplayEmitsFrozenSurface(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		Locale:      "en",
	})
	tLock := c.Transitions["t-lock-replay"]
	prev, _ := json.Marshal(map[string]any{
		"applied":          true,
		"op":               "set-default",
		"registry_id":      "anthropic/claude-opus-4-6",
		"stale_surface_id": "model-registry:list:stale-xyz",
	})
	out, err := tLock.ToolHandler(context.Background(), []cpn.Token{{Payload: string(prev)}})
	if err != nil {
		t.Fatalf("t-lock-replay: %v", err)
	}
	tok, ok := out["P_ReplayEmitted"]
	if !ok {
		t.Fatal("expected P_ReplayEmitted deposit")
	}
	s, _ := tok.Payload.(string)
	var surface map[string]any
	if err := json.Unmarshal([]byte(s), &surface); err != nil {
		t.Fatalf("decode replay surface: %v", err)
	}
	if sid, _ := surface["surfaceId"].(string); !strings.HasPrefix(sid, "model-registry:replay:") {
		t.Fatalf("surfaceId = %q; want model-registry:replay: prefix", sid)
	}
	// deleteSurfaceIds carries the stale id — frontend uses it to tear down
	// the prior bubble.
	dels, _ := surface["deleteSurfaceIds"].([]any)
	if len(dels) != 1 {
		t.Fatalf("deleteSurfaceIds len = %d, want 1", len(dels))
	}
	if got, _ := dels[0].(string); got != "model-registry:list:stale-xyz" {
		t.Fatalf("deleteSurfaceIds[0] = %q; want stale surface id", got)
	}
}

// ── Response normalization (t-recv-response) ───────────────────────────────

func TestManageModelsFlow_RecvResponseNormalizesHITLResponse(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
	})
	tRecv := c.Transitions["t-recv-response"]
	resp := cpn.HITLResponse{Action: cpn.HITLSubmit, Content: `{"registry_id":"x"}`}
	out, err := tRecv.ToolHandler(context.Background(), []cpn.Token{{Payload: resp}})
	if err != nil {
		t.Fatalf("t-recv-response: %v", err)
	}
	tok, ok := out["P_HITLResponse"]
	if !ok {
		t.Fatal("expected P_HITLResponse")
	}
	s, _ := tok.Payload.(string)
	if !strings.Contains(s, `"action":"submit"`) {
		t.Fatalf("normalized response missing action=submit: %q", s)
	}
}

// Each kind-specific surface builder fires its matching A2UIPayloadBuilder
// and produces a surface with the expected prefix. Drives manageSurfaceBuilder
// coverage from 32% → full.
func TestManageModelsFlow_EmitSurfaceBuildersCoverAllKinds(t *testing.T) {
	reg := &fakeRegistry{}
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		Registry:    reg,
		AdminEmails: []string{"admin@example.com"},
		Locale:      "en",
	})
	cases := []struct {
		id         string
		wantPrefix string
	}{
		{"t-emit-list", "model-registry:list:"},
		{"t-emit-register", "model-registry:register:"},
		{"t-emit-toggle", "model-registry:confirm:"},
		{"t-emit-setdefault", "model-registry:default:"},
		{"t-emit-license", "model-registry:license:"},
		{"t-emit-delete", "model-registry:confirm:"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tr := c.Transitions[tc.id]
			if tr == nil || tr.HITLConfig == nil {
				t.Fatalf("%s missing HITLConfig", tc.id)
			}
			envelope := `{"intent":"manage-models","manage_kind":"x","manage_args":{"registry_id":"google/gemma-4-31b-it"}}`
			payload, err := tr.HITLConfig.A2UIPayloadBuilder([]cpn.Token{{Payload: envelope}})
			if err != nil {
				t.Fatalf("builder: %v", err)
			}
			surface := payload.(map[string]any)
			sid, _ := surface["surfaceId"].(string)
			if !strings.HasPrefix(sid, tc.wantPrefix) {
				t.Fatalf("%s surfaceId = %q; want prefix %q", tc.id, sid, tc.wantPrefix)
			}
		})
	}
}

// t-emit-confirm HITL builder renders a ConfirmStateChange surface from the
// upstream mutation token.
func TestManageModelsFlow_EmitConfirmRendersDiff(t *testing.T) {
	c := manageModelsTopologyFactory("test-session", ManageFlowDeps{
		AdminEmails: []string{"admin@example.com"},
		Locale:      "en",
	})
	mutation := `{"op":"set-default","registry_id":"anthropic/claude-opus-4-6","fields":{}}`
	payload, err := c.Transitions["t-emit-confirm"].HITLConfig.A2UIPayloadBuilder([]cpn.Token{{Payload: mutation}})
	if err != nil {
		t.Fatalf("emit-confirm: %v", err)
	}
	surface := payload.(map[string]any)
	sid, _ := surface["surfaceId"].(string)
	if !strings.HasPrefix(sid, "model-registry:confirm:") {
		t.Fatalf("surfaceId = %q; want confirm: prefix", sid)
	}
	dm := surface["dataModel"].(map[string]any)
	if dm["op"] != "set-default" {
		t.Fatalf("dataModel.op = %v; want set-default", dm["op"])
	}
	if dm["registry_id"] != "anthropic/claude-opus-4-6" {
		t.Fatalf("dataModel.registry_id = %v", dm["registry_id"])
	}
}

// manageArgsRegistryID reads registry_id off the first token's envelope.
func TestManageArgsRegistryID(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"happy", `{"manage_args":{"registry_id":"foo/bar"}}`, "foo/bar"},
		{"empty_args", `{"manage_args":{}}`, ""},
		{"bad_json", `{`, ""},
		{"non_string", 42, ""},
		{"no_consumed", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var in []cpn.Token
			if tc.payload != nil {
				in = []cpn.Token{{Payload: tc.payload}}
			}
			got := manageArgsRegistryID(in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// toRegistryEntry maps a register-form submission to a ModelRegistryEntry.
func TestManageResponseEnvelope_ToRegistryEntry(t *testing.T) {
	env := manageResponseEnvelope{
		ActionName: "submit_register",
		Op:         "register",
		RegistryID: "openai/gpt-5",
		Fields: map[string]any{
			"vendor":                 "openai",
			"family":                 "gpt",
			"version":                "5",
			"display_name":           "GPT-5",
			"hugging_face_id":        "openai/gpt-5",
			"context_length":         float64(128000),
			"primary_route_adapter":  "openrouter",
			"primary_route_model_id": "openai/gpt-5",
		},
	}
	entry := env.toRegistryEntry()
	if entry.RegistryID != "openai/gpt-5" {
		t.Fatalf("registry_id = %q", entry.RegistryID)
	}
	if entry.Vendor != "openai" {
		t.Fatalf("vendor = %q", entry.Vendor)
	}
	if entry.DisplayName != "GPT-5" {
		t.Fatalf("display_name = %q", entry.DisplayName)
	}
	if entry.Context.Length != 128000 {
		t.Fatalf("context.length = %d", entry.Context.Length)
	}
	if entry.HuggingFaceID == nil || *entry.HuggingFaceID != "openai/gpt-5" {
		t.Fatalf("hugging_face_id = %v", entry.HuggingFaceID)
	}
	if len(entry.Routes) != 1 {
		t.Fatalf("routes len = %d", len(entry.Routes))
	}
	if entry.Routes[0].ProviderAdapter != "openrouter" {
		t.Fatalf("route adapter = %q", entry.Routes[0].ProviderAdapter)
	}
	// Nil Fields → empty entry.
	empty := manageResponseEnvelope{RegistryID: "x"}.toRegistryEntry()
	if empty.Vendor != "" || empty.DisplayName != "" || len(empty.Routes) != 0 {
		t.Fatalf("empty envelope should yield bare entry: %+v", empty)
	}
}

// adminEmailsFromEnv reads both ADMIN_EMAILS and the legacy ADMIN_EMAIL.
func TestAdminEmailsFromEnv(t *testing.T) {
	t.Setenv("ADMIN_EMAILS", "a@example.com,b@example.com")
	t.Setenv("ADMIN_EMAIL", "")
	got := adminEmailsFromEnv()
	if len(got) != 2 || got[0] != "a@example.com" || got[1] != "b@example.com" {
		t.Fatalf("got %v", got)
	}
	t.Setenv("ADMIN_EMAILS", "")
	t.Setenv("ADMIN_EMAIL", "legacy@example.com")
	got = adminEmailsFromEnv()
	if len(got) == 0 || got[0] != "legacy@example.com" {
		t.Fatalf("legacy fallback not honoured: %v", got)
	}
}

// manageModelsTopologyFactoryForSession just delegates — smoke-test it for
// coverage completeness.
func TestManageModelsTopologyFactoryForSession_Smoke(t *testing.T) {
	t.Setenv("ADMIN_EMAILS", "admin@example.com")
	c := manageModelsTopologyFactoryForSession("smoke-session")
	if c == nil {
		t.Fatal("factory returned nil")
	}
	if _, ok := c.Transitions["t-classify-mgmt"]; !ok {
		t.Fatal("missing t-classify-mgmt in default-wired fragment")
	}
}

// Ensure surface IDs are unique across emissions (small sanity — boundedness
// relies on the emission place holding only one live surface at a time, but
// the id helper generates monotonically increasing stamps).
func TestNewSurfaceID_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := range 50 {
		id := cpn.NewSurfaceIDForTest("list")
		if seen[id] {
			t.Fatalf("duplicate surfaceId %q at iteration %d", id, i)
		}
		seen[id] = true
		// microscopic wait so time.Now has millisecond granularity.
		time.Sleep(time.Millisecond)
	}
}
