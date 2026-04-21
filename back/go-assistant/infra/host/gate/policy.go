package gate

import (
	"fmt"
	"os"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"
)

// Sentinels controlling YAML size caps (guards against a maliciously large
// user-supplied policy wedging the boot path). Pattern counts are generous
// because the default policy alone adds ~10 entries per bucket; the caps
// here are one order of magnitude above real-world use.
const (
	maxPatternsPerBucket = 256
	maxYAMLBytes         = 1 << 20 // 1 MiB
)

// HostPolicy is the YAML-loadable policy document per spec §4. Every
// pattern in *_patterns is compiled on load.
type HostPolicy struct {
	Version         int                       `yaml:"version"`
	Defaults        PolicyDefaults            `yaml:"defaults"`
	ForbiddenPat    []string                  `yaml:"forbidden_patterns"`
	SafePat         []string                  `yaml:"safe_patterns"`
	CautionPat      []string                  `yaml:"caution_patterns"`
	DangerousPat    []string                  `yaml:"dangerous_patterns"`
	HITLPat         []string                  `yaml:"hitl_patterns"`
	PathJail        PolicyPathJail            `yaml:"path_jail"`
	SandboxMappings map[string]SandboxMapping `yaml:"sandbox_mappings"`

	// ── Compiled state (populated by finalize). Not YAML-serialised. ──

	forbidden *compiledBand
	safe      *compiledBand
	caution   *compiledBand
	dangerous *compiledBand
	hitl      *compiledBand
}

// PolicyDefaults groups the server-wide defaults.
type PolicyDefaults struct {
	Sandbox                string `yaml:"sandbox"`
	CPUSecondsBudget       int    `yaml:"cpu_seconds_budget"`
	BytesWrittenBudget     int64  `yaml:"bytes_written_budget"`
	OutboundRequestsBudget int    `yaml:"outbound_requests_budget"`
}

// PolicyPathJail defines the read/write path roots.
type PolicyPathJail struct {
	Write string `yaml:"write"`
	Read  string `yaml:"read"`
}

// SandboxMapping is the preferred/fallback runtime for one profile.
type SandboxMapping struct {
	Preferred string `yaml:"preferred"`
	Fallback  string `yaml:"fallback"`
}

// compiledBand is a compiled pattern set paired with its source strings.
type compiledBand struct {
	raw      []string
	compiled []*regexp.Regexp
}

// matches returns (true, matchedIdx) when s matches any compiled pattern.
func (b *compiledBand) matches(s string) (bool, int) {
	if b == nil {
		return false, -1
	}
	for i, re := range b.compiled {
		if re.MatchString(s) {
			return true, i
		}
	}
	return false, -1
}

// ── Loader ──────────────────────────────────────────────────────────────────

// LoadFromFile reads the YAML document at path, compiles every pattern
// and returns the finalised HostPolicy. Returns a descriptive error on
// YAML parse failure or pattern compile failure; the previous policy
// (if any) is unaffected.
func LoadFromFile(path string) (*HostPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gate: read policy %s: %w", path, err)
	}
	if len(raw) > maxYAMLBytes {
		return nil, fmt.Errorf("gate: policy %s exceeds %d bytes", path, maxYAMLBytes)
	}
	return LoadFromBytes(raw)
}

// LoadFromBytes parses a YAML policy document from an in-memory buffer.
func LoadFromBytes(raw []byte) (*HostPolicy, error) {
	var p HostPolicy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("gate: parse policy: %w", err)
	}
	if err := p.finalize(); err != nil {
		return nil, err
	}
	return &p, nil
}

// finalize compiles every pattern and applies the size caps. Safe to call
// multiple times; it rebuilds the compiled state each call.
func (p *HostPolicy) finalize() error {
	if p.Version == 0 {
		p.Version = 1
	}
	// Apply sensible defaults that let callers short-cut budget fields.
	if p.Defaults.CPUSecondsBudget == 0 {
		p.Defaults.CPUSecondsBudget = 120
	}
	if p.Defaults.BytesWrittenBudget == 0 {
		p.Defaults.BytesWrittenBudget = 256 << 20 // 256 MiB
	}
	if p.Defaults.OutboundRequestsBudget == 0 {
		p.Defaults.OutboundRequestsBudget = 50
	}
	if p.Defaults.Sandbox == "" {
		p.Defaults.Sandbox = "readonly"
	}

	buckets := map[string][]string{
		"forbidden_patterns": p.ForbiddenPat,
		"safe_patterns":      p.SafePat,
		"caution_patterns":   p.CautionPat,
		"dangerous_patterns": p.DangerousPat,
		"hitl_patterns":      p.HITLPat,
	}
	for name, patterns := range buckets {
		if len(patterns) > maxPatternsPerBucket {
			return fmt.Errorf("gate: %s exceeds cap of %d", name, maxPatternsPerBucket)
		}
	}

	var err error
	p.forbidden, err = compileBand(p.ForbiddenPat, "forbidden_patterns")
	if err != nil {
		return err
	}
	p.safe, err = compileBand(p.SafePat, "safe_patterns")
	if err != nil {
		return err
	}
	p.caution, err = compileBand(p.CautionPat, "caution_patterns")
	if err != nil {
		return err
	}
	p.dangerous, err = compileBand(p.DangerousPat, "dangerous_patterns")
	if err != nil {
		return err
	}
	p.hitl, err = compileBand(p.HITLPat, "hitl_patterns")
	if err != nil {
		return err
	}
	return nil
}

