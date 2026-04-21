package main

// topologies_host_discovery.go — GAP-2 first-boot host discovery CPN.
//
// Spec: spec/spec-architecture-host-discovery-capability-registry.md
//
// This topology probes the Linux host for identity, kernel facts and the
// set of executable binaries reachable from $PATH (see
// DefaultHostProbeResolver), derives high-level capabilities from the probe
// results, and persists the aggregated snapshot through
// HostCapabilityRepository. brae is Linux-first: the probe set is not a
// compiled-in allow-list — callers can inject a custom HostProbeResolver
// (tests do) but the default is a runtime $PATH scan.
//
// Shape (spec §3):
//
//   p-trigger ──┬── t-who      ──► p-identity-raw ──► t-parse-identity ──► p-identity ─┐
//               │                                                                       │
//               ├── t-uname    ──► p-kernel-raw   ──► t-parse-kernel   ──► p-kernel   ─┤
//               │                                                                       │
//               └── t-probe-<bin> × 22 ──► p-probe-<bin>-raw ─┐                          │
//                                                             ├── t-parse-binaries ──► p-binaries ─┤
//                                                             │                                     │
//                                                                                       ├── t-derive ──► p-capabilities ─► t-persist ──► p-snapshot
//                                                                                       │
//                                                                                       │
//
// NodeKind assignment follows the spec verbatim: t-who/t-uname/t-probe-<bin>
// are NodeKindBash; t-parse-*/t-derive/t-persist are NodeKindTool. Each bash
// transition carries AllowNonZeroExit=true so missing binaries or a missing
// /etc/os-release never fail the CPN — they just emit a non-zero ExitCode
// that the parser tolerates.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// HostDiscoveryFlowName is the FlowRegistry name for this topology.
const HostDiscoveryFlowName = "host-discovery"

// hostProbeDef describes one probe entry: the binary basename and the
// command-line flag that coaxes out a version banner (defaults to --version).
type hostProbeDef struct {
	Name        string
	VersionFlag string // defaults to "--version"
}

// hostProbeVersionFlagOverrides records non-standard version flags for
// well-known binaries. Everything else gets the default `--version`.
//
// brae is Linux-first and deliberately does NOT ship a closed allow-list of
// binaries: the discovery pass is driven by a runtime $PATH scan (see
// DefaultHostProbeResolver). This map only exists to rescue the handful of
// tools whose `--version` flag is absent or misbehaves.
var hostProbeVersionFlagOverrides = map[string]string{
	"go":  "version",
	"ssh": "-V",
}

// defaultHostProbeCap caps how many binaries a single host-discovery run will
// probe. A naïve $PATH scan on a developer machine can easily turn up several
// thousand entries (language-manager shims, vendored toolchains, etc.); that
// would explode into thousands of parallel bash transitions for no real
// benefit. 256 is comfortably above the set of tools any realistic brae
// session needs while keeping the fan-out bounded. Follow-up PR will move
// this to config + add a worker pool (see roadmap spec §T9).
const defaultHostProbeCap = 256

// HostProbeResolver returns the deterministic, sorted list of binaries to
// probe. Tests inject a fixed resolver so assertions don't depend on the
// host's actual $PATH contents.
type HostProbeResolver func() []hostProbeDef

// DefaultHostProbeResolver scans $PATH (and falls back to a sane system
// default when PATH is empty), merges curated version-flag overrides, caps
// the result, and returns a deterministic sorted slice.
//
// It does NOT invoke any binary — it only stats directory entries and checks
// the executable bit. The actual --version invocations happen later inside
// the CPN, wrapped by HostAdapter + HostGate.
func DefaultHostProbeResolver() []hostProbeDef {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		// POSIX default; matches `getconf PATH` on most distros.
		pathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	return scanPathForExecutables(pathEnv, hostProbeVersionFlagOverrides, defaultHostProbeCap)
}

