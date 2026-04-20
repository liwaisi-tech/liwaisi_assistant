package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// awakeningDeadline is the hard upper-bound for the `brae-awakens` topology
// (CON-001). Exceeded → fallback to legacy host-discovery-cpn (AC-009).
const awakeningDeadline = 30 * time.Second

// AwakeningModeMarker is the minimal interface SessionService expects from
// an awakening-mode registry. Scoped per-session: Begin while the flow is
// running, End when it completes. Kept as an interface so infra/host/gate
// stays independent of internal/app at the type level.
type AwakeningModeMarker interface {
	Begin(sessionID string)
	End(sessionID string)
}

// AwakensFactory constructs the `brae-awakens` topology for a session.
type AwakensFactory func(sessionID string, deps awakens.Deps) *cpn.CPN

// WithAwakensFactory wires the `brae-awakens` topology factory. When set,
// new interactive sessions run the awakening flow before becoming usable.
// Nil disables the awakening path — legacy host-discovery-cpn continues to
// run as the pre-existing seed (CON-006).
func WithAwakensFactory(f AwakensFactory) SessionServiceOption {
	return func(s *SessionService) { s.awakensFactory = f }
}

// WithAwakeningMode wires the session-scoped awakening-mode registry used by
// the host-gate to silently deny non-introspection commands (CON-003, SEC-004,
// AC-005) while `brae-awakens` is driving a session.
func WithAwakeningMode(m AwakeningModeMarker) SessionServiceOption {
	return func(s *SessionService) { s.awakeningMode = m }
}

// runAwakening performs the brae-awakens first-turn boot flow for a fresh
// interactive session. It returns the projected snapshot + the first-turn
// A2UI envelope + the chosen source tag. On LLM-unreachable or timeout it
// falls through to the legacy host-discovery path (REQ-010, AC-004, AC-009).
//
// The method is synchronous by design: REQ-001 blocks session readiness on
// the awakening turn. It is bounded by awakeningDeadline so callers never
// hang past 30 seconds.
//
// When no awakensFactory / LLM / host-capability repo is wired, returns
// (zero, zero, nil) so CreateSession can fall back to legacy behaviour.
func (s *SessionService) runAwakening(ctx context.Context, sessionID string) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	var zero persist.HostCapabilitySnapshot
	if s.awakensFactory == nil || s.hostCapabilityRepo == nil {
		return zero, awakens.A2UIMessage{}, "", errAwakeningNotConfigured
	}

	hostID := s.resolveHostID(ctx)

	// REQ-009 / AC-008 — 24h cache hit skips shell probes but still emits
	// the first-turn card.
	if snap, fresh, err := awakens.LookupCachedSnapshot(ctx, s.hostCapabilityRepo, hostID, awakens.DefaultCacheTTL, awakens.SystemClock); err == nil && fresh {
		envelope := firstTurnMessageFromSnapshot(snap)
		s.logger.Info("awakening: served from 24h cache",
			slog.String("session_id", sessionID),
			slog.String("host_id", hostID),
			slog.String("source", snap.Source),
		)
		return snap, envelope, snap.Source, nil
	}

	// If no LLM is wired, the normal path cannot run — fall back directly.
	if s.llm == nil {
		return s.runAwakeningFallback(ctx, sessionID, hostID)
	}

	// Build and run the awakens CPN.
	deps := awakens.Deps{
		Repository: s.hostCapabilityRepo,
		HostID:     hostID,
		Source:     awakens.SourceAwakening,
		Clock:      awakens.SystemClock,
	}
	root := s.awakensFactory(sessionID, deps)
	if root == nil {
		return s.runAwakeningFallback(ctx, sessionID, hostID)
	}
	root.LLMClient = s.llm
	root.Cost = s.cost
	root.HostRuntime = s.hostRuntime
	if s.toolRegistry != nil {
		s.toolRegistry.InjectIntoCPN(root)
		root.ToolRegistry = s.toolRegistry
	}

	runCtx, cancel := context.WithTimeout(ctx, awakeningDeadline)
	defer cancel()
	runCtx = awakens.WithToolRegistry(runCtx, s.toolRegistry)

	// Flag the session as awakening-mode so the host-gate silently denies
	// any non-introspection command the LLM might emit.
	if s.awakeningMode != nil {
		s.awakeningMode.Begin(sessionID)
		defer s.awakeningMode.End(sessionID)
	}

	if err := root.Run(runCtx); err != nil {
		s.logger.Warn("awakening: LLM path failed; falling through to legacy discovery",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		return s.runAwakeningFallback(ctx, sessionID, hostID)
	}

	// Extract the emitted A2UI envelope and snapshot from the terminal places.
	envelope, ok := extractEmittedMessage(root)
	if !ok {
		s.logger.Warn("awakening: CPN completed but emitted no A2UI envelope; falling back",
			slog.String("session_id", sessionID))
		return s.runAwakeningFallback(ctx, sessionID, hostID)
	}
	snap, ok := extractAwakeningSnapshot(root)
	if !ok {
		// The CPN ran but no snapshot landed on p-host-capabilities —
		// legacy consumers depend on it, so fall back to populate one.
		s.logger.Warn("awakening: CPN completed but deposited no snapshot",
			slog.String("session_id", sessionID))
		return s.runAwakeningFallback(ctx, sessionID, hostID)
	}
	s.logger.Info("awakening: complete",
		slog.String("session_id", sessionID),
		slog.String("host_id", hostID),
		slog.String("source", awakens.SourceAwakening),
	)
	return snap, envelope, awakens.SourceAwakening, nil
}

