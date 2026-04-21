package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// awakeningDeadline is the hard upper-bound for the `brae-awakens` topology.
// First-boot awakening MUST succeed within this window. When it does not,
// session creation fails — there is no fallback.
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
// Nil disables the awakening path — CreateSession then treats the session
// as if no awakening is configured and does not run the flow.
func WithAwakensFactory(f AwakensFactory) SessionServiceOption {
	return func(s *SessionService) { s.awakensFactory = f }
}

// WithAwakeningMode wires the session-scoped awakening-mode registry used by
// the host-gate to silently deny non-introspection commands while
// `brae-awakens` is driving a session.
func WithAwakeningMode(m AwakeningModeMarker) SessionServiceOption {
	return func(s *SessionService) { s.awakeningMode = m }
}

// runAwakening performs the brae-awakens first-turn boot flow for a fresh
// interactive session. It returns the projected snapshot + the first-turn
// A2UI envelope + the chosen source tag.
//
// The method is synchronous by design: session readiness blocks on the
// awakening turn. It is bounded by awakeningDeadline so callers never
// hang past 30 seconds.
//
// When no awakensFactory / LLM / host-capability repo is wired, returns
// errAwakeningNotConfigured so CreateSession can skip the flow cleanly.
// Any other error — LLM unreachable, topology deadlock, missing terminal
// emission — is propagated to the caller; there is no fallback path.
func (s *SessionService) runAwakening(ctx context.Context, sessionID string) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	var zero persist.HostCapabilitySnapshot
	if s.awakensFactory == nil || s.hostCapabilityRepo == nil {
		return zero, awakens.A2UIMessage{}, "", errAwakeningNotConfigured
	}

	hostID := s.resolveHostID(ctx)

	// 24h cache hit skips shell probes but still emits the first-turn card.
	if snap, fresh, err := awakens.LookupCachedSnapshot(ctx, s.hostCapabilityRepo, hostID, awakens.DefaultCacheTTL, awakens.SystemClock); err == nil && fresh {
		envelope := firstTurnMessageFromSnapshot(snap)
		s.logger.Info("awakening: served from 24h cache",
			slog.String("session_id", sessionID),
			slog.String("host_id", hostID),
			slog.String("source", snap.Source),
		)
		return snap, envelope, snap.Source, nil
	}

	// First-boot awakening requires a live LLM. Missing provider is a
	// configuration failure — bubble it up rather than silently degrading.
	if s.llm == nil {
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: LLM client not configured")
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
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: factory returned nil topology")
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
		return zero, awakens.A2UIMessage{}, "", fmt.Errorf("awakening: %w", err)
	}

	// Extract the emitted A2UI envelope and snapshot from the terminal places.
	envelope, ok := extractEmittedMessage(root)
	if !ok {
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: CPN completed but emitted no A2UI envelope")
	}
	snap, ok := extractAwakeningSnapshot(root)
	if !ok {
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: CPN completed but deposited no snapshot")
	}
	s.logger.Info("awakening: complete",
		slog.String("session_id", sessionID),
		slog.String("host_id", hostID),
		slog.String("source", awakens.SourceAwakening),
	)
	return snap, envelope, awakens.SourceAwakening, nil
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
// existing snapshot — used when the cache is fresh. It projects the
// snapshot back into an AwakeningReport shape just for rendering; the
// underlying snapshot semantics are unchanged.
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
// session's opening `assistant` message with cpn_role="awakening". The
// content is the JSON-encoded envelope — downstream frontends parse it as
// A2UI v0.8. Best-effort: persistence errors are logged but non-fatal.
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
// conversational turn-taking. Today every defined ChannelType is
// interactive; the helper is a seam for future non-interactive channels
// (batch, webhook) that should skip awakening.
func isInteractiveChannel(ch cpn.ChannelType) bool {
	switch ch {
	case cpn.ChannelWeb, cpn.ChannelWhatsApp, cpn.ChannelTelegram:
		return true
	default:
		return true
	}
}