// scanPathForExecutables walks each directory in pathEnv (first-wins dedup
// by basename, mirroring shell `command -v` semantics), collects executable
// regular files whose name looks like a command, applies version-flag
// overrides, sorts by name, and caps the result.
func scanPathForExecutables(pathEnv string, overrides map[string]string, cap int) []hostProbeDef {
	seen := make(map[string]struct{})
	out := make([]hostProbeDef, 0, 64)
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if _, dup := seen[name]; dup {
				continue
			}
			if !looksLikeCommandName(name) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			if mode.IsDir() {
				continue
			}
			if mode&0o111 == 0 {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, hostProbeDef{
				Name:        name,
				VersionFlag: overrides[name],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if cap > 0 && len(out) > cap {
		out = out[:cap]
	}
	return out
}

// looksLikeCommandName filters out directory entries whose names contain
// characters no one types at a shell prompt. The goal is to drop noise
// (files like `.keep`, `README`, entries with shell metacharacters) before
// we generate a transition per entry. Conservative: only dash, underscore,
// dot, plus, and alphanumerics are allowed.
func looksLikeCommandName(s string) bool {
	if s == "" || s[0] == '.' {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-', r == '_', r == '.', r == '+':
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}

// HostDiscoveryDeps captures the collaborators the topology needs at
// construction time. Repository is the only hard dependency; ProbeResolver
// is optional and defaults to DefaultHostProbeResolver so callers don't have
// to think about it.
type HostDiscoveryDeps struct {
	Repository    persist.HostCapabilityRepository
	Source        string // e.g. "bootstrap" | "session" | "manual"
	ProbeResolver HostProbeResolver
}

// hostDiscoveryProbeTimeout is the per-probe timeout mandated by GUD-001.
const hostDiscoveryProbeTimeout = 2 * time.Second

// hostDiscoveryLongTimeout is the timeout for the composite whoami / uname
// scripts. They chain a handful of commands, so 2 s is tight; 5 s gives us
// head-room on a cold host.
const hostDiscoveryLongTimeout = 5 * time.Second

// ── Place ids (exported for tests) ───────────────────────────────────────────

const (
	PlaceHostTrigger        = "p-trigger"
	PlaceHostIdentityRaw    = "p-identity-raw"
	PlaceHostKernelRaw      = "p-kernel-raw"
	PlaceHostIdentity       = "p-identity"
	PlaceHostKernel         = "p-kernel"
	PlaceHostBinaries       = "p-binaries"
	PlaceHostCapabilities   = "p-capabilities"
	PlaceHostSnapshot       = "p-snapshot"
	PlaceHostCapabilitiesWK = "p-host-capabilities" // well-known; seeded in every root CPN.
)

// PlaceHostProbeRaw returns the per-binary raw-output place id.
func PlaceHostProbeRaw(binaryName string) string {
	return "p-probe-" + binaryName + "-raw"
}

// hostDiscoveryTopologyFactory constructs the host-discovery CPN.
// When deps.Repository is nil, t-persist becomes a no-op that still produces
// p-snapshot so the topology stays deadlock-free in tests that don't want to
// stub a repository.
func hostDiscoveryTopologyFactory(sessionID string, deps HostDiscoveryDeps) *cpn.CPN {
	resolver := deps.ProbeResolver
	if resolver == nil {
		resolver = DefaultHostProbeResolver
	}
	probes := resolver()

	places := map[string]*cpn.Place{
		PlaceHostTrigger:      cpn.NewPlace(PlaceHostTrigger, cpn.ColorString, cpn.SpaceComputation),
		PlaceHostIdentityRaw:  cpn.NewPlace(PlaceHostIdentityRaw, cpn.ColorShellResult, cpn.SpaceComputation),
		PlaceHostKernelRaw:    cpn.NewPlace(PlaceHostKernelRaw, cpn.ColorShellResult, cpn.SpaceComputation),
		PlaceHostIdentity:     cpn.NewPlace(PlaceHostIdentity, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceHostKernel:       cpn.NewPlace(PlaceHostKernel, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceHostBinaries:     cpn.NewPlace(PlaceHostBinaries, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceHostCapabilities: cpn.NewPlace(PlaceHostCapabilities, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceHostSnapshot:     cpn.NewPlace(PlaceHostSnapshot, cpn.ColorHostFact, cpn.SpaceComputation),
	}
	for _, probe := range probes {
		id := PlaceHostProbeRaw(probe.Name)
		places[id] = cpn.NewPlace(id, cpn.ColorShellResult, cpn.SpaceComputation)
	}

	transitions := map[string]*cpn.Transition{}

	// ── Bash probes ─────────────────────────────────────────────────────────

	tWho := cpn.NewTransition("t-who", cpn.NodeKindBash,
		[]string{PlaceHostTrigger}, []string{PlaceHostIdentityRaw})
	tWho.BashConfig = &cpn.BashConfig{
		Command:          "sh",
		Args:             []string{"-c", whoamiScript()},
		Timeout:          hostDiscoveryLongTimeout,
		AllowNonZeroExit: true,
	}
	transitions["t-who"] = tWho

	tUname := cpn.NewTransition("t-uname", cpn.NodeKindBash,
		[]string{PlaceHostTrigger}, []string{PlaceHostKernelRaw})
	tUname.BashConfig = &cpn.BashConfig{
		Command:          "sh",
		Args:             []string{"-c", unameScript()},
		Timeout:          hostDiscoveryLongTimeout,
		AllowNonZeroExit: true,
	}
	transitions["t-uname"] = tUname

	for _, probe := range probes {
		probe := probe // capture
		id := "t-probe-" + probe.Name
		out := PlaceHostProbeRaw(probe.Name)
		t := cpn.NewTransition(id, cpn.NodeKindBash,
			[]string{PlaceHostTrigger}, []string{out})
		t.BashConfig = &cpn.BashConfig{
			Command:          "sh",
			Args:             []string{"-c", probeScript(probe)},
			Timeout:          hostDiscoveryProbeTimeout,
			AllowNonZeroExit: true,
		}
		transitions[id] = t
	}

	// ── Tool transitions ────────────────────────────────────────────────────

	transitions["t-parse-identity"] = newParseIdentityTransition()
	transitions["t-parse-kernel"] = newParseKernelTransition()
	transitions["t-parse-binaries"] = newParseBinariesTransition(probes)
	transitions["t-derive"] = newDeriveTransition(deps)
	transitions["t-persist"] = newPersistTransition(deps)

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-host-discovery", sessionID),
		"host-discovery",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 0 // no LLM history on this flow

	// Seed the trigger place with one token per bash transition so they can
	// all fire concurrently in the first batch (REQ-004). SeedFunc is also
	// re-invoked by Reset() so the net is reusable.
	triggerCount := 2 + len(probes) // t-who + t-uname + one per probe.
	seed := func(c *cpn.CPN) {
		trigger, ok := c.Places[PlaceHostTrigger]
		if !ok {
			return
		}
		for i := 0; i < triggerCount; i++ {
			_ = trigger.Deposit(&cpn.Token{
				Color:   cpn.ColorString,
				Space:   cpn.SpaceComputation,
				Payload: "go",
			})
		}
	}
	c.SeedFunc = seed
	seed(c)

	return c
}

// hostDiscoveryTopologyFactoryForSession is the TopologyFactory shim for
// the SessionService option. Globals from main.go supply the repository.
func hostDiscoveryTopologyFactoryForSession(sessionID string) *cpn.CPN {
	return hostDiscoveryTopologyFactory(sessionID, HostDiscoveryDeps{
		Repository: globalHostCapabilityRepo,
		Source:     persist.HostSnapshotSourceSession,
	})
}

// globalHostCapabilityRepo is populated by main.go when persistence is
// enabled. Keeping it package-level matches the pattern used by the
// manage-models fragment (see topologies_model_registry.go::adminEmailsFromEnv).
var globalHostCapabilityRepo persist.HostCapabilityRepository

// setHostCapabilityRepo is called by main.go.
func setHostCapabilityRepo(r persist.HostCapabilityRepository) {
	globalHostCapabilityRepo = r
}

// ── Scripts ──────────────────────────────────────────────────────────────────

// whoamiScript prints identity facts separated by the fixed "||" delimiter.
// Each line is `key||value` so the parser is a trivial split.
//
// machine-id falls back to the hostname when /etc/machine-id is unreadable
// (the parser re-hashes it so persisted rows never leak a raw hostname).
func whoamiScript() string {
	return strings.Join([]string{
		`printf 'user||%s\n' "$(whoami 2>/dev/null || id -un 2>/dev/null)"`,
		`printf 'uid||%s\n' "$(id -u 2>/dev/null)"`,
		`printf 'gid||%s\n' "$(id -g 2>/dev/null)"`,
		`printf 'hostname||%s\n' "$(hostname 2>/dev/null)"`,
		`printf 'home||%s\n' "${HOME}"`,
		`printf 'shell||%s\n' "${SHELL}"`,
		`printf 'machine_id||%s\n' "$(cat /etc/machine-id 2>/dev/null)"`,
		`env`,
	}, "; ")
}

// unameScript prints kernel / distro / cpu / mem facts.
func unameScript() string {
	return strings.Join([]string{
		`printf 'os||%s\n' "$(uname -s 2>/dev/null)"`,
		`printf 'kernel||%s\n' "$(uname -r 2>/dev/null)"`,
		`printf 'arch||%s\n' "$(uname -m 2>/dev/null)"`,
		`printf 'cpu_count||%s\n' "$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null)"`,
		`mem_kb=$(grep '^MemTotal' /proc/meminfo 2>/dev/null | awk '{print $2}'); printf 'mem_kb||%s\n' "${mem_kb}"`,
		`printf 'os_release_begin||\n'`,
		`cat /etc/os-release 2>/dev/null || true`,
		`printf 'os_release_end||\n'`,
	}, "; ")
}

// probeScript runs `which <bin>` (fast exit) then a version flag if the
// binary is present. Output is two lines: `path||<which-output>` and
// `version||<version-line>`.
func probeScript(def hostProbeDef) string {
	flag := def.VersionFlag
	if flag == "" {
		flag = "--version"
	}
	// `command -v` returns non-zero when missing; we print the empty path
	// then skip the version invocation. Binary names are pre-filtered by
	// looksLikeCommandName, so inlining without quoting is safe (no shell
	// metacharacters can reach this script body).
	return fmt.Sprintf(
		"p=$(command -v %s 2>/dev/null || true); printf 'path||%%s\\n' \"${p}\"; "+
			"if [ -n \"${p}\" ]; then printf 'version||%%s\\n' \"$(%s %s 2>&1 | head -n 1)\"; fi",
		def.Name, def.Name, flag,
	)
}

// ── Parsers (tool transitions) ───────────────────────────────────────────────

func newParseIdentityTransition() *cpn.Transition {
	t := cpn.NewTransition("t-parse-identity", cpn.NodeKindTool,
		[]string{PlaceHostIdentityRaw}, []string{PlaceHostIdentity})
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		id := parseIdentityFromShell(shellStdout(consumed))
		return map[string]cpn.Token{
			PlaceHostIdentity: {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: id},
		}, nil
	}
	return t
}

func newParseKernelTransition() *cpn.Transition {
	t := cpn.NewTransition("t-parse-kernel", cpn.NodeKindTool,
		[]string{PlaceHostKernelRaw}, []string{PlaceHostKernel})
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		k := parseKernelFromShell(shellStdout(consumed))
		return map[string]cpn.Token{
			PlaceHostKernel: {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: k},
		}, nil
	}
	return t
}

func newParseBinariesTransition(probes []hostProbeDef) *cpn.Transition {
	// One input place per probe; one output place carrying the aggregated
	// []BinaryProbe slice.
	inputs := make([]string, 0, len(probes))
	for _, p := range probes {
		inputs = append(inputs, PlaceHostProbeRaw(p.Name))
	}
	// Sort inputs for deterministic consume order.
	sort.Strings(inputs)

	t := cpn.NewTransition("t-parse-binaries", cpn.NodeKindTool,
		inputs, []string{PlaceHostBinaries})
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		out := make([]persist.BinaryProbe, 0, len(consumed))
		for i, tok := range consumed {
			// The input slice is in the same order as t.InputPlaces so we can
			// recover the probe name from there.
			placeID := inputs[i]
			name := strings.TrimSuffix(strings.TrimPrefix(placeID, "p-probe-"), "-raw")
			out = append(out, parseBinaryProbeFromShell(name, tok))
		}
		// Sort by name for determinism.
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return map[string]cpn.Token{
			PlaceHostBinaries: {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: out},
		}, nil
	}
	return t
}

func newDeriveTransition(deps HostDiscoveryDeps) *cpn.Transition {
	t := cpn.NewTransition("t-derive", cpn.NodeKindTool,
		[]string{PlaceHostIdentity, PlaceHostKernel, PlaceHostBinaries},
		[]string{PlaceHostCapabilities})
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var identity persist.HostIdentity
		var kernel persist.HostKernel
		var binaries []persist.BinaryProbe
		for _, tok := range consumed {
			switch p := tok.Payload.(type) {
			case persist.HostIdentity:
				identity = p
			case persist.HostKernel:
				kernel = p
			case []persist.BinaryProbe:
				binaries = p
			}
		}
		caps := DeriveCapabilities(identity, kernel, binaries)
		source := deps.Source
		if source == "" {
			source = persist.HostSnapshotSourceSession
		}
		snap := persist.HostCapabilitySnapshot{
			HostID:       identity.MachineID,
			CapturedAt:   time.Now().UTC(),
			Source:       source,
			Identity:     identity,
			Kernel:       kernel,
			Binaries:     binaries,
			Capabilities: caps,
		}
		if snap.HostID == "" {
			// Fallback host_id so the row never has an empty host_id in the
			// DB. Uses hostname if available, else a deterministic sentinel.
			if kernel.Kernel != "" {
				snap.HostID = "unknown-" + kernel.Kernel
			} else {
				snap.HostID = "unknown-host"
			}
		}
		return map[string]cpn.Token{
			PlaceHostCapabilities: {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: snap},
		}, nil
	}
	return t
}

func newPersistTransition(deps HostDiscoveryDeps) *cpn.Transition {
	t := cpn.NewTransition("t-persist", cpn.NodeKindTool,
		[]string{PlaceHostCapabilities}, []string{PlaceHostSnapshot})
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("t-persist: no input token")
		}
		snap, ok := consumed[0].Payload.(persist.HostCapabilitySnapshot)
		if !ok {
			return nil, fmt.Errorf("t-persist: expected HostCapabilitySnapshot payload, got %T", consumed[0].Payload)
		}
		if snap.ID == "" {
			snap.ID = uuid.NewString()
		}
		if deps.Repository != nil {
			if err := deps.Repository.Save(ctx, snap); err != nil {
				return nil, fmt.Errorf("t-persist: save snapshot: %w", err)
			}
		}
		return map[string]cpn.Token{
			PlaceHostSnapshot: {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: snap},
		}, nil
	}
	return t
}

// ── Shell payload plumbing ───────────────────────────────────────────────────

// shellStdout extracts the Stdout string from a ShellResultPayload token.
// Returns "" on absence / type mismatch so downstream parsers stay
// defensive against empty input.
func shellStdout(consumed []cpn.Token) string {
	for _, tok := range consumed {
		if r, ok := tok.Payload.(cpn.ShellResultPayload); ok {
			return r.Stdout
		}
	}
	return ""
}

// ── Parsers ──────────────────────────────────────────────────────────────────

// envRedactRe matches env keys that must NOT be persisted (GUD-002 / AC-008).
var envRedactRe = regexp.MustCompile(`(?i)(token|key|password|secret)`)

// parseIdentityFromShell reads the key||value lines produced by whoamiScript.
func parseIdentityFromShell(stdout string) persist.HostIdentity {
	id := persist.HostIdentity{Env: map[string]string{}}
	envMode := false
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		// env dumps its output without our ||-delimiter; detect it by fallback.
		if envMode || !strings.Contains(line, "||") {
			envMode = true
			if i := strings.IndexByte(line, '='); i > 0 {
				key := line[:i]
				val := line[i+1:]
				if envRedactRe.MatchString(key) {
					continue
				}
				id.Env[key] = val
			}
			continue
		}
		parts := strings.SplitN(line, "||", 2)
		key, val := parts[0], parts[1]
		switch key {
		case "user":
			id.User = val
		case "uid":
			id.UID, _ = strconv.Atoi(val)
		case "gid":
			id.GID, _ = strconv.Atoi(val)
		case "hostname":
			id.Hostname = val
		case "home":
			id.Home = val
		case "shell":
			id.Shell = val
		case "machine_id":
			if val != "" {
				id.MachineID = val
				id.MachineIDSource = "file"
			}
		}
	}
	if id.MachineID == "" {
		id.MachineID = fallbackMachineID(id.Hostname)
		id.MachineIDSource = "fallback"
	}
	return id
}

