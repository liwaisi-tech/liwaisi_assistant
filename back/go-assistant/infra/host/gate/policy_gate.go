package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// PolicyHostGate is the GAP-6 cpn.HostGate implementation. It evaluates
// a HostPolicy against each GateOp, consults the first-run ledger for
// unknown binaries, enforces session budgets, and writes an audit row
// for every decision.
//
// It is safe for unbounded concurrent use.
type PolicyHostGate struct {
	Policies   *Holder
	FirstRun   persist.FirstRunRepository
	Decisions  persist.GateDecisionRepository
	Budgets    *BudgetTracker
	SandboxCap SandboxCapability
	Logger     *slog.Logger

	// HostID is the stable identifier used to key first-run entries.
	// Defaults to os.Hostname() at construction; callers may override.
	HostID string

	// SessionIDResolver lets the gate pull the current session ID from
	// the context. When nil, the gate falls back to a constant bucket so
	// budgets still aggregate predictably in tests.
	SessionIDResolver func(context.Context) string

	// BudgetEstimator returns the predicted BudgetEstimate for an op.
	// When nil, the gate uses DefaultEstimate().
	BudgetEstimator func(op cpn.GateOp) BudgetEstimate

	// AwakeningMode tracks the set of sessions currently running the
	// `brae-awakens` topology. When the current session is registered, any
	// non-introspection shell command is SILENTLY denied (no HITL card) —
	// CON-003 / SEC-004 / AC-005. Introspection commands continue through
	// the normal safe-band path. Nil disables the feature (legacy).
	AwakeningMode *AwakeningModeRegistry

	// auditPool lazily holds the bounded worker pool that drains audit
	// writes off the hot path. Unbounded `go func()` spawning was removed
	// in REQ-FIX-008; the pool caps concurrent DB writes at
	// auditWorkerCount and drops (with a metric) on queue full / shutdown.
	auditPoolOnce sync.Once
	auditPool     *auditWorkerPool
}

// auditWorkerCount caps concurrent audit DB writers. 16 matches the
// BudgetTracker cadence and is small enough to avoid saturating the
// connection pool under burst.
const auditWorkerCount = 16

// auditQueueSize is how many decisions may be queued before Submit drops
// with a warn log. Sized to roughly one second of sustained gate throughput.
const auditQueueSize = 256

// auditJob bundles a decision record with the GateOp kind so the worker
// can emit a meaningful log on failure.
type auditJob struct {
	rec    *persist.GateDecisionRecord
	opKind string
}

// auditWorkerPool drains audit writes asynchronously. Cf. REQ-FIX-008 and
// spec §4.5.
type auditWorkerPool struct {
	queue    chan auditJob
	wg       sync.WaitGroup
	store    persist.GateDecisionRepository
	logger   *slog.Logger
	shutdown chan struct{}
	// closed guards against double-Close on the queue channel.
	closeOnce sync.Once
}

