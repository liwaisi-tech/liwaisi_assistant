package cpn

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildToolApprovalA2UI_UsesParsedCommandAndInvocation asserts that the
// A2UI payload carries the parsed shell command in `command` and preserves
// the raw args in `invocation.args` (REQ-003 / REQ-004 / SEC-002 of
// spec-process-bugfix-tool-hitl-single-gate.md).
func TestBuildToolApprovalA2UI_UsesParsedCommandAndInvocation(t *testing.T) {
	rawArgs := json.RawMessage(`{"command":"bash","args":["-c","uname -a && whoami"]}`)
	out := buildToolApprovalA2UI("bash_exec", rawArgs)

	if !strings.HasPrefix(out, "$$a2ui:") {
		t.Fatalf("expected $$a2ui: prefix, got %q", out)
	}
	body := strings.TrimPrefix(out, "$$a2ui:")

	var parsed struct {
		Schema       string              `json:"schema"`
		HostApproval HostApprovalPayload `json:"hostApproval"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if parsed.Schema != "host.approval" {
		t.Errorf("schema = %q, want host.approval", parsed.Schema)
	}
	if parsed.HostApproval.Command != "uname -a && whoami" {
		t.Errorf("command = %q, want %q", parsed.HostApproval.Command, "uname -a && whoami")
	}
	if parsed.HostApproval.Invocation.Tool != "bash_exec" {
		t.Errorf("invocation.tool = %q, want bash_exec", parsed.HostApproval.Invocation.Tool)
	}
	// SEC-002: args in invocation MUST be byte-identical to the executed
	// invocation. Compare after canonical re-marshalling to absorb whitespace.
	var got, want any
	_ = json.Unmarshal(parsed.HostApproval.Invocation.Args, &got)
	_ = json.Unmarshal(rawArgs, &want)
	gotB, _ := json.Marshal(got)
	wantB, _ := json.Marshal(want)
	if string(gotB) != string(wantB) {
		t.Errorf("invocation.args mismatch: got %s, want %s", gotB, wantB)
	}
	if parsed.HostApproval.Title == "" {
		t.Error("title must be populated (spec REQ-003)")
	}
	if parsed.HostApproval.Risk == "" {
		t.Error("risk must be populated")
	}
}

// TestBuildToolApprovalA2UI_FileReadRisksSafe asserts that file_read gets
// the "safe" risk band while file_write stays at caution.
func TestBuildToolApprovalA2UI_FileReadRisksSafe(t *testing.T) {
	readOut := buildToolApprovalA2UI("file_read", json.RawMessage(`{"path":"/x.txt"}`))
	writeOut := buildToolApprovalA2UI("file_write", json.RawMessage(`{"path":"/x.txt","content":"..."}`))

	var readEnv, writeEnv struct {
		HostApproval HostApprovalPayload `json:"hostApproval"`
	}
	_ = json.Unmarshal([]byte(strings.TrimPrefix(readOut, "$$a2ui:")), &readEnv)
	_ = json.Unmarshal([]byte(strings.TrimPrefix(writeOut, "$$a2ui:")), &writeEnv)

	if readEnv.HostApproval.Risk != "safe" {
		t.Errorf("file_read risk = %q, want safe", readEnv.HostApproval.Risk)
	}
	if writeEnv.HostApproval.Risk != "caution" {
		t.Errorf("file_write risk = %q, want caution", writeEnv.HostApproval.Risk)
	}
	if readEnv.HostApproval.Command != "read ← /x.txt" {
		t.Errorf("file_read command = %q", readEnv.HostApproval.Command)
	}
	if writeEnv.HostApproval.Command != "write → /x.txt" {
		t.Errorf("file_write command = %q", writeEnv.HostApproval.Command)
	}
}

// TestBuildToolApprovalA2UI_UnknownToolEscalatesRisk asserts spec §9.2:
// unknown tool shapes render but the risk MUST be "caution" regardless of
// the tool name.
func TestBuildToolApprovalA2UI_UnknownToolEscalatesRisk(t *testing.T) {
	out := buildToolApprovalA2UI("mystery_tool", json.RawMessage(`{"foo":"bar"}`))
	var env struct {
		HostApproval HostApprovalPayload `json:"hostApproval"`
	}
	_ = json.Unmarshal([]byte(strings.TrimPrefix(out, "$$a2ui:")), &env)
	if env.HostApproval.Risk != "caution" {
		t.Errorf("unknown tool risk = %q, want caution", env.HostApproval.Risk)
	}
}

// TestBuildToolApprovalA2UI_EmptyArgsYieldsObjectStub guards against invalid
// JSON in the A2UI envelope when a tool is called with no args — we want a
// valid "{}" rather than an empty RawMessage that breaks frontend parse.
func TestBuildToolApprovalA2UI_EmptyArgsYieldsObjectStub(t *testing.T) {
	out := buildToolApprovalA2UI("bash_exec", nil)
	body := strings.TrimPrefix(out, "$$a2ui:")
	// Must be parseable JSON.
	var any any
	if err := json.Unmarshal([]byte(body), &any); err != nil {
		t.Fatalf("envelope failed to parse: %v", err)
	}
}
