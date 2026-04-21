package awakens

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// AwakeningReport is the LLM's structured self-discovery output (§4.1).
//
// The struct tolerates partial LLM responses — Validate() enforces the
// minimum fields required for a usable snapshot, but optional sections
// (capabilities, tools_to_register, probe_trace) may be empty without
// failing validation. A report that fails Validate() MUST be retried; the
// awakening flow has no fallback path.
type AwakeningReport struct {
	OS            AwakeningOS             `json:"os"`
	Shell         AwakeningShell          `json:"shell"`
	Identity      AwakeningIdentity       `json:"identity"`
	Host          AwakeningHost           `json:"host,omitempty"`
	PresentTools  []AwakeningTool         `json:"present_tools"`
	AbsentTools   []string                `json:"absent_tools"`
	Capabilities  []AwakeningCapability   `json:"capabilities"`
	ToolsRegister []AwakeningToolRegister `json:"tools_to_register"`
	NarrativeMD   string                  `json:"narrative_md"`
	ProbeTrace    []AwakeningProbe        `json:"probe_trace"`
}

// AwakeningHost carries environment-level facts that are not probe outputs.
// Today it only holds the SC-10 sandbox wrapper; future gap-closures (help-
// parser, capability ACL) will slot in here rather than bloating OS/Shell.
type AwakeningHost struct {
	Sandbox AwakeningSandbox `json:"sandbox,omitempty"`
}

// AwakeningSandbox is the REQ-1004 record of the probe-sandbox wrapper.
type AwakeningSandbox struct {
	Tool    string `json:"tool"`
	Version string `json:"version,omitempty"`
}

// AwakeningOS describes the operating system + kernel + arch.
type AwakeningOS struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kernel  string `json:"kernel"`
	Arch    string `json:"arch"`
}

// AwakeningShell identifies the active shell.
type AwakeningShell struct {
	Path           string `json:"path"`
	Implementation string `json:"implementation"`
}

