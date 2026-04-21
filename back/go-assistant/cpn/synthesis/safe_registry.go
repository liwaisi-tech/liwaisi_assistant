package synthesis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Primitive kinds ───────────────────────────────────────────────────────

// Primitive kinds — agent-authored topologies MAY reference any name whose
// SafePrimitive.Kind equals one of these.
const (
	PrimitiveKindGuard    = "guard"
	PrimitiveKindExecutor = "executor"
	PrimitiveKindFactory  = "factory"
)

// SafePrimitive is the compact catalogue entry embedded into the LLM prompt
// (REQ-020). Params/Output are JSON-schema-ish hints, not strict specs — the
// LLM uses them to pick well-typed combinations; the linter rejects anything
// that escapes the kind whitelist.
type SafePrimitive struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Params   json.RawMessage `json:"params,omitempty"`
	Output   string          `json:"output,omitempty"`
	CostHint string          `json:"cost_hint,omitempty"`
}

// ── SafeRegistry ──────────────────────────────────────────────────────────

// SafeRegistry is the sealed catalogue of primitives an agent-authored
// topology may reference. Register at server boot via RegisterSafePrimitive;
// BuildSafeFuncRegistry returns a persist.FuncRegistry preloaded with every
// safe primitive's Go implementation. Once Seal has been called, further
// Register calls panic.
//
// The registry is a twin of the persist.FuncRegistry: agent-authored
// topologies resolve their guard/executor/factory names through this
// sealed surface, while the surrounding platform uses its own FuncRegistry
// for built-in topologies. A single primitive may (and does) appear in
// both — we register into the safe registry first, then the boot wiring
// replays the same names into the platform FuncRegistry so unmarshalling
// a saved topology back into a *cpn.CPN works from either path.
type SafeRegistry struct {
	mu         sync.RWMutex
	sealed     bool
	primitives map[string]SafePrimitive

	guards    map[string]func([]*cpn.Token) bool
	executors map[string]func(context.Context, cpn.Token) (cpn.Token, error)
	factories map[string]func() *cpn.CPN

	bashSnippets map[string]string
}

// NewSafeRegistry returns an empty SafeRegistry. Typical callers invoke
// RegisterDefaults(sr) right after construction.
func NewSafeRegistry() *SafeRegistry {
	return &SafeRegistry{
		primitives:   make(map[string]SafePrimitive),
		guards:       make(map[string]func([]*cpn.Token) bool),
		executors:    make(map[string]func(context.Context, cpn.Token) (cpn.Token, error)),
		factories:    make(map[string]func() *cpn.CPN),
		bashSnippets: make(map[string]string),
	}
}

// RegisterSafePrimitive enrols a new primitive. Panics if the registry has
// been sealed or if the same name is registered twice.
func (r *SafeRegistry) RegisterSafePrimitive(p SafePrimitive, impl any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		panic(fmt.Sprintf("synthesis: SafeRegistry is sealed, cannot add %q", p.Name))
	}
	if _, ok := r.primitives[p.Name]; ok {
		panic(fmt.Sprintf("synthesis: duplicate SafePrimitive %q", p.Name))
	}
	if p.Name == "" {
		panic("synthesis: SafePrimitive.Name is required")
	}
	switch p.Kind {
	case PrimitiveKindGuard:
		fn, ok := impl.(func([]*cpn.Token) bool)
		if !ok {
			panic(fmt.Sprintf("synthesis: primitive %q kind=guard needs func([]*cpn.Token) bool, got %T", p.Name, impl))
		}
		r.guards[p.Name] = fn
	case PrimitiveKindExecutor:
		fn, ok := impl.(func(context.Context, cpn.Token) (cpn.Token, error))
		if !ok {
			panic(fmt.Sprintf("synthesis: primitive %q kind=executor needs func(ctx, Token) (Token, error), got %T", p.Name, impl))
		}
		r.executors[p.Name] = fn
	case PrimitiveKindFactory:
		fn, ok := impl.(func() *cpn.CPN)
		if !ok {
			panic(fmt.Sprintf("synthesis: primitive %q kind=factory needs func() *cpn.CPN, got %T", p.Name, impl))
		}
		r.factories[p.Name] = fn
	default:
		panic(fmt.Sprintf("synthesis: primitive %q has unknown kind %q", p.Name, p.Kind))
	}
	r.primitives[p.Name] = p
}