// runAwakeningFallback runs a deterministic introspection probe through the
// host adapter (bypassing the LLM) and produces a populated snapshot + A2UI
// envelope. Shell commands are kept to the `introspection` safe band
// (uname, whoami, id, cat /etc/os-release, command -v X) and are issued
// directly through the adapter so HITL is never invoked.
//
// When the host runtime is not wired (tests) we degrade to a zero snapshot
// plus a minimal envelope so the session still becomes usable (BEH-003).
func (s *SessionService) runAwakeningFallback(ctx context.Context, sessionID, hostID string) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	if s.hostRuntime == nil || s.hostRuntime.Adapter == nil {
		s.logger.Info("awakening: LLM path unavailable; serving synthesized envelope",
			slog.String("session_id", sessionID),
			slog.String("host_id", hostID),
			slog.String("source", awakens.SourceAwakeningFallback),
		)
		return persist.HostCapabilitySnapshot{}, synthesizedFallbackEnvelope(), awakens.SourceAwakeningFallback, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	report := s.deterministicProbe(probeCtx)
	snap := report.Project(hostID, awakens.SourceAwakeningFallback, time.Now().UTC())
	if snap.ID == "" {
		snap.ID = uuid.NewString()
	}

	if s.hostCapabilityRepo != nil {
		if err := s.hostCapabilityRepo.Save(ctx, snap); err != nil {
			s.logger.Warn("awakening-fallback: save snapshot",
				slog.String("session_id", sessionID), slog.Any("error", err))
		}
	}

	envelope := awakens.BuildFirstTurnMessage(report)
	s.logger.Info("awakening: deterministic fallback probe complete",
		slog.String("session_id", sessionID),
		slog.String("host_id", hostID),
		slog.String("os", report.OS.Name),
		slog.String("arch", report.OS.Arch),
		slog.Int("present_tools", len(report.PresentTools)),
		slog.Int("absent_tools", len(report.AbsentTools)),
		slog.String("source", awakens.SourceAwakeningFallback),
	)
	return snap, envelope, awakens.SourceAwakeningFallback, nil
}

// deterministicProbe issues the small set of read-only commands in the
// `introspection` safe band directly through the host adapter and assembles
// an AwakeningReport. Bypasses the CPN / gate because these calls are
// internal bootstrap, not user-authored tool invocations.
func (s *SessionService) deterministicProbe(ctx context.Context) awakens.AwakeningReport {
	adapter := s.hostRuntime.Adapter

	run := func(cmd string, args ...string) (string, int) {
		res, err := adapter.Exec(ctx, cpn.ExecRequest{
			Command:      cmd,
			Args:         args,
			Timeout:      2 * time.Second,
			AllowNonZero: true,
		})
		if err != nil {
			return "", -1
		}
		return strings.TrimSpace(string(res.Stdout)), res.ExitCode
	}

	// Kernel / OS.
	kernelRel, _ := run("uname", "-r")
	arch, _ := run("uname", "-m")
	osReleaseOut, _ := run("cat", "/etc/os-release")
	osName, osVersion, _ := parseOSReleaseFile(osReleaseOut)

	// Identity.
	user, _ := run("whoami")
	uid, gid := 0, 0
	if out, exit := run("id", "-u"); exit == 0 {
		uid, _ = strconv.Atoi(out)
	}
	if out, exit := run("id", "-g"); exit == 0 {
		gid, _ = strconv.Atoi(out)
	}

	// Shell: /etc/passwd lookup for the user's login shell, fall back to /bin/sh.
	shell := "/bin/sh"
	if user != "" {
		if passwd, exit := run("cat", "/etc/passwd"); exit == 0 {
			if got := loginShellFromPasswd(passwd, user); got != "" {
				shell = got
			}
		}
	}

	// Binary probes — small shortlist of tools most commonly referenced.
	// `which` is in the host-gate safe band and succeeds (exit 0 + absolute
	// path on stdout) only when the binary is on PATH, so we get presence
	// without touching the path jail.
	probes := []string{
		"sh", "bash", "ash", "dash",
		"awk", "sed", "grep", "find", "tar", "gzip", "xz",
		"wget", "curl",
		"git", "make", "gcc", "cc",
		"python", "python3", "node", "npm", "go", "ruby", "perl",
		"sqlite3", "jq", "yq",
	}

	var present []awakens.AwakeningTool
	var absent []string
	var trace []awakens.AwakeningProbe

	for _, name := range probes {
		path, exit := run("which", name)
		if exit == 0 && path != "" {
			present = append(present, awakens.AwakeningTool{Name: name})
		} else {
			absent = append(absent, name)
		}
		trace = append(trace, awakens.AwakeningProbe{Cmd: "which " + name, Exit: exit})
	}

	if osName == "" {
		osName = "Linux"
	}
	if arch == "" {
		arch = "unknown"
	}

	return awakens.AwakeningReport{
		OS:           awakens.AwakeningOS{Name: osName, Version: osVersion, Kernel: kernelRel, Arch: arch},
		Shell:        awakens.AwakeningShell{Path: shell},
		Identity:     awakens.AwakeningIdentity{User: user, UID: uid, GID: gid},
		PresentTools: present,
		AbsentTools:  absent,
		ProbeTrace:   trace,
	}
}

