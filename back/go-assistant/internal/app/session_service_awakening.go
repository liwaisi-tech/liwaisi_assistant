package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// awakeningDeadline is the hard upper-bound for the `brae-awakens` topology.
// First-boot awakening MUST succeed within this window. When it does not,
// session creation fails — there is no fallback.
const awakeningDeadline = 30 * time.Second

// awakeningPinnedModel is the model every awakening LLM transition uses,
// regardless of the user's PreferredModel. Kept in sync with a GA Google
// slug that OpenRouter is known to accept with tools + tool-call re-calls.
// Do NOT route awakening through preview slugs — a broken awakening means
// the session never boots, and users cannot even log in to change the
// preference that broke it.
const awakeningPinnedModel = "google/gemini-2.5-flash"

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

// WithAwakeningComposer wires the probe-fanout composer invoked by the
// awakening topology's t-awaken-probe-compose transition. Production code
// passes a closure wrapping fanout.Compose; tests may pass any function
// conforming to awakens.ComposerFunc.
func WithAwakeningComposer(c awakens.ComposerFunc) SessionServiceOption {
	return func(s *SessionService) { s.awakeningComposer = c }
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
func (s *SessionService) runAwakening(ctx context.Context, sessionID, userID string) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	var zero persist.HostCapabilitySnapshot
	if s.awakensFactory == nil || s.hostCapabilityRepo == nil {
		return zero, awakens.A2UIMessage{}, "", errAwakeningNotConfigured
	}

	emitter := awakens.NewEmitter(s.logger)
	emitter.Started(ctx, sessionID, awakeningPinnedModel)
	startedAt := time.Now()
	defer func() {
		emitter.CheckSLO(ctx, "total", time.Since(startedAt))
	}()

	hostID := s.resolveHostID(ctx)

	// 24h cache hit skips shell probes but still emits the first-turn card.
	if snap, fresh, err := awakens.LookupCachedSnapshot(ctx, s.hostCapabilityRepo, hostID, awakens.DefaultCacheTTL, awakens.SystemClock); err == nil && fresh {
		envelope := firstTurnMessageFromSnapshot(snap)
		age := time.Since(snap.CapturedAt)
		if age < 0 {
			age = 0
		}
		emitter.CacheHit(ctx, age)
		emitter.Emitted(ctx, len(envelope.Components))
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

	// SC-16: BRAE_AWAKENING_MODE selects between the default fanout topology
	// and the legacy single-turn quarantine escape hatch. Emit the mode event
	// before any downstream wiring so operators see which path ran even when
	// subsequent stages fail.
	awakeningMode := awakens.ReadAwakeningMode(s.logger)
	emitter.Mode(ctx, string(awakeningMode))
	if awakeningMode == awakens.ModeLegacy {
		return s.runAwakeningLegacy(ctx, sessionID, hostID, emitter)
	}

	// SC-10 / SEC-004: every probe subprocess must run through bwrap or
	// firejail. Detect up front so a missing wrapper fails fast with a
	// stable error_class instead of 16 parallel probe-level failures.
	sandbox, sbErr := fanout.DetectSandboxFn()
	if sbErr != nil {
		emitter.Failed(ctx, "sandbox.detect", "sandbox_missing", sbErr.Error())
		return zero, awakens.A2UIMessage{}, "", fmt.Errorf("awakening: %w", sbErr)
	}

	// Build and run the awakens CPN.
	deps := awakens.Deps{
		Repository: s.hostCapabilityRepo,
		HostID:     hostID,
		Source:     awakens.SourceAwakening,
		Clock:      awakens.SystemClock,
		// Composer wires the probe-fanout composer (fanout.Compose). It is
		// set to nil here until cpn/awakens/fanout/composer.go lands; the
		// probe-compose transition surfaces a clear diagnostic when it is
		// invoked without a composer so the awakening path fails loudly
		// rather than stalling. The Composer Engineer will adapt
		// fanout.Compose into a ComposerFunc closure here.
		Composer: s.awakeningComposer,
		// Lexicon is wired via SessionService.lexicon (optional). When non-
		// nil, the awakening prompt builder embeds the 32-entry excerpt
		// + few-shot anchors per spec REQ-002 / AC-003.
		Lexicon: s.lexicon,
		Emitter: emitter,
	}
	if s.hostRuntime != nil {
		deps.HostAdapter = s.hostRuntime.Adapter
		deps.HostGate = s.hostRuntime.Gate
	}
	deps.Sandbox = sandbox
	_ = userID

	runCtx, cancel := context.WithTimeout(ctx, awakeningDeadline)
	defer cancel()
	runCtx = awakens.WithToolRegistry(runCtx, s.toolRegistry)

	// Flag the session as awakening-mode so the host-gate silently denies
	// any non-introspection command the LLM might emit.
	if s.awakeningMode != nil {
		s.awakeningMode.Begin(sessionID)
		defer s.awakeningMode.End(sessionID)
	}

	// SC-15: the primary awakening slug is awakeningPinnedModel; on 5xx /
	// 429 / timeout the chain advances through the configured fallbacks,
	// sharing the 30s awakeningDeadline across all attempts. Auth errors
	// fail fast.
	var root *cpn.CPN
	chain := awakens.DefaultFallbackChain()
	runErr := chain.Run(runCtx, emitter, func(attemptCtx context.Context, model string) error {
		root = s.awakensFactory(sessionID, deps)
		if root == nil {
			return errors.New("awakening: factory returned nil topology")
		}
		root.LLMClient = s.llm
		root.Cost = s.cost
		root.HostRuntime = s.hostRuntime
		if s.toolRegistry != nil {
			s.toolRegistry.InjectIntoCPN(root)
			root.ToolRegistry = s.toolRegistry
		}
		for _, tr := range root.Transitions {
			if tr == nil || tr.Kind != cpn.NodeKindLLM {
				continue
			}
			if tr.LLMConfig == nil {
				tr.LLMConfig = &cpn.LLMConfig{}
			}
			tr.LLMConfig.Model = model
		}
		return root.Run(attemptCtx)
	})
	if runErr != nil {
		emitter.Failed(ctx, "cpn.run", classifyAwakeningError(runErr), runErr.Error())
		return zero, awakens.A2UIMessage{}, "", fmt.Errorf("awakening: %w", runErr)
	}
	if root == nil {
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: factory returned nil topology")
	}

	// Extract the emitted A2UI envelope and snapshot from the terminal places.
	envelope, ok := extractEmittedMessage(root)
	if !ok {
		err := errors.New("awakening: CPN completed but emitted no A2UI envelope")
		emitter.Failed(ctx, "extract.envelope", "empty", err.Error())
		return zero, awakens.A2UIMessage{}, "", err
	}
	snap, ok := extractAwakeningSnapshot(root)
	if !ok {
		err := errors.New("awakening: CPN completed but deposited no snapshot")
		emitter.Failed(ctx, "extract.snapshot", "empty", err.Error())
		return zero, awakens.A2UIMessage{}, "", err
	}
	s.logger.Info("awakening: complete",
		slog.String("session_id", sessionID),
		slog.String("host_id", hostID),
		slog.String("source", awakens.SourceAwakening),
	)
	return snap, envelope, awakens.SourceAwakening, nil
}

// runAwakeningLegacy is the SC-16 quarantine escape hatch: a minimal single-
// turn LLM path that projects the same AwakeningReport shape without
// probe-fanout. Reuses the SC-15 FallbackChain and SC-08 emitter. No sandbox
// detection — legacy has no probes, so SC-10's bwrap/firejail requirement
// does not apply.
func (s *SessionService) runAwakeningLegacy(ctx context.Context, sessionID, hostID string, emitter *awakens.Emitter) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	var zero persist.HostCapabilitySnapshot

	deps := awakens.Deps{
		Repository: s.hostCapabilityRepo,
		HostID:     hostID,
		Source:     awakens.SourceAwakening,
		Clock:      awakens.SystemClock,
		Lexicon:    s.lexicon,
		Emitter:    emitter,
	}

	runCtx, cancel := context.WithTimeout(ctx, awakeningDeadline)
	defer cancel()
	runCtx = awakens.WithToolRegistry(runCtx, s.toolRegistry)

	if s.awakeningMode != nil {
		s.awakeningMode.Begin(sessionID)
		defer s.awakeningMode.End(sessionID)
	}

	var root *cpn.CPN
	chain := awakens.DefaultFallbackChain()
	runErr := chain.Run(runCtx, emitter, func(attemptCtx context.Context, model string) error {
		root = awakens.LegacyTopologyFactory(sessionID, deps)
		if root == nil {
			return errors.New("awakening: legacy factory returned nil topology")
		}
		root.LLMClient = s.llm
		root.Cost = s.cost
		root.HostRuntime = s.hostRuntime
		if s.toolRegistry != nil {
			s.toolRegistry.InjectIntoCPN(root)
			root.ToolRegistry = s.toolRegistry
		}
		for _, tr := range root.Transitions {
			if tr == nil || tr.Kind != cpn.NodeKindLLM {
				continue
			}
			if tr.LLMConfig == nil {
				tr.LLMConfig = &cpn.LLMConfig{}
			}
			tr.LLMConfig.Model = model
		}
		return root.Run(attemptCtx)
	})
	if runErr != nil {
		emitter.Failed(ctx, "cpn.run", classifyAwakeningError(runErr), runErr.Error())
		return zero, awakens.A2UIMessage{}, "", fmt.Errorf("awakening: %w", runErr)
	}
	if root == nil {
		return zero, awakens.A2UIMessage{}, "", errors.New("awakening: legacy factory returned nil topology")
	}

	envelope, ok := extractEmittedMessage(root)
	if !ok {
		err := errors.New("awakening: legacy CPN completed but emitted no A2UI envelope")
		emitter.Failed(ctx, "extract.envelope", "empty", err.Error())
		return zero, awakens.A2UIMessage{}, "", err
	}
	snap, ok := extractAwakeningSnapshot(root)
	if !ok {
		err := errors.New("awakening: legacy CPN completed but deposited no snapshot")
		emitter.Failed(ctx, "extract.snapshot", "empty", err.Error())
		return zero, awakens.A2UIMessage{}, "", err
	}
	s.logger.Info("awakening: complete (legacy mode)",
		slog.String("session_id", sessionID),
		slog.String("host_id", hostID),
		slog.String("source", awakens.SourceAwakening),
	)
	return snap, envelope, awakens.SourceAwakening, nil
}

// classifyAwakeningError coarse-buckets run-time failures into the
// error_class values the observability spine (REQ-801) reports. Kept
// deterministic by string-matching on sentinel text — the downstream
// consumer is alerting, not a parser.
func classifyAwakeningError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case strings.Contains(msg, "invalid awakening report"):
		return "invalid_report"
	case strings.Contains(msg, "composer"):
		return "composer"
	case strings.Contains(msg, "llm"):
		return "llm"
	default:
		return "unknown"
	}
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

	snap, envelope, source, err := s.runAwakening(ctx, session.ID, session.UserID)
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