// compileBand pre-compiles every regex. We reject partial failures so a
// typo never silently downgrades policy coverage.
func compileBand(src []string, name string) (*compiledBand, error) {
	if len(src) == 0 {
		return &compiledBand{}, nil
	}
	b := &compiledBand{
		raw:      append([]string(nil), src...),
		compiled: make([]*regexp.Regexp, 0, len(src)),
	}
	for i, s := range src {
		re, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("gate: compile %s[%d] %q: %w", name, i, s, err)
		}
		b.compiled = append(b.compiled, re)
	}
	return b, nil
}

// ── Marshal ─────────────────────────────────────────────────────────────────

// ToYAML encodes the policy back to YAML for GET /api/host/policy.
func (p *HostPolicy) ToYAML() ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("gate: nil policy")
	}
	return yaml.Marshal(p)
}

// ── Policy holder (thread-safe hot swap, REQ-EDGE "policy hot-reload") ────

// Holder wraps a *HostPolicy behind a sync.RWMutex so Check() readers can
// proceed in parallel while Put() serialises writes. Get() returns the
// current pointer; Check() callers must NOT cache it across calls.
type Holder struct {
	mu     sync.RWMutex
	policy *HostPolicy

	// learnedPath is the on-disk location of the "approve-and-remember"
	// overlay. When non-empty, AppendLearnedSafePattern writes to it.
	learnedPath string
}

// NewHolder constructs a holder pre-loaded with policy.
func NewHolder(policy *HostPolicy) *Holder {
	return &Holder{policy: policy}
}

// WithLearnedPath sets the on-disk location of the "learned" overlay.
func (h *Holder) WithLearnedPath(path string) *Holder {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.learnedPath = path
	return h
}

// Get returns the current policy (safe for concurrent read).
func (h *Holder) Get() *HostPolicy {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.policy
}

// Put atomically swaps the active policy. Intended for PUT /api/host/policy.
func (h *Holder) Put(p *HostPolicy) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy = p
}

// LearnedPath returns the configured learned-overlay path ("" when unset).
func (h *Holder) LearnedPath() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.learnedPath
}

// AppendLearnedSafePattern appends pattern to the in-memory safe-pattern
// list AND, when a learned-overlay file is configured, persists it so
// restarts retain the allow-list. REQ-042 "approve-and-remember".
//
// The pattern is expected to already be a valid Go regex — callers that
// pull it from untrusted input MUST validate it first (see ValidatePattern).
func (h *Holder) AppendLearnedSafePattern(pattern string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.policy == nil {
		return fmt.Errorf("gate: no policy loaded")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("gate: invalid learned pattern %q: %w", pattern, err)
	}
	h.policy.SafePat = append(h.policy.SafePat, pattern)
	if h.policy.safe == nil {
		h.policy.safe = &compiledBand{}
	}
	h.policy.safe.raw = append(h.policy.safe.raw, pattern)
	h.policy.safe.compiled = append(h.policy.safe.compiled, re)

	if h.learnedPath == "" {
		return nil
	}
	return appendToLearnedFile(h.learnedPath, pattern)
}

// ValidatePattern compiles pattern and returns nil when it is a valid Go
// regex. Helper for the HTTP handlers.
func ValidatePattern(pattern string) error {
	_, err := regexp.Compile(pattern)
	return err
}

// appendToLearnedFile persists a single safe_patterns entry to the learned
// overlay YAML. The file format is identical to default.yaml but only the
// safe_patterns list is read back on startup.
func appendToLearnedFile(path, pattern string) error {
	// Load-or-init the overlay.
	var learned struct {
		SafePat []string `yaml:"safe_patterns"`
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(raw, &learned)
	}
	for _, p := range learned.SafePat {
		if p == pattern {
			return nil // dedup
		}
	}
	learned.SafePat = append(learned.SafePat, pattern)
	out, err := yaml.Marshal(&learned)
	if err != nil {
		return fmt.Errorf("gate: marshal learned overlay: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("gate: write learned overlay %s: %w", path, err)
	}
	return nil
}

// MergeLearnedFromFile loads the overlay at path (if it exists) and
// appends its safe_patterns to the given policy. Missing file is not an
// error — it just means no prior "approve-and-remember" has fired.
func MergeLearnedFromFile(p *HostPolicy, path string) error {
	if p == nil {
		return fmt.Errorf("gate: nil policy")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("gate: read learned overlay %s: %w", path, err)
	}
	var learned struct {
		SafePat []string `yaml:"safe_patterns"`
	}
	if err := yaml.Unmarshal(raw, &learned); err != nil {
		return fmt.Errorf("gate: parse learned overlay: %w", err)
	}
	if len(learned.SafePat) == 0 {
		return nil
	}
	p.SafePat = append(p.SafePat, learned.SafePat...)
	return p.finalize()
}