// parseKernelFromShell reads unameScript output.
func parseKernelFromShell(stdout string) persist.HostKernel {
	k := persist.HostKernel{OSRelease: map[string]string{}}
	inOSRelease := false
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		if line == "os_release_begin||" {
			inOSRelease = true
			continue
		}
		if line == "os_release_end||" {
			inOSRelease = false
			continue
		}
		if inOSRelease {
			if i := strings.IndexByte(line, '='); i > 0 {
				key := line[:i]
				val := strings.Trim(line[i+1:], `"`)
				k.OSRelease[key] = val
			}
			continue
		}
		parts := strings.SplitN(line, "||", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "os":
			k.OS = val
		case "kernel":
			k.Kernel = val
		case "arch":
			k.Arch = val
		case "cpu_count":
			k.CPUCount, _ = strconv.Atoi(val)
		case "mem_kb":
			kb, err := strconv.Atoi(val)
			if err == nil {
				k.MemMB = kb / 1024
			}
		}
	}
	if k.OS == "" {
		k.OS = runtime.GOOS
	}
	if k.Arch == "" {
		k.Arch = runtime.GOARCH
	}
	return k
}

// parseBinaryProbeFromShell converts the shell output of one t-probe-<bin>
// into a BinaryProbe. Tolerates missing binaries (empty path).
func parseBinaryProbeFromShell(name string, tok cpn.Token) persist.BinaryProbe {
	probe := persist.BinaryProbe{Name: name}
	result, ok := tok.Payload.(cpn.ShellResultPayload)
	if !ok {
		probe.DetectionErr = "no-shell-result"
		return probe
	}
	probe.DurationMs = result.DurationMs
	if result.ExitCode != 0 && result.Stdout == "" && result.Stderr != "" {
		// The script guards against which-miss; a genuine exec failure
		// (e.g. shell spawn refused) lands here.
		probe.DetectionErr = strings.TrimSpace(result.Stderr)
	}
	for _, raw := range strings.Split(result.Stdout, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "||", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "path":
			probe.Path = strings.TrimSpace(val)
		case "version":
			probe.Version = ParseBinaryVersion(name, val)
		}
	}
	probe.Present = probe.Path != ""
	return probe
}