// parseOSReleaseFile parses /etc/os-release KEY=VALUE lines, returning the
// pretty name, version id, and the full parsed map. Quotes are stripped.
func parseOSReleaseFile(raw string) (name, version string, kv map[string]string) {
	kv = map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		kv[k] = v
	}
	name = kv["NAME"]
	if name == "" {
		name = kv["PRETTY_NAME"]
	}
	version = kv["VERSION_ID"]
	return name, version, kv
}

// loginShellFromPasswd returns the shell field for user from /etc/passwd, or
// "" when the user is not found.
func loginShellFromPasswd(passwd, user string) string {
	prefix := user + ":"
	for _, line := range strings.Split(passwd, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 7 {
			return parts[6]
		}
	}
	return ""
}

// errAwakeningNotConfigured signals that the awakening path cannot run
// because no factory or repo is wired. CreateSession treats this as "skip"
// and preserves the legacy seed.
var errAwakeningNotConfigured = errors.New("awakening: not configured")

// extractEmittedMessage retrieves the A2UI envelope deposited by
// t-awaken-emit-message on the terminal "-emitted" place.
func extractEmittedMessage(c *cpn.CPN) (awakens.A2UIMessage, bool) {
	if c == nil {
		return awakens.A2UIMessage{}, false
	}
	place, ok := c.Places[awakens.PlaceAwakeningMessage+"-emitted"]
	if !ok {
		return awakens.A2UIMessage{}, false
	}
	tokens, ok := place.Peek()
	if !ok || len(tokens) == 0 {
		return awakens.A2UIMessage{}, false
	}
	env, ok := tokens[0].Payload.(awakens.A2UIMessage)
	return env, ok
}

// extractAwakeningSnapshot retrieves the HostCapabilitySnapshot deposited by
// t-awaken-persist on the well-known p-host-capabilities place.
func extractAwakeningSnapshot(c *cpn.CPN) (persist.HostCapabilitySnapshot, bool) {
	if c == nil {
		return persist.HostCapabilitySnapshot{}, false
	}
	place, ok := c.Places[cpn.WellKnownHostCapabilitiesPlace]
	if !ok {
		return persist.HostCapabilitySnapshot{}, false
	}
	tokens, ok := place.Peek()
	if !ok || len(tokens) == 0 {
		return persist.HostCapabilitySnapshot{}, false
	}
	snap, ok := tokens[0].Payload.(persist.HostCapabilitySnapshot)
	return snap, ok
}

// firstTurnMessageFromSnapshot derives a minimal A2UI envelope from an
// existing snapshot — used when the cache is fresh (AC-008) or the
// fallback path synthesised one. It projects the snapshot back into an
// AwakeningReport shape just for rendering; the underlying snapshot
// semantics are unchanged.
func firstTurnMessageFromSnapshot(snap persist.HostCapabilitySnapshot) awakens.A2UIMessage {
	report := awakens.AwakeningReport{
		OS: awakens.AwakeningOS{
			Name:    snap.Kernel.OS,
			Version: snap.Kernel.OSRelease["VERSION_ID"],
			Kernel:  snap.Kernel.Kernel,
			Arch:    snap.Kernel.Arch,
		},
		Shell: awakens.AwakeningShell{Path: snap.Identity.Shell},
	}
	for _, b := range snap.Binaries {
		if b.Present {
			report.PresentTools = append(report.PresentTools, awakens.AwakeningTool{
				Name: b.Name, Version: b.Version,
			})
		} else {
			report.AbsentTools = append(report.AbsentTools, b.Name)
		}
	}
	return awakens.BuildFirstTurnMessage(report)
}