// AwakeningIdentity is the running process identity.
type AwakeningIdentity struct {
	User string `json:"user"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
	Home string `json:"home"`
}

// AwakeningTool is one present binary.
type AwakeningTool struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Version  string `json:"version,omitempty"`
}

// AwakeningCapability is a derived boolean assertion.
type AwakeningCapability struct {
	Name      string   `json:"name"`
	Satisfied bool     `json:"satisfied"`
	Evidence  []string `json:"evidence,omitempty"`
}

// AwakeningToolRegister is one tool-forge registration request.
//
// Toolbox and Hashtags are additive per spec-architecture-brae-awakening-
// toolbox-extension.md REQ-001. Both are optional — empty values are valid
// (REQ-004); invalid hashtag tokens are silently dropped at registration
// time, not rejected here (REQ-007 cross-ref in the taxonomy spec).
type AwakeningToolRegister struct {
	Name     string   `json:"name"`
	Basis    string   `json:"basis"`
	Toolbox  string   `json:"toolbox,omitempty"`
	Hashtags []string `json:"hashtags,omitempty"`
}

// MaxHashtagsPerAwakeningTool caps hashtags per registered tool per REQ-011
// (spec-architecture-brae-awakening-toolbox-extension.md). When the LLM emits
// more, the normaliser deterministically keeps the first 6 after ascending
// sort and emits one personality.tooltags.truncated event per affected tool.
const MaxHashtagsPerAwakeningTool = 6

// AwakeningProbe is a single probe_trace entry.
type AwakeningProbe struct {
	Cmd  string `json:"cmd"`
	Exit int    `json:"exit"`
}

// ErrInvalidReport is returned by Validate() when a structural invariant is
// violated. Callers pass this back to the LLM as feedback (§9.4).
var ErrInvalidReport = errors.New("awakens: invalid awakening report")

// MaxToolsToRegister caps the batch registration per CON-004.
const MaxToolsToRegister = 20

// ToolNameRe restricts registration names to a conservative identifier set.
// We reject anything that could embed shell metachars or namespace separators
// that might be confused by ToolRegistry.
var toolNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// Validate enforces the minimum shape required for a useful snapshot.
// Returns a wrapped ErrInvalidReport on failure — the message is suitable
// for feeding back to the LLM as a retry hint.
func (r *AwakeningReport) Validate() error {
	if r == nil {
		return fmt.Errorf("%w: nil report", ErrInvalidReport)
	}
	if strings.TrimSpace(r.OS.Name) == "" {
		return fmt.Errorf("%w: os.name is required", ErrInvalidReport)
	}
	if strings.TrimSpace(r.OS.Arch) == "" {
		return fmt.Errorf("%w: os.arch is required", ErrInvalidReport)
	}
	if strings.TrimSpace(r.Shell.Path) == "" {
		return fmt.Errorf("%w: shell.path is required", ErrInvalidReport)
	}
	if len(r.ToolsRegister) > MaxToolsToRegister {
		return fmt.Errorf("%w: tools_to_register exceeds cap %d (got %d)",
			ErrInvalidReport, MaxToolsToRegister, len(r.ToolsRegister))
	}
	seen := make(map[string]struct{}, len(r.ToolsRegister))
	for _, tr := range r.ToolsRegister {
		if !toolNameRe.MatchString(tr.Name) {
			return fmt.Errorf("%w: tool-to-register name %q not a valid identifier",
				ErrInvalidReport, tr.Name)
		}
		if _, dup := seen[tr.Name]; dup {
			return fmt.Errorf("%w: duplicate tool-to-register %q", ErrInvalidReport, tr.Name)
		}
		seen[tr.Name] = struct{}{}
	}
	for _, tool := range r.PresentTools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("%w: present_tools[*].name is required", ErrInvalidReport)
		}
	}
	return nil
}

// ParseReport decodes the LLM's structured output and runs Validate().
// Tolerates prose-wrapped JSON (e.g. "I'll inspect... {json}" or markdown
// code fences) which Gemini and other models emit when RequireJSON cannot
// be set — OpenRouter rejects response_format=json_object together with
// tools, so we rely on prompt discipline + best-effort extraction.
func ParseReport(raw []byte) (AwakeningReport, error) {
	candidate := raw
	var r AwakeningReport
	if err := json.Unmarshal(candidate, &r); err != nil {
		extracted, ok := extractJSONObject(raw)
		if !ok {
			return AwakeningReport{}, fmt.Errorf("%w: decode: %v; raw[:%d]=%q",
				ErrInvalidReport, err, previewLen(raw), previewBytes(raw))
		}
		candidate = extracted
		if err := json.Unmarshal(candidate, &r); err != nil {
			return AwakeningReport{}, fmt.Errorf("%w: decode after extract: %v; extracted[:%d]=%q",
				ErrInvalidReport, err, previewLen(candidate), previewBytes(candidate))
		}
	}
	if err := r.Validate(); err != nil {
		return AwakeningReport{}, err
	}
	return r, nil
}

// previewLen caps the logged preview so we do not spill a 4 KB blob into
// structured logs on every decode error.
func previewLen(b []byte) int {
	if len(b) > 240 {
		return 240
	}
	return len(b)
}

func previewBytes(b []byte) []byte {
	return b[:previewLen(b)]
}

// extractJSONObject scans for the first balanced top-level JSON object in
// the raw LLM output. Skips markdown fences and leading prose. Returns the
// slice and true when a balanced {...} is found.
func extractJSONObject(raw []byte) ([]byte, bool) {
	start := -1
	depth := 0
	inString := false
	escape := false
	for i, b := range raw {
		if start == -1 {
			if b == '{' {
				start = i
				depth = 1
			}
			continue
		}
		if escape {
			escape = false
			continue
		}
		if b == '\\' && inString {
			escape = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch b {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], true
			}
		}
	}
	return nil, false
}

// envRedactRe matches env keys that must NOT be persisted (SEC-005/SEC-006).
var envRedactRe = regexp.MustCompile(`(?i)(token|key|password|secret|credential|auth)`)

// Project maps the AwakeningReport onto the legacy HostCapabilitySnapshot
// shape so existing readers of host_capability_snapshots (REQ-validation #6)
// keep working without migration.
//
// source is injected by the caller — first-boot awakening always passes
// "awakening" (SourceAwakening).
func (r AwakeningReport) Project(hostID, source string, capturedAt time.Time) persist.HostCapabilitySnapshot {
	// Build binaries from present_tools.
	binaries := make([]persist.BinaryProbe, 0, len(r.PresentTools)+len(r.AbsentTools))
	for _, pt := range r.PresentTools {
		binaries = append(binaries, persist.BinaryProbe{
			Name:    pt.Name,
			Present: true,
			Version: pt.Version,
		})
	}
	for _, absent := range r.AbsentTools {
		binaries = append(binaries, persist.BinaryProbe{
			Name:    absent,
			Present: false,
		})
	}

	caps := make([]persist.Capability, 0, len(r.Capabilities))
	for _, c := range r.Capabilities {
		caps = append(caps, persist.Capability{
			Name:        c.Name,
			Satisfied:   c.Satisfied,
			DerivedFrom: append([]string(nil), c.Evidence...),
		})
	}

	// Build identity. We intentionally do NOT persist env vars here
	// (SEC-005/SEC-006) — the legacy schema's env map stays empty under
	// the awakening path; secrets never cross this boundary.
	identity := persist.HostIdentity{
		MachineID:       hostID,
		MachineIDSource: "awakening",
		User:            r.Identity.User,
		UID:             r.Identity.UID,
		GID:             r.Identity.GID,
		Home:            r.Identity.Home,
		Shell:           r.Shell.Path,
		Env:             map[string]string{},
	}

	kernel := persist.HostKernel{
		OS:        r.OS.Name,
		Kernel:    r.OS.Kernel,
		Arch:      r.OS.Arch,
		OSRelease: map[string]string{},
	}
	if r.OS.Version != "" {
		kernel.OSRelease["VERSION_ID"] = r.OS.Version
	}

	probeTrace, _ := json.Marshal(r.ProbeTrace)

	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}

	return persist.HostCapabilitySnapshot{
		HostID:       hostID,
		CapturedAt:   capturedAt.UTC(),
		Source:       source,
		Identity:     identity,
		Kernel:       kernel,
		Binaries:     binaries,
		Capabilities: caps,
		RawProbes:    probeTrace,
	}
}

// RedactEnvKey reports whether an env key must be dropped before persistence.
// Exported so the awakening LLM turn's context assembly can share the logic.
func RedactEnvKey(k string) bool { return envRedactRe.MatchString(k) }