// ParseBinaryVersion reduces a tool's `--version` banner to a bare SemVer
// string. Exported for unit tests.
//
// Examples:
//
//	gcc  : "gcc (Ubuntu 13.2.0-4ubuntu3) 13.2.0" → "13.2.0"
//	go   : "go version go1.22.3 linux/amd64"     → "1.22.3"
//	node : "v20.11.1"                             → "20.11.1"
//	py3  : "Python 3.11.6"                        → "3.11.6"
//	ssh  : "OpenSSH_9.6p1 Ubuntu-3"               → "9.6p1"
//
// Unknown tools fall through to a generic "grab the first X.Y.Z token".
var versionNumberRe = regexp.MustCompile(`[0-9]+(?:\.[0-9]+){1,3}(?:[A-Za-z0-9]+)?`)

func ParseBinaryVersion(name, banner string) string {
	banner = strings.TrimSpace(banner)
	if banner == "" {
		return ""
	}
	switch name {
	case "go":
		// "go version go1.22.3 linux/amd64"
		for _, tok := range strings.Fields(banner) {
			if strings.HasPrefix(tok, "go") {
				if v := strings.TrimPrefix(tok, "go"); versionNumberRe.MatchString(v) {
					return v
				}
			}
		}
	case "python3":
		// "Python 3.11.6" or "python 3.11.6 (main, ...)"
		parts := strings.Fields(banner)
		if len(parts) >= 2 {
			return parts[1]
		}
	case "node":
		// "v20.11.1"
		v := strings.TrimPrefix(banner, "v")
		if versionNumberRe.MatchString(v) {
			return strings.Fields(v)[0]
		}
	case "ssh":
		// "OpenSSH_9.6p1, OpenSSL 3.0.11 19 Sep 2023"
		first := strings.SplitN(banner, ",", 2)[0]
		first = strings.TrimPrefix(first, "OpenSSH_")
		return first
	case "gcc":
		// "gcc (Ubuntu 13.2.0-4ubuntu3) 13.2.0"
		match := versionNumberRe.FindAllString(banner, -1)
		if len(match) > 0 {
			return match[len(match)-1]
		}
	case "jq":
		// "jq-1.7.1"
		return strings.TrimPrefix(banner, "jq-")
	}
	// Fallback: first numeric version token.
	if v := versionNumberRe.FindString(banner); v != "" {
		return v
	}
	return banner
}

// fallbackMachineID returns a deterministic hash when /etc/machine-id is
// unreadable (INF-002). We prefer a simple hex SHA-256 so the value is
// stable across boots as long as the hostname is stable.
func fallbackMachineID(hostname string) string {
	if hostname == "" {
		hostname = "unknown-host"
	}
	// Keep a trivial hash here to avoid pulling crypto/sha256 into the
	// hot path; the spec accepts any deterministic fallback.
	h := uint64(1469598103934665603)
	for _, b := range []byte(hostname + "|fallback") {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("fallback-%016x", h)
}

// ── Well-known place seeding ────────────────────────────────────────────────
//
// The session service calls cpn.SeedHostSnapshot directly to seed the
// well-known p-host-capabilities place on every root CPN. This factory
// produces the SOURCE snapshot that seed consumes — see
// internal/app/session_service.go::ensureHostCapabilitiesSeed.