// Seal freezes the registry. After Seal, Register panics and the
// catalogue JSON is cached for cheap prompt-injection.
func (r *SafeRegistry) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
}

// Lookup implements cpn.SafeRegistryPort — returns the kind of the named
// primitive, or ("", false) if absent.
func (r *SafeRegistry) Lookup(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.primitives[name]
	if !ok {
		return "", false
	}
	return p.Kind, true
}

// Catalogue implements cpn.SafeRegistryPort — returns a JSON array of every
// primitive, sorted by name for determinism. Safe for prompt injection.
func (r *SafeRegistry) Catalogue() json.RawMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.primitives))
	for n := range r.primitives {
		names = append(names, n)
	}
	sort.Strings(names)
	entries := make([]SafePrimitive, 0, len(names))
	for _, n := range names {
		entries = append(entries, r.primitives[n])
	}
	b, _ := json.Marshal(entries)
	return b
}

// Names implements cpn.SafeRegistryPort — sorted list of every primitive.
func (r *SafeRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.primitives))
	for n := range r.primitives {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// BuildSafeFuncRegistry returns a fresh persist.FuncRegistry populated with
// every safe primitive's Go implementation. The instantiate materialiser
// uses this registry to rehydrate agent-authored topology JSON back into a
// runnable *cpn.CPN.
//
// The returned registry is independent of the platform FuncRegistry so
// safe-only execution never accidentally resolves a non-whitelisted name.
func (r *SafeRegistry) BuildSafeFuncRegistry() *persist.FuncRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg := persist.NewFuncRegistry()
	for name, fn := range r.guards {
		reg.RegisterGuard(name, fn)
	}
	for name, fn := range r.executors {
		reg.RegisterExecutor(name, fn)
	}
	for name, fn := range r.factories {
		reg.RegisterSubNetFactory(name, fn)
	}
	return reg
}

// SetBashSnippets seeds the pre-approved bash catalogue used by
// exec-bash-preregistered. Empty map is legal.
func (r *SafeRegistry) SetBashSnippets(snippets map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bashSnippets = make(map[string]string, len(snippets))
	for k, v := range snippets {
		r.bashSnippets[k] = v
	}
}

// BashSnippet returns the pre-approved command for the given id (empty
// string + false if absent). Safe for concurrent use.
func (r *SafeRegistry) BashSnippet(id string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.bashSnippets[id]
	return s, ok
}

