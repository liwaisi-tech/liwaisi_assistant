// Package fanout implements the minimal probe-fanout composer defined in
// spec-architecture-brae-awakening-probe-fanout.md. Given an
// AwakeningProbePlan emitted by the awakening LLM on its first turn, Compose
// builds an N-branch parallel sub-CPN — one NodeKindTool per probe plus a
// single aggregator transition — and returns it ready for Run.
//
// The package deliberately owns a small, self-contained surface:
//
//   - No import of cpn/persist or any store (CON-001, Axiom A13).
//   - No NodeKindInstantiate / NodeKindSubNet in the emitted sub-CPN
//     (CON-004).
//   - Deterministic output keyed on sorted probe IDs (NFR-002).
//
// The AwakeningReport type below intentionally mirrors only the fields the
// probe-fanout reducer is responsible for assembling. Conversion to the
// persisted cpn/awakens.AwakeningReport shape lives in the parent awakening
// topology, outside this package, so cpn/awakens/fanout stays free of
// persistence imports.
package fanout

import (
	"context"
	"errors"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// MaxProbes is the fanout hard cap (CON-002).
//
// Composed plans total = LLM-emitted probes + mandatoryInfoProbes (8 entries).
// The LLM prompt bounds its own emission to MaxPlanProbes so the union always
// fits under MaxProbes.
const MaxProbes = 24

// MaxPlanProbes caps the LLM-emitted portion of the plan. The prompt enforces
// this number so the composer-injected info probes (metadata: OS, shell,
// identity) always have room below the MaxProbes ceiling.
const MaxPlanProbes = 12

// MaxCommandLen is the per-probe command size ceiling (CON-003).
const MaxCommandLen = 256

// DefaultPerProbeTimeout is the ceiling applied when a plan either omits
// TimeoutPerProbeMs or requests a value above 2s. Per REQ-004 the effective
// timeout is min(2s, plan.TimeoutPerProbeMs/1000).
const DefaultPerProbeTimeout = 2 * time.Second

// MinPerProbeTimeout is the floor applied when a plan requests a value below
// 100 ms.
const MinPerProbeTimeout = 100 * time.Millisecond

// ProbeKindBinary classifies a probe as a binary-presence check.
const ProbeKindBinary = "binary"

// ProbeKindCapability classifies a probe as a derived capability check.
const ProbeKindCapability = "capability"

// ProbeKindInfo classifies a probe as a metadata probe (OS, shell, identity).
// Info probes are composer-injected rather than LLM-emitted — the composer
// always appends a fixed set so the report reliably carries
// AwakeningReportOS/Shell/Identity even when the LLM plan omits them.
const ProbeKindInfo = "info"

// Well-known info-probe targets consumed by the reducer. Values are the
// canonical target string; the reducer switches on these to populate the
// structured OS / Shell / Identity fields of AwakeningReport.
const (
	InfoTargetOSName     = "os.name"
	InfoTargetOSKernel   = "os.kernel"
	InfoTargetOSArch     = "os.arch"
	InfoTargetShellPath  = "shell.path"
	InfoTargetIdentity   = "identity.id"
	InfoTargetUser       = "identity.user"
	InfoTargetHome       = "identity.home"
	InfoTargetHostname   = "identity.hostname"
	InfoTargetBusyboxApp = "shell.implementation"
)

// TimeoutExitCode is the reserved ExitCode value the executor uses to signal
// "probe timed out" (REQ-004).
const TimeoutExitCode = -1

// GateDenyExitCode is the reserved ExitCode value the executor uses to
// signal "the host-gate denied this probe" (§9.2).
const GateDenyExitCode = -2

// AwakeningProbePlan is the first-turn LLM output per spec §4.1.
type AwakeningProbePlan struct {
	// Rationale is a one-sentence explanation of why the LLM chose these
	// probes. Stored in raw_probes metadata for observability.
	Rationale string `json:"rationale"`

	// TimeoutPerProbeMs bounds each probe. Composer clamps to
	// [MinPerProbeTimeout, DefaultPerProbeTimeout].
	TimeoutPerProbeMs int `json:"timeout_per_probe_ms"`

	// Probes enumerates the probes to run. Max MaxProbes entries (CON-002).
	Probes []AwakeningProbeEntry `json:"probes"`
}

// AwakeningProbeEntry is one probe in the plan per spec §4.1.
type AwakeningProbeEntry struct {
	// ID is a short stable slug. When empty or non-conformant the composer
	// derives one from Command (GUD-002).
	ID string `json:"id"`

	// Kind is one of ProbeKindBinary or ProbeKindCapability. Drives which
	// field of AwakeningReport receives the result.
	Kind string `json:"kind"`

	// Target is the binary name or capability name the probe verifies.
	Target string `json:"target"`

	// Command is the full POSIX command passed to sh -c. Must pass the
	// introspection gate. Max MaxCommandLen bytes (CON-003).
	Command string `json:"command"`
}

// AwakeningProbeResult is the per-branch output token payload per spec §4.2.
type AwakeningProbeResult struct {
	ProbeID    string `json:"probe_id"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
	GateDenied bool   `json:"gate_denied,omitempty"`
}

// AwakeningReportBinary is one entry in AwakeningReport.Binaries.
type AwakeningReportBinary struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Version string `json:"version,omitempty"`
}

// AwakeningReportCapability is one entry in AwakeningReport.Capabilities.
type AwakeningReportCapability struct {
	Name      string   `json:"name"`
	Satisfied bool     `json:"satisfied"`
	Evidence  []string `json:"evidence,omitempty"`
}

// AwakeningReportToolRegister is one entry in AwakeningReport.ToolsRegister.
// The fanout reducer leaves this empty: classification-to-registration is
// the LLM's job (a later turn), not the probe layer's.
type AwakeningReportToolRegister struct {
	Name  string `json:"name"`
	Basis string `json:"basis"`
}

// AwakeningReport is the reducer output emitted by the fanout sub-CPN. It
// carries the probe-derived slice of the final awakening snapshot. The
// OS/Shell/Identity fields are populated from composer-injected info probes
// (ProbeKindInfo) so the parent topology does not have to invent defaults.
// LLM-authored sections (ToolsRegister) are still merged by the parent.
type AwakeningReport struct {
	Binaries      []AwakeningReportBinary       `json:"binaries"`
	Capabilities  []AwakeningReportCapability   `json:"capabilities"`
	OS            AwakeningReportOS             `json:"os,omitempty"`
	Shell         AwakeningReportShell          `json:"shell,omitempty"`
	Identity      AwakeningReportIdentity       `json:"identity,omitempty"`
	Notes         []string                      `json:"notes,omitempty"`
	ToolsRegister []AwakeningReportToolRegister `json:"tools_register"`
}

// AwakeningReportOS carries the OS section the reducer fills from info probes.
type AwakeningReportOS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Kernel  string `json:"kernel,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

// AwakeningReportShell carries the shell section.
type AwakeningReportShell struct {
	Path           string `json:"path,omitempty"`
	Implementation string `json:"implementation,omitempty"`
}

// AwakeningReportIdentity carries the running-process identity.
type AwakeningReportIdentity struct {
	User     string `json:"user,omitempty"`
	UID      int    `json:"uid,omitempty"`
	GID      int    `json:"gid,omitempty"`
	Home     string `json:"home,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// Deps mirrors cpn/awakens.Deps but only with fields used during fanout.
// Clock defaults to time.Now when nil; the other fields surface structured
// errors from the executor when missing so the flow fails cleanly rather
// than silently.
type Deps struct {
	HostAdapter cpn.HostAdapter
	HostGate    cpn.HostGate
	Clock       func() time.Time
	// OnProbeFired is an optional observability callback invoked once per
	// probe completion (success, timeout, or gate-deny). A nil callback is
	// a no-op so callers outside the awakening topology (tests, composer
	// fuzzers) don't have to wire observability.
	OnProbeFired func(ctx context.Context, slug string, duration time.Duration, exitCode int)
	// OnProbeReduced is invoked once by the AND-join reducer with the
	// counts of success / fail probes.
	OnProbeReduced func(ctx context.Context, success, fail int)
}

// ── Sentinel errors ─────────────────────────────────────────────────────────

// ErrEmptyPlan is returned when ValidatePlan receives a plan with zero probes.
var ErrEmptyPlan = errors.New("fanout: plan has no probes")

// ErrTooManyProbes is returned when the plan exceeds MaxProbes (CON-002).
var ErrTooManyProbes = errors.New("fanout: plan exceeds max probes")

// ErrCommandTooLong is returned when a probe command exceeds MaxCommandLen
// (CON-003).
var ErrCommandTooLong = errors.New("fanout: probe command exceeds max length")

// ErrInvalidProbeKind is returned when a probe's Kind is not
// ProbeKindBinary or ProbeKindCapability.
var ErrInvalidProbeKind = errors.New("fanout: invalid probe kind")

// ErrInvalidProbe is a catch-all sentinel for structural defects in a probe
// entry (empty command, null byte, missing target).
var ErrInvalidProbe = errors.New("fanout: invalid probe entry")

// ── Place / transition ID prefixes ─────────────────────────────────────────

// Exported ID prefixes let the topology-wiring teammate reference the
// fanout sub-CPN's places without copying string literals.
const (
	// PlaceTriggerID is the single start-place seeded at compose time.
	PlaceTriggerID = "p-probe-trigger"

	// PlacePlanID is the plan-payload place seeded with the validated plan.
	PlacePlanID = "p-probe-plan"

	// PlaceReportID is the reducer's output place — the single terminal.
	PlaceReportID = "p-probe-report"

	// PlaceEgressID mirrors PlaceReportID for SubNet egress semantics
	// (PAT-002). The reducer deposits the report on both places so callers
	// can read either name.
	PlaceEgressID = "p-probe-egress"

	// TransitionProbePrefix is the prefix applied to per-probe transition
	// IDs. Full ID = TransitionProbePrefix + "<probe_id>".
	TransitionProbePrefix = "t-probe-"

	// PlaceProbeResultPrefix is the prefix applied to per-probe output
	// places. Full ID = PlaceProbeResultPrefix + "<probe_id>".
	PlaceProbeResultPrefix = "p-probe-result-"

	// TransitionReduceID is the aggregator transition ID.
	TransitionReduceID = "t-probe-reduce"
)