// synthesizedFallbackEnvelope is the last-resort card shown when neither the
// awakening path nor the legacy host-discovery path is wired. BEH-003 still
// requires an invitation to the user.
func synthesizedFallbackEnvelope() awakens.A2UIMessage {
	return awakens.A2UIMessage{
		Components: []map[string]any{
			{
				"type":  "text",
				"props": map[string]any{"content": "¿En qué trabajamos hoy?"},
			},
		},
	}
}

// runAwakeningAsync runs the awakening flow on a fresh background context so
// session creation returns immediately. When the flow completes, it seeds
// p-host-capabilities on the session root and appends the first-turn A2UI
// card as an assistant message (delivered to the frontend via SSE).
func (s *SessionService) runAwakeningAsync(session *cpn.Session) {
	if session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), awakeningDeadline+15*time.Second)
	defer cancel()

	snap, envelope, source, err := s.runAwakening(ctx, session.ID)
	if err != nil {
		if !errors.Is(err, errAwakeningNotConfigured) {
			s.logger.Warn("awakening async flow errored",
				slog.String("session_id", session.ID), slog.Any("error", err))
		}
		return
	}
	s.seedAwakeningOnRoot(session.Root, snap)
	if len(envelope.Components) > 0 {
		s.appendAwakeningMessage(ctx, session, envelope)
	}
	_ = source
}

// seedHostCapabilitiesFromCache seeds root's p-host-capabilities from the
// latest persisted snapshot if one exists. Non-blocking: returns immediately
// when no snapshot is available. Fresh discovery runs asynchronously via
// runAwakeningAsync.
func (s *SessionService) seedHostCapabilitiesFromCache(ctx context.Context, root *cpn.CPN) {
	if root == nil || s.hostCapabilityRepo == nil {
		return
	}
	hostID := s.resolveHostID(ctx)
	snap, err := s.hostCapabilityRepo.LatestForHost(ctx, hostID)
	if err != nil || snap.CapturedAt.IsZero() {
		return
	}
	cpn.SeedHostSnapshot(root, snap)
}

// appendAwakeningMessage writes the first-turn A2UI envelope as the
// session's opening `assistant` message with cpn_role="awakening"
// (AC-001). The content is the JSON-encoded envelope — downstream
// frontends parse it as A2UI v0.8 per §4.4. Best-effort: persistence
// errors are logged but non-fatal.
func (s *SessionService) appendAwakeningMessage(ctx context.Context, session *cpn.Session, envelope awakens.A2UIMessage) {
	if session == nil {
		return
	}
	raw, err := envelope.Marshal()
	if err != nil {
		s.logger.Warn("awakening: marshal envelope", slog.Any("error", err))
		return
	}
	msg := &cpn.Message{
		ID:        session.ID + "-awaken-" + uuid.NewString(),
		Role:      cpn.RoleAssistant,
		Content:   cpn.A2UIMarker + string(raw),
		CPNID:     session.Root.ID,
		CPNRole:   awakens.A2UICPNRole,
		CPNDepth:  session.Root.Depth,
		Timestamp: time.Now().UTC(),
	}
	session.AppendMessage(msg)

	if s.persist == nil || s.persist.Sessions == nil {
		return
	}
	rec := persist.MessageToRecord(session.ID, msg)
	if err := s.persist.Sessions.AppendMessage(ctx, session.ID, rec); err != nil {
		s.logger.Warn("awakening: persist first-turn message",
			slog.String("session_id", session.ID), slog.Any("error", err))
	}
}

// seedAwakeningOnRoot seeds the well-known p-host-capabilities place on the
// session's root CPN so downstream flows (classify / plan / execute) see
// the awakening snapshot without re-reading the DB. Idempotent.
func (s *SessionService) seedAwakeningOnRoot(root *cpn.CPN, snap persist.HostCapabilitySnapshot) {
	if root == nil || snap.HostID == "" {
		return
	}
	cpn.SeedHostSnapshot(root, snap)
}

// envelopeToJSON is a tiny helper used by tests to assert structural
// equality of emitted A2UI payloads. It round-trips through encoding/json
// so key ordering is canonical.
func envelopeToJSON(e awakens.A2UIMessage) ([]byte, error) { return json.Marshal(e) }

// isInteractiveChannel reports whether the channel participates in
// conversational turn-taking (REQ-001). Today every defined ChannelType
// is interactive; the helper is a seam for future non-interactive
// channels (batch, webhook) that should skip awakening.
func isInteractiveChannel(ch cpn.ChannelType) bool {
	switch ch {
	case cpn.ChannelWeb, cpn.ChannelWhatsApp, cpn.ChannelTelegram:
		return true
	default:
		// Unknown channels default to interactive so new front-ends
		// inherit the behaviour; they can opt out explicitly later.
		return true
	}
}
