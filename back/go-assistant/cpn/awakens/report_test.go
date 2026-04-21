package awakens

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validReport() AwakeningReport {
	return AwakeningReport{
		OS:       AwakeningOS{Name: "Alpine Linux", Version: "3.21", Kernel: "6.17", Arch: "aarch64"},
		Shell:    AwakeningShell{Path: "/bin/sh", Implementation: "busybox-ash"},
		Identity: AwakeningIdentity{User: "app", UID: 100, GID: 101, Home: "/home/app"},
		PresentTools: []AwakeningTool{
			{Name: "awk", Provider: "busybox", Version: "1.37.0"},
			{Name: "sed"},
		},
		AbsentTools: []string{"git", "python3"},
		Capabilities: []AwakeningCapability{
			{Name: "can-script-sh", Satisfied: true, Evidence: []string{"/bin/sh"}},
		},
		ToolsRegister: []AwakeningToolRegister{
			{Name: "shell-exec", Basis: "sh"},
		},
		NarrativeMD: "Woke up on Alpine.",
		ProbeTrace:  []AwakeningProbe{{Cmd: "uname -a", Exit: 0}},
	}
}

func TestAwakeningReport_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*AwakeningReport)
		wantErr bool
	}{
		{"happy path", func(r *AwakeningReport) {}, false},
		{"nil os name", func(r *AwakeningReport) { r.OS.Name = "" }, true},
		{"nil os arch", func(r *AwakeningReport) { r.OS.Arch = "" }, true},
		{"nil shell", func(r *AwakeningReport) { r.Shell.Path = "" }, true},
		{"too many registrations", func(r *AwakeningReport) {
			r.ToolsRegister = make([]AwakeningToolRegister, MaxToolsToRegister+1)
			for i := range r.ToolsRegister {
				r.ToolsRegister[i] = AwakeningToolRegister{Name: "t" + string(rune('a'+i%26)), Basis: "x"}
			}
		}, true},
		{"bad tool name", func(r *AwakeningReport) {
			r.ToolsRegister = []AwakeningToolRegister{{Name: "ev|il", Basis: "x"}}
		}, true},
		{"duplicate tool name", func(r *AwakeningReport) {
			r.ToolsRegister = []AwakeningToolRegister{{Name: "a", Basis: "x"}, {Name: "a", Basis: "y"}}
		}, true},
		{"empty present tool name", func(r *AwakeningReport) {
			r.PresentTools = append(r.PresentTools, AwakeningTool{})
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := validReport()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("want err, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("err must wrap ErrInvalidReport: %v", err)
			}
		})
	}
}

func TestParseReport_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := validReport()
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := ParseReport(raw)
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if got.OS.Name != orig.OS.Name {
		t.Errorf("os.name: got %q want %q", got.OS.Name, orig.OS.Name)
	}
	if len(got.PresentTools) != len(orig.PresentTools) {
		t.Errorf("present_tools len: got %d want %d", len(got.PresentTools), len(orig.PresentTools))
	}
}

func TestParseReport_Malformed(t *testing.T) {
	t.Parallel()
	_, err := ParseReport([]byte("not json"))
	if !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("want ErrInvalidReport, got %v", err)
	}
}

func TestProject_LegacyShape(t *testing.T) {
	t.Parallel()
	r := validReport()
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	snap := r.Project("host-abc", SourceAwakening, ts)

	if snap.HostID != "host-abc" {
		t.Errorf("host_id: got %q", snap.HostID)
	}
	if snap.Source != SourceAwakening {
		t.Errorf("source: got %q", snap.Source)
	}
	if !snap.CapturedAt.Equal(ts) {
		t.Errorf("captured_at: got %v", snap.CapturedAt)
	}
	// Identity.Env must be non-nil (legacy readers type-assert) AND empty.
	if snap.Identity.Env == nil {
		t.Errorf("Identity.Env must be non-nil (legacy reader compat)")
	}
	if len(snap.Identity.Env) != 0 {
		t.Errorf("Identity.Env must be empty (SEC-005); got %v", snap.Identity.Env)
	}
	// Legacy consumers read Binaries[]BinaryProbe — ensure presence mapping.
	seenAwk := false
	seenGit := false
	for _, b := range snap.Binaries {
		if b.Name == "awk" && b.Present {
			seenAwk = true
		}
		if b.Name == "git" && !b.Present {
			seenGit = true
		}
	}
	if !seenAwk || !seenGit {
		t.Errorf("binaries projection wrong: awk=%v git=%v", seenAwk, seenGit)
	}
}

func TestRedactEnvKey(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"PATH":               false,
		"HOME":               false,
		"OPENROUTER_API_KEY": true,
		"USER_TOKEN":         true,
		"my_password":        true,
		"GITHUB_CREDENTIALS": true,
		"AWS_SECRET_ACCESS":  true,
		"SSH_AUTH_SOCK":      true,
	}
	for k, want := range cases {
		if got := RedactEnvKey(k); got != want {
			t.Errorf("RedactEnvKey(%q) = %v, want %v", k, got, want)
		}
	}
	if !strings.Contains(envRedactRe.String(), "token") {
		t.Errorf("redact regex must cover 'token'")
	}
}