func newAuditWorkerPool(store persist.GateDecisionRepository, logger *slog.Logger) *auditWorkerPool {
	if logger == nil {
		logger = slog.Default()
	}
	p := &auditWorkerPool{
		queue:    make(chan auditJob, auditQueueSize),
		store:    store,
		logger:   logger,
		shutdown: make(chan struct{}),
	}
	for range auditWorkerCount {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

func (p *auditWorkerPool) worker() {
	defer p.wg.Done()
	for job := range p.queue {
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := p.store.Insert(writeCtx, job.rec); err != nil {
			p.logger.Warn("gate: audit insert failed", "error", err, "op", job.opKind)
		}
		cancel()
	}
}

// Submit enqueues a job non-blocking. On full queue the record is dropped
// and logged — audit continuity is best-effort, never a hot-path block.
func (p *auditWorkerPool) Submit(job auditJob) {
	// Fast-path: if shutdown has been signalled, record the drop and bail
	// BEFORE touching p.queue (which is closed by Shutdown; a send would
	// panic).
	select {
	case <-p.shutdown:
		p.logger.Warn("gate: audit dropped on shutdown", "op", job.opKind)
		return
	default:
	}
	select {
	case p.queue <- job:
	default:
		p.logger.Warn("gate: audit queue full; dropping record", "op", job.opKind)
	}
}

// Shutdown signals workers to finish the queue and waits for them or for
// ctx to expire — whichever comes first.
func (p *auditWorkerPool) Shutdown(ctx context.Context) error {
	p.closeOnce.Do(func() {
		close(p.shutdown)
		close(p.queue)
	})
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Compile-time interface check.
var _ cpn.HostGate = (*PolicyHostGate)(nil)

// SandboxCapability reports whether a sandbox runtime is available. The
// policy gate consults it once per decision so it can deny ops that
// demand a profile the host cannot enforce (REQ-021).
type SandboxCapability interface {
	Available(runtime string) bool
}

// staticSandboxCapability is a no-dependency implementation that probes
// $PATH lazily on first query and caches the answer.
//
// Safe for concurrent use: PolicyHostGate.Check is documented as unbounded
// concurrent (see struct doc); the cache MUST NOT race. Reads dominate once
// the set of runtimes is warm, so an RWMutex is the right trade-off.
type staticSandboxCapability struct {
	mu    sync.RWMutex
	cache map[string]bool
}

// NewStaticSandboxCapability returns a capability object that defers to
// exec.LookPath the first time a runtime is queried.
func NewStaticSandboxCapability() SandboxCapability {
	return &staticSandboxCapability{cache: make(map[string]bool)}
}

func (s *staticSandboxCapability) Available(runtime string) bool {
	if runtime == "" {
		return false
	}
	s.mu.RLock()
	v, ok := s.cache[runtime]
	s.mu.RUnlock()
	if ok {
		return v
	}
	// Probe outside the lock — exec.LookPath hits the filesystem and is
	// safe to repeat if two callers race on the same unseen runtime.
	_, err := exec.LookPath(runtime)
	avail := err == nil
	s.mu.Lock()
	s.cache[runtime] = avail
	s.mu.Unlock()
	return avail
}

// NewPolicyHostGate constructs a gate with sensible defaults. Any
// dependency left nil is replaced with a safe no-op so the gate can be
// exercised in isolation from its collaborators.
func NewPolicyHostGate(policies *Holder, firstRun persist.FirstRunRepository, decisions persist.GateDecisionRepository, budgets *BudgetTracker, sandbox SandboxCapability, logger *slog.Logger) *PolicyHostGate {
	if logger == nil {
		logger = slog.Default()
	}
	if budgets == nil {
		budgets = NewBudgetTracker()
	}
	if sandbox == nil {
		sandbox = NewStaticSandboxCapability()
	}
	hostID, _ := os.Hostname()
	if hostID == "" {
		hostID = "localhost"
	}
	return &PolicyHostGate{
		Policies:   policies,
		FirstRun:   firstRun,
		Decisions:  decisions,
		Budgets:    budgets,
		SandboxCap: sandbox,
		Logger:     logger,
		HostID:     hostID,
	}
}

// Check implements cpn.HostGate.Check.
func (g *PolicyHostGate) Check(ctx context.Context, op cpn.GateOp) error {
	dec := g.Evaluate(ctx, op)
	g.auditAsync(ctx, dec, op, "")

	switch dec.Verdict {
	case VerdictAllow:
		return nil
	case VerdictDeny:
		return cpn.NewHostError(cpn.HostErrCodeGateDenied,
			fmt.Sprintf("gate denied %s: %s (band=%s)", op.Kind, dec.Reason, dec.RiskBand), nil)
	case VerdictRequireHITL:
		return &ErrRequiresHITL{Decision: dec}
	}
	return cpn.ErrGateDenied
}

// Evaluate runs the full policy pipeline and returns a Decision
// without auditing. Exposed for tests and for the HITL flow that
// audits the eventual approve/deny once the user has responded.
func (g *PolicyHostGate) Evaluate(ctx context.Context, op cpn.GateOp) Decision {
	policy := g.policy()
	sessionID := g.sessionID(ctx)

	dec := Decision{
		Verdict:   VerdictDeny,
		RiskBand:  RiskUnknown,
		Sandbox:   g.resolveSandbox(op, policy),
		SessionID: sessionID,
	}

	// Awakening-mode fast-path (CON-003 / SEC-001..SEC-004 / AC-005). When
	// the current session is running `brae-awakens`:
	//   - Non-introspection commands are silently denied (no HITL prompt).
	//   - Introspection commands are AUTO-APPROVED immediately, bypassing
	//     first-run ledger / caution / HITL. Awakening must complete
	//     unattended; asking the user to approve every `whoami`/`uname`
	//     the planner emits defeats the point of silent probing and was
	//     the cause of all `info-*` probes returning GateDenyExitCode=-2
	//     on fresh hosts whose ledger had no entry for the busybox applets.
	// This short-circuit runs BEFORE kill ops and budget checks so an
	// LLM-issued destructive command cannot leak into the user's HITL
	// surface even if the classifier were to somehow grant it a budget.
	if g.AwakeningMode != nil && g.AwakeningMode.Active(sessionID) &&
		(op.Kind == "exec" || op.Kind == "spawn_pty") {
		if isIntrospectionCommand(op.Command) {
			dec.Verdict = VerdictAllow
			dec.RiskBand = RiskSafe
			dec.Reason = "awakening mode: introspection auto-approved"
			return dec
		}
		dec.Verdict = VerdictDeny
		dec.RiskBand = RiskForbidden
		dec.Reason = "awakening mode: command outside introspection allow-list"
		return dec
	}

	// Kill ops bypass policy classification but are still budget-checked.
	// They allow the killer path to reclaim stuck processes without
	// bouncing through HITL.
	if op.Kind == "kill" {
		dec.Verdict = VerdictAllow
		dec.RiskBand = RiskSafe
		dec.Reason = "kill allowed"
		return dec
	}

	// Build the matcher target. For write_file we synthesise a pseudo
	// command "write_file: <path>" so path-based patterns (dangerous)
	// can match cleanly per spec §4.
	target := op.Command
	if op.Kind == "write_file" || op.Kind == "read_file" {
		target = fmt.Sprintf("%s: %s", op.Kind, op.Path)
	}

	// ── 1. Forbidden short-circuit ───────────────────────────────────
	band, _ := policy.Classify(target)
	dec.RiskBand = band
	if band == RiskForbidden {
		dec.Verdict = VerdictDeny
		dec.Reason = "command matches forbidden_patterns"
		return dec
	}

	// ── 2. Budget gate ───────────────────────────────────────────────
	estimate := DefaultEstimate()
	if g.BudgetEstimator != nil {
		estimate = g.BudgetEstimator(op)
	}
	dec.Budget = estimate
	if reason, ok := g.Budgets.CheckEstimate(sessionID, policy.Defaults, estimate); !ok {
		dec.Verdict = VerdictRequireHITL
		dec.Reason = reason
		dec.Prompt = &HostApprovalPrompt{
			Schema:    HostApprovalSchema,
			Operation: op.Kind,
			Command:   displayCommand(op),
			RiskBand:  dec.RiskBand,
			Rationale: "Session budget exhausted: " + reason,
			Alternatives: []string{
				"raise-budget",
				"terminate-task",
			},
		}
		return dec
	}

	// ── 3. Sandbox-availability gate (REQ-021) ──────────────────────
	if dec.Sandbox != cpn.SandboxNone && dec.Sandbox != "" {
		if err := g.checkSandboxAvailable(dec.Sandbox, policy); err != nil {
			dec.Verdict = VerdictDeny
			dec.Reason = "sandbox_unavailable"
			return dec
		}
	}

	// ── 4. Dangerous band: always HITL, even when remembered (REQ-042
	//      / AC-007). Evaluate BEFORE first-run so the ledger does not
	//      silently approve a future dangerous invocation. ────────────
	if band == RiskDangerous {
		dec.Verdict = VerdictRequireHITL
		dec.Reason = "command matches dangerous_patterns"
		dec.Prompt = &HostApprovalPrompt{
			Schema:    HostApprovalSchema,
			Operation: op.Kind,
			Command:   displayCommand(op),
			RiskBand:  RiskDangerous,
			Rationale: "Dangerous pattern — approval does not stick.",
		}
		return dec
	}

	// ── 5. First-run ledger (REQ-011 + AC-008) ──────────────────────
	// Only applies to exec/spawn_pty (binaries); write_file has no SHA.
	if g.FirstRun != nil && (op.Kind == "exec" || op.Kind == "spawn_pty") && op.Command != "" {
		sum, entry, err := g.firstRunLookup(ctx, op.Command)
		if err == nil && (entry == nil || entry.Revoked || entry.FirstApprovedAt == nil) {
			dec.Verdict = VerdictRequireHITL
			dec.Reason = "first_run_unapproved"
			dec.FirstRun = true
			dec.Prompt = &HostApprovalPrompt{
				Schema:    HostApprovalSchema,
				Operation: op.Kind,
				Command:   displayCommand(op),
				RiskBand:  dec.RiskBand,
				Rationale: fmt.Sprintf("Binary first-seen on this host (sha256=%s...).", truncSHA(sum)),
				Alternatives: []string{
					"approve-once",
					"approve-and-remember",
					"deny",
				},
			}
			return dec
		}
	}

	// ── 6. Caution band: HITL unless a learned safe pattern overrules. ─
	if band == RiskCaution {
		// If safe_patterns also match (approve-and-remember), let it
		// through. Classify runs safe last so caution hits shadow
		// earlier; we re-check explicitly.
		if ok, _ := policy.safe.matches(normaliseCommand(target)); ok {
			dec.Verdict = VerdictAllow
			dec.Reason = "caution overridden by learned safe pattern"
			return dec
		}
		dec.Verdict = VerdictRequireHITL
		dec.Reason = "command matches caution_patterns"
		dec.Prompt = &HostApprovalPrompt{
			Schema:    HostApprovalSchema,
			Operation: op.Kind,
			Command:   displayCommand(op),
			RiskBand:  RiskCaution,
			Rationale: "Caution-band command requires explicit approval.",
			Alternatives: []string{
				"approve-once",
				"approve-and-remember",
				"deny",
			},
		}
		return dec
	}

	// ── 7. Safe band: explicit allow. ────────────────────────────────
	if band == RiskSafe {
		dec.Verdict = VerdictAllow
		dec.Reason = "command matches safe_patterns"
		return dec
	}

	// ── 8. Default: deny-by-default for unknown commands (GUD-001). ──
	dec.Verdict = VerdictRequireHITL
	dec.Reason = "command not in any pattern (default deny)"
	dec.RiskBand = RiskUnknown
	dec.Prompt = &HostApprovalPrompt{
		Schema:    HostApprovalSchema,
		Operation: op.Kind,
		Command:   displayCommand(op),
		RiskBand:  RiskUnknown,
		Rationale: "Command not in any policy bucket.",
		Alternatives: []string{
			"approve-once",
			"approve-and-remember",
			"deny",
		},
	}
	return dec
}

// RecordActual records actual resource usage against the session budget.
// Exposed via PolicyHostGate so callers can wire it into the executor
// post-flight reconcile (REQ-031).
func (g *PolicyHostGate) RecordActual(_ context.Context, op cpn.GateOp, actual BudgetEstimate) error {
	if g.Budgets == nil {
		return nil
	}
	sid := op.SessionID
	if sid == "" {
		// Caller failed to plumb a session id through GateOp. Bucket into a
		// named-"unknown" so one mis-wired caller can't siphon budget from
		// real sessions (REQ-FIX-009).
		sid = "unknown"
	}
	g.Budgets.RecordActual(sid, actual)
	return nil
}

// ApproveFirstRun stamps the ledger row for (hostID, sha) as approved by
// userID. No-op when FirstRun is nil.
func (g *PolicyHostGate) ApproveFirstRun(ctx context.Context, hostID, sha, userID string) error {
	if g.FirstRun == nil {
		return nil
	}
	if hostID == "" {
		hostID = g.HostID
	}
	return g.FirstRun.Approve(ctx, hostID, sha, userID)
}

// RevokeFirstRun flips a ledger row back to unapproved.
func (g *PolicyHostGate) RevokeFirstRun(ctx context.Context, hostID, sha string) error {
	if g.FirstRun == nil {
		return nil
	}
	if hostID == "" {
		hostID = g.HostID
	}
	return g.FirstRun.Revoke(ctx, hostID, sha)
}

// ── Internal helpers ────────────────────────────────────────────────────────

func (g *PolicyHostGate) policy() *HostPolicy {
	if g.Policies == nil {
		// Fallback empty policy: deny-by-default, no patterns.
		empty := &HostPolicy{}
		_ = empty.finalize()
		return empty
	}
	return g.Policies.Get()
}

func (g *PolicyHostGate) sessionID(ctx context.Context) string {
	if g.SessionIDResolver != nil {
		if s := g.SessionIDResolver(ctx); s != "" {
			return s
		}
	}
	return "default"
}

// resolveSandbox returns the profile the op will run under. When the op
// supplies one explicitly, that wins; otherwise the policy default is
// used.
func (g *PolicyHostGate) resolveSandbox(op cpn.GateOp, policy *HostPolicy) cpn.SandboxProfile {
	if op.Sandbox != "" {
		return op.Sandbox
	}
	return cpn.SandboxProfile(policy.Defaults.Sandbox)
}

// checkSandboxAvailable verifies that at least one runtime (preferred or
// fallback) is installed for the requested profile.
func (g *PolicyHostGate) checkSandboxAvailable(profile cpn.SandboxProfile, policy *HostPolicy) error {
	mapping, ok := policy.SandboxMappings[string(profile)]
	if !ok {
		// Default to bwrap/firejail when no explicit mapping.
		mapping = SandboxMapping{Preferred: "bwrap", Fallback: "firejail"}
	}
	if g.SandboxCap.Available(mapping.Preferred) {
		return nil
	}
	if mapping.Fallback != "" && g.SandboxCap.Available(mapping.Fallback) {
		return nil
	}
	return ErrSandboxUnavailable
}

// firstRunLookup computes the SHA-256 of op.Command (resolved to its
// absolute path via exec.LookPath) and consults the ledger. A missing
// binary returns ("", nil, nil) so the gate falls through to default
// deny instead of wedging.
func (g *PolicyHostGate) firstRunLookup(ctx context.Context, command string) (string, *persist.FirstRunLedgerEntry, error) {
	path, err := resolveBinaryPath(command)
	if err != nil {
		return "", nil, err
	}
	sum, err := sha256File(path)
	if err != nil {
		return "", nil, err
	}
	// Record idempotently and then consult.
	if _, err := g.FirstRun.Record(ctx, g.HostID, path, sum); err != nil {
		return sum, nil, err
	}
	entry, err := g.FirstRun.GetBySHA(ctx, g.HostID, sum)
	return sum, entry, err
}

// resolveBinaryPath resolves a command string to an absolute path.
func resolveBinaryPath(command string) (string, error) {
	head, _, _ := splitFirstToken(strings.TrimSpace(command))
	head = unquote(head)
	if head == "" {
		return "", fmt.Errorf("empty command")
	}
	if filepath.IsAbs(head) {
		return head, nil
	}
	return exec.LookPath(head)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// auditAsync writes the decision to the audit log in a goroutine so the
// gate hot path never blocks on the database. hitlResponseID is empty
// during the pre-decision audit; callers that resolve a HITL approval
// should use AuditDecision directly with the response ID.
func (g *PolicyHostGate) auditAsync(ctx context.Context, dec Decision, op cpn.GateOp, hitlResponseID string) {
	if g.Decisions == nil {
		return
	}
	rec := newAuditRecord(dec, op, hitlResponseID)
	g.ensureAuditPool().Submit(auditJob{rec: rec, opKind: op.Kind})
	_ = ctx
}

// ensureAuditPool lazily starts the bounded worker pool on first use. The
// pool survives for the lifetime of the gate; callers MAY call
// ShutdownAuditPool at process exit to drain pending writes.
func (g *PolicyHostGate) ensureAuditPool() *auditWorkerPool {
	g.auditPoolOnce.Do(func() {
		logger := g.Logger
		if logger == nil {
			logger = slog.Default()
		}
		g.auditPool = newAuditWorkerPool(g.Decisions, logger)
	})
	return g.auditPool
}

// ShutdownAuditPool signals the audit worker pool to drain and waits up to
// ctx's deadline. Safe to call multiple times; also safe when the pool was
// never started (no-op).
func (g *PolicyHostGate) ShutdownAuditPool(ctx context.Context) error {
	if g.auditPool == nil {
		return nil
	}
	return g.auditPool.Shutdown(ctx)
}

// AuditDecision writes a decision synchronously. Used by the HITL post-
// resolution path where ordering with the response row matters.
func (g *PolicyHostGate) AuditDecision(ctx context.Context, dec Decision, op cpn.GateOp, hitlResponseID string) error {
	if g.Decisions == nil {
		return nil
	}
	return g.Decisions.Insert(ctx, newAuditRecord(dec, op, hitlResponseID))
}

func newAuditRecord(dec Decision, op cpn.GateOp, hitlResponseID string) *persist.GateDecisionRecord {
	hash := sha256.Sum256([]byte(CommandKey(op.Command)))
	var hitlPtr *string
	if hitlResponseID != "" {
		h := hitlResponseID
		hitlPtr = &h
	}
	return &persist.GateDecisionRecord{
		ID:             uuid.NewString(),
		SessionID:      dec.SessionID,
		OpKind:         op.Kind,
		CommandHash:    hex.EncodeToString(hash[:]),
		Path:           op.Path,
		Sandbox:        string(dec.Sandbox),
		Decision:       string(dec.Verdict),
		Reason:         dec.Reason,
		RiskBand:       string(dec.RiskBand),
		DecidedAt:      time.Now().UTC(),
		HITLResponseID: hitlPtr,
	}
}

func displayCommand(op cpn.GateOp) string {
	if op.Command != "" {
		return op.Command
	}
	if op.Path != "" {
		return op.Kind + " " + op.Path
	}
	return op.Kind
}

func truncSHA(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}
