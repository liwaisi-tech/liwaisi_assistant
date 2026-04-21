package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Mock HostAdapter that returns canned output based on the command ──────

type cannedHostAdapter struct {
	// Responses by the FIRST sub-string in the Args[1] script (the `sh -c`
	// body). Matching is substring, first hit wins, so callers can pin
	// unique strings in their script excerpts.
	responses map[string]cpn.ExecResult
	def       cpn.ExecResult // default when no key matches
}

func (a *cannedHostAdapter) Exec(_ context.Context, req cpn.ExecRequest) (cpn.ExecResult, error) {
	// req.Args is []string{"-c", "<script>"}
	body := ""
	if len(req.Args) >= 2 {
		body = req.Args[1]
	}
	for key, r := range a.responses {
		if strings.Contains(body, key) {
			return r, nil
		}
	}
	return a.def, nil
}

func (a *cannedHostAdapter) SpawnPTY(_ context.Context, _ cpn.PTYRequest) (cpn.PTYHandle, error) {
	return cpn.PTYHandle{}, nil
}
func (a *cannedHostAdapter) KillPID(_ context.Context, _ int, _ cpn.Signal) error { return nil }
func (a *cannedHostAdapter) ReadFile(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (a *cannedHostAdapter) WriteFile(_ context.Context, _ string, _ []byte, _ fs.FileMode) error {
	return nil
}
func (a *cannedHostAdapter) Stat(_ context.Context, _ string) (cpn.FileInfo, error) {
	return cpn.FileInfo{}, nil
}

type allowGate struct{}

func (allowGate) Check(_ context.Context, _ cpn.GateOp) error { return nil }

// ── Tests ──────────────────────────────────────────────────────────────────

// TestHostDiscoveryTopology_EndToEnd runs the full CPN with a mocked
// HostAdapter and asserts that the persisted snapshot contains the expected
// identity, kernel, binaries, and capabilities.
func TestHostDiscoveryTopology_EndToEnd(t *testing.T) {
	repo := persist.NewMemoryHostCapabilityRepository()
	adapter := &cannedHostAdapter{
		responses: map[string]cpn.ExecResult{
			// whoamiScript uses `env` and printf; pin by the unique token.
			"machine_id": {Stdout: []byte(
				"user||testuser\nuid||1000\ngid||1000\nhostname||myhost\nhome||/home/testuser\n" +
					"shell||/bin/bash\nmachine_id||abc123\nPATH=/usr/bin\n" +
					"API_TOKEN=should-be-redacted\nLANG=en_US.UTF-8\n",
			)},
			// unameScript pins via os_release_begin.
			"os_release_begin": {Stdout: []byte(
				"os||Linux\nkernel||6.17.9-test\narch||x86_64\ncpu_count||8\nmem_kb||16777216\n" +
					"os_release_begin||\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nos_release_end||\n",
			)},
		},
		// Default: simulate `gcc` and `git` present, everything else missing.
		def: cpn.ExecResult{Stdout: []byte("path||\n")}, // nothing present by default
	}
	// Override a few probes to present.
	adapter.responses["command -v gcc"] = cpn.ExecResult{Stdout: []byte(
		"path||/usr/bin/gcc\nversion||gcc (Ubuntu 13.2.0-4ubuntu3) 13.2.0\n",
	)}
	adapter.responses["command -v git"] = cpn.ExecResult{Stdout: []byte(
		"path||/usr/bin/git\nversion||git version 2.43.0\n",
	)}
	adapter.responses["command -v bash"] = cpn.ExecResult{Stdout: []byte(
		"path||/bin/bash\nversion||GNU bash, version 5.2.21(1)-release\n",
	)}
	adapter.responses["command -v tar"] = cpn.ExecResult{Stdout: []byte(
		"path||/bin/tar\nversion||tar (GNU tar) 1.35\n",
	)}

	c := hostDiscoveryTopologyFactory("test-session", HostDiscoveryDeps{
		Repository: repo,
		Source:     persist.HostSnapshotSourceBootstrap,
		// Pin the probe set so the test doesn't depend on whatever lives in
		// the host's $PATH during CI. The binaries listed here match the
		// adapter's canned responses below.
		ProbeResolver: func() []hostProbeDef {
			return []hostProbeDef{
				{Name: "bash"},
				{Name: "gcc"},
				{Name: "git"},
				{Name: "python3"},
				{Name: "tar"},
			}
		},
	})
	c.HostRuntime = &cpn.HostRuntime{
		Adapter: adapter,
		Gate:    allowGate{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify snapshot was saved.
	snap, err := repo.LatestForHost(ctx, "abc123")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}

	// AC-003: gcc present → can-compile-c.
	if !snap.HasCapability("can-compile-c") {
		t.Errorf("expected can-compile-c to be satisfied (snap=%+v)", snap.Capabilities)
	}
	// AC-004: python3 missing → no failure, probe still appears.
	var py persist.BinaryProbe
	for _, b := range snap.Binaries {
		if b.Name == "python3" {
			py = b
			break
		}
	}
	if py.Name == "" {
		t.Error("expected python3 probe entry even when missing")
	}
	if py.Present {
		t.Errorf("expected python3 missing, got present")
	}
	// can-pack satisfied by tar.
	if !snap.HasCapability("can-pack") {
		t.Error("expected can-pack satisfied via tar")
	}
	// can-version-control satisfied by git.
	if !snap.HasCapability("can-version-control") {
		t.Error("expected can-version-control satisfied via git")
	}
	// can-run-python not satisfied.
	if snap.HasCapability("can-run-python") {
		t.Error("expected can-run-python unsatisfied")
	}

	// Identity has user + machine_id.
	if snap.Identity.User != "testuser" {
		t.Errorf("identity.user = %q, want testuser", snap.Identity.User)
	}
	if snap.Identity.MachineID != "abc123" {
		t.Errorf("identity.machine_id = %q, want abc123", snap.Identity.MachineID)
	}

	// AC-008: API_TOKEN redacted.
	if v, ok := snap.Identity.Env["API_TOKEN"]; ok {
		t.Errorf("API_TOKEN leaked into env: %q", v)
	}
	// But LANG should be kept.
	if snap.Identity.Env["LANG"] == "" {
		t.Error("expected LANG to be preserved in env")
	}

	// Kernel info parsed.
	if snap.Kernel.OS != "Linux" {
		t.Errorf("kernel.os = %q, want Linux", snap.Kernel.OS)
	}
	if snap.Kernel.CPUCount != 8 {
		t.Errorf("kernel.cpu_count = %d, want 8", snap.Kernel.CPUCount)
	}
	if snap.Kernel.MemMB != 16384 {
		t.Errorf("kernel.mem_mb = %d, want 16384", snap.Kernel.MemMB)
	}
	if snap.Kernel.OSRelease["NAME"] != "Ubuntu" {
		t.Errorf("os_release[NAME] = %q, want Ubuntu", snap.Kernel.OSRelease["NAME"])
	}
}

// TestHostDiscoveryTopology_SecretRedaction is a focused test for GUD-002 /
// AC-008. A broader regex coverage lives in TestParseIdentityFromShell.
func TestHostDiscoveryTopology_SecretRedaction(t *testing.T) {
	got := parseIdentityFromShell("user||alice\n" +
		"GITHUB_TOKEN=xyz\nAPI_KEY=shh\nDB_PASSWORD=no\nCLIENT_SECRET=no\n" +
		"FOO_BAR=keep\n")

	banned := []string{"GITHUB_TOKEN", "API_KEY", "DB_PASSWORD", "CLIENT_SECRET"}
	for _, k := range banned {
		if _, ok := got.Env[k]; ok {
			t.Errorf("banned key %q leaked", k)
		}
	}
	if got.Env["FOO_BAR"] != "keep" {
		t.Errorf("benign key FOO_BAR lost; env=%+v", got.Env)
	}
}

// TestScanPathForExecutables verifies $PATH scanning semantics: first-hit
// wins dedup, executable-bit filtering, directory rejection, name filtering,
// deterministic sort, override application, and the cap.
func TestScanPathForExecutables(t *testing.T) {
	tmp := t.TempDir()
	dirA := filepath.Join(tmp, "a")
	dirB := filepath.Join(tmp, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	// dirA: git (exec), README (not exec), subdir/ (directory), bad-name with
	// invalid char, .hidden (leading dot rejected).
	mustWrite(t, filepath.Join(dirA, "git"), 0o755)
	mustWrite(t, filepath.Join(dirA, "README"), 0o644)
	if err := os.Mkdir(filepath.Join(dirA, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	mustWrite(t, filepath.Join(dirA, "bad$name"), 0o755)
	mustWrite(t, filepath.Join(dirA, ".hidden"), 0o755)
	// dirB: git (should lose to dirA — first-hit-wins), go, ssh.
	mustWrite(t, filepath.Join(dirB, "git"), 0o755)
	mustWrite(t, filepath.Join(dirB, "go"), 0o755)
	mustWrite(t, filepath.Join(dirB, "ssh"), 0o755)

	overrides := map[string]string{"go": "version", "ssh": "-V"}
	got := scanPathForExecutables(dirA+string(os.PathListSeparator)+dirB, overrides, 0)

	want := []hostProbeDef{
		{Name: "git"},
		{Name: "go", VersionFlag: "version"},
		{Name: "ssh", VersionFlag: "-V"},
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (got=%+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("probe[%d] = %+v, want %+v", i, got[i], w)
		}
	}

	// Cap truncates.
	capped := scanPathForExecutables(dirA+string(os.PathListSeparator)+dirB, overrides, 2)
	if len(capped) != 2 {
		t.Errorf("cap=2 len=%d", len(capped))
	}
}

func mustWrite(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLooksLikeCommandName pins the filter so we don't accidentally broaden
// or narrow it.
func TestLooksLikeCommandName(t *testing.T) {
	accept := []string{"git", "g++", "python3", "docker-compose", "node_exporter", "my.tool", "A"}
	reject := []string{"", ".hidden", "bad name", "bad|name", "bad$name", "bad/name"}
	for _, s := range accept {
		if !looksLikeCommandName(s) {
			t.Errorf("expected accept: %q", s)
		}
	}
	for _, s := range reject {
		if looksLikeCommandName(s) {
			t.Errorf("expected reject: %q", s)
		}
	}
}

// TestDefaultHostProbeResolver_IsDeterministic ensures two consecutive calls
// return identical slices (same order, same content). PATH contents don't
// change between adjacent calls, so determinism must hold.
func TestDefaultHostProbeResolver_IsDeterministic(t *testing.T) {
	a := DefaultHostProbeResolver()
	b := DefaultHostProbeResolver()
	if len(a) != len(b) {
		t.Fatalf("len differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("[%d] %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestHostDiscoveryTopology_RebootFallbackMachineID verifies that an empty
// machine-id triggers the deterministic hostname fallback.
func TestHostDiscoveryTopology_RebootFallbackMachineID(t *testing.T) {
	got := parseIdentityFromShell("user||bob\nhostname||myhost\nmachine_id||\n")
	if got.MachineID == "" {
		t.Fatal("expected fallback machine id, got empty")
	}
	if got.MachineIDSource != "fallback" {
		t.Errorf("source = %q, want fallback", got.MachineIDSource)
	}
	if !strings.HasPrefix(got.MachineID, "fallback-") {
		t.Errorf("machine_id = %q, want fallback- prefix", got.MachineID)
	}
}