// LoadBashSnippetsFile reads a YAML file of snippet_id → command mappings.
// The file shape is intentionally trivial:
//
//	snippets:
//	  list-home: "ls -la $HOME"
//	  check-git: "git status --short"
//
// Missing file is not an error — an empty catalogue is legal. Parse errors
// propagate so the operator sees them at boot.
func (r *SafeRegistry) LoadBashSnippetsFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.SetBashSnippets(nil)
			return nil
		}
		return fmt.Errorf("synthesis: read %s: %w", path, err)
	}
	var doc struct {
		Snippets map[string]string `yaml:"snippets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("synthesis: parse %s: %w", path, err)
	}
	r.SetBashSnippets(doc.Snippets)
	return nil
}

// ── Default primitive implementations ─────────────────────────────────────

// RegisterDefaults enrols the spec §3 REQ-030 catalogue. Call once at boot
// before Seal. Safe to call on a fresh registry only — panics on a sealed
// registry.
func RegisterDefaults(r *SafeRegistry) {
	// ── Guards ────────────────────────────────────────────────────────
	r.RegisterSafePrimitive(SafePrimitive{
		Name: "guard-json-nonempty", Kind: PrimitiveKindGuard,
		Output:   "bool",
		CostHint: "free",
	}, guardJSONNonEmpty)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "guard-timer-elapsed", Kind: PrimitiveKindGuard,
		Output:   "bool",
		CostHint: "free",
	}, guardTimerElapsed)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "guard-regex-match", Kind: PrimitiveKindGuard,
		Output:   "bool",
		CostHint: "free",
	}, guardRegexMatch)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "guard-exit-code-zero", Kind: PrimitiveKindGuard,
		Output:   "bool",
		CostHint: "free",
	}, guardExitCodeZero)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "guard-cost-under-budget", Kind: PrimitiveKindGuard,
		Output:   "bool",
		CostHint: "free",
	}, guardCostUnderBudget)

	// ── Executors ─────────────────────────────────────────────────────
	r.RegisterSafePrimitive(SafePrimitive{
		Name: "exec-noop", Kind: PrimitiveKindExecutor,
		Output:   "any",
		CostHint: "free",
	}, execNoop)

	r.RegisterSafePrimitive(SafePrimitive{
		Name:   "exec-http-get",
		Kind:   PrimitiveKindExecutor,
		Params: json.RawMessage(`{"url":"string"}`),
		Output: "json",
	}, execHTTPGetPlaceholder)

	r.RegisterSafePrimitive(SafePrimitive{
		Name:   "exec-bash-preregistered",
		Kind:   PrimitiveKindExecutor,
		Params: json.RawMessage(`{"snippet_id":"string"}`),
		Output: "string",
	}, r.execBashPreregistered)

	r.RegisterSafePrimitive(SafePrimitive{
		Name:   "exec-format-string",
		Kind:   PrimitiveKindExecutor,
		Params: json.RawMessage(`{"template":"string"}`),
		Output: "string",
	}, execFormatString)

	// ── Factories (sub-CPNs) ──────────────────────────────────────────
	r.RegisterSafePrimitive(SafePrimitive{
		Name: "factory-llm-respond", Kind: PrimitiveKindFactory,
		Output: "artifact",
	}, factoryLLMRespond)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "factory-validate-json", Kind: PrimitiveKindFactory,
		Output: "json",
	}, factoryValidateJSON)

	r.RegisterSafePrimitive(SafePrimitive{
		Name: "factory-observer", Kind: PrimitiveKindFactory,
		Output: "artifact",
	}, factoryObserver)
}

// ── Guard implementations ─────────────────────────────────────────────────

func guardJSONNonEmpty(tokens []*cpn.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		switch v := tok.Payload.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "{}" && strings.TrimSpace(v) != "null" {
				return true
			}
		case []byte:
			s := strings.TrimSpace(string(v))
			if s != "" && s != "{}" && s != "null" {
				return true
			}
		case map[string]any:
			if len(v) > 0 {
				return true
			}
		case json.RawMessage:
			s := strings.TrimSpace(string(v))
			if s != "" && s != "{}" && s != "null" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// guardTimerElapsed returns true once the first token's Timestamp is older
// than 1s. Deliberately conservative — synthesised topologies that need a
// tighter timer can re-emit tokens.
func guardTimerElapsed(tokens []*cpn.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok == nil || tok.Timestamp.IsZero() {
			continue
		}
		if time.Since(tok.Timestamp) > time.Second {
			return true
		}
	}
	return false
}

// guardRegexMatch matches the first string-payload token against a regex
// baked into the token payload (map key "pattern"). Safe: operates on
// already-consumed token data, no reflection magic.
func guardRegexMatch(tokens []*cpn.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		text, pattern := coerceRegexInputs(tok.Payload)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func coerceRegexInputs(payload any) (text, pattern string) {
	switch v := payload.(type) {
	case string:
		return v, ""
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			text = t
		}
		if p, ok := v["pattern"].(string); ok {
			pattern = p
		}
	}
	return text, pattern
}

func guardExitCodeZero(tokens []*cpn.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		if v, ok := tok.Payload.(map[string]any); ok {
			if code, ok := v["exit_code"]; ok {
				switch c := code.(type) {
				case int:
					if c == 0 {
						return true
					}
				case int64:
					if c == 0 {
						return true
					}
				case float64:
					if c == 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

func guardCostUnderBudget(tokens []*cpn.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		if v, ok := tok.Payload.(map[string]any); ok {
			spent, _ := v["spent_usd"].(float64)
			budget, _ := v["budget_usd"].(float64)
			if budget > 0 && spent < budget {
				return true
			}
		}
	}
	return false
}

// ── Executor implementations ─────────────────────────────────────────────

func execNoop(_ context.Context, in cpn.Token) (cpn.Token, error) {
	return in, nil
}

// execHTTPGetPlaceholder is a conservative default — the real HTTP fetcher
// is expected to be wired later via the tool registry. Safe default so a
// synthesised topology does not immediately reach out to the network when
// nobody has plumbed an adapter. Returns a JSON-encoded marker.
func execHTTPGetPlaceholder(_ context.Context, in cpn.Token) (cpn.Token, error) {
	out := in
	out.Color = cpn.ColorJSON
	out.Payload = map[string]any{
		"status":  0,
		"body":    "",
		"note":    "exec-http-get: no HTTP adapter wired",
		"request": in.Payload,
	}
	return out, nil
}

func (r *SafeRegistry) execBashPreregistered(_ context.Context, in cpn.Token) (cpn.Token, error) {
	id, _ := in.Payload.(string)
	if id == "" {
		if m, ok := in.Payload.(map[string]any); ok {
			if s, ok := m["snippet_id"].(string); ok {
				id = s
			}
		}
	}
	if id == "" {
		return cpn.Token{}, errors.New("exec-bash-preregistered: snippet_id missing")
	}
	cmd, ok := r.BashSnippet(id)
	if !ok {
		return cpn.Token{}, fmt.Errorf("exec-bash-preregistered: unknown snippet_id %q", id)
	}
	out := in
	out.Color = cpn.ColorString
	out.Payload = cmd
	return out, nil
}

func execFormatString(_ context.Context, in cpn.Token) (cpn.Token, error) {
	var template string
	var args map[string]string
	switch v := in.Payload.(type) {
	case string:
		template = v
	case map[string]any:
		if t, ok := v["template"].(string); ok {
			template = t
		}
		if a, ok := v["args"].(map[string]any); ok {
			args = make(map[string]string, len(a))
			for k, val := range a {
				args[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	if template == "" {
		return cpn.Token{}, errors.New("exec-format-string: template missing")
	}
	rendered := template
	for k, v := range args {
		rendered = strings.ReplaceAll(rendered, "{{"+k+"}}", v)
	}
	out := in
	out.Color = cpn.ColorString
	out.Payload = rendered
	return out, nil
}

// ── Factory implementations ──────────────────────────────────────────────

// factoryLLMRespond, factoryValidateJSON, and factoryObserver return empty
// placeholder CPNs. A production build wires real sub-nets; the synthesiser
// happily references these by name, and the instantiate path resolves them
// through BuildSafeFuncRegistry. Tests and dev builds get well-typed stubs
// so the executor dispatch never panics.

func factoryLLMRespond() *cpn.CPN {
	in := cpn.NewPlace("p-in", cpn.ColorString, cpn.SpaceComputation)
	out := cpn.NewPlace("p-out", cpn.ColorArtifact, cpn.SpaceComputation)
	tr := cpn.NewTransition("t-echo", cpn.NodeKindTool, []string{"p-in"}, []string{"p-out"})
	tr.Executor = func(_ context.Context, t cpn.Token) (cpn.Token, error) {
		out := t
		out.Color = cpn.ColorArtifact
		return out, nil
	}
	return cpn.NewCPN("factory-llm-respond", "llm-respond", 0, cpn.ModeMAS, "",
		map[string]*cpn.Place{"p-in": in, "p-out": out},
		map[string]*cpn.Transition{"t-echo": tr},
	)
}

func factoryValidateJSON() *cpn.CPN {
	in := cpn.NewPlace("p-in", cpn.ColorString, cpn.SpaceComputation)
	out := cpn.NewPlace("p-out", cpn.ColorJSON, cpn.SpaceComputation)
	tr := cpn.NewTransition("t-validate", cpn.NodeKindTool, []string{"p-in"}, []string{"p-out"})
	tr.Executor = func(_ context.Context, t cpn.Token) (cpn.Token, error) {
		out := t
		out.Color = cpn.ColorJSON
		return out, nil
	}
	return cpn.NewCPN("factory-validate-json", "validate-json", 0, cpn.ModeMAS, "",
		map[string]*cpn.Place{"p-in": in, "p-out": out},
		map[string]*cpn.Transition{"t-validate": tr},
	)
}

func factoryObserver() *cpn.CPN {
	in := cpn.NewPlace("p-in", cpn.ColorString, cpn.SpaceComputation)
	out := cpn.NewPlace("p-out", cpn.ColorArtifact, cpn.SpaceComputation)
	tr := cpn.NewTransition("t-observe", cpn.NodeKindTool, []string{"p-in"}, []string{"p-out"})
	tr.Executor = func(_ context.Context, t cpn.Token) (cpn.Token, error) {
		out := t
		out.Color = cpn.ColorArtifact
		return out, nil
	}
	return cpn.NewCPN("factory-observer", "observer", 0, cpn.ModeMAS, "",
		map[string]*cpn.Place{"p-in": in, "p-out": out},
		map[string]*cpn.Transition{"t-observe": tr},
	)
}
