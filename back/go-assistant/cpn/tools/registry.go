package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Public types ───────────────────────────────────────────────────────────

// ToolExecutor is the in-process tool handler signature.
type ToolExecutor = func(ctx context.Context, in cpn.Token) (cpn.Token, error)

// Origin classifies who authored a tool.
const (
	OriginBuiltin       = "builtin"
	OriginUser          = "user"
	OriginAgentAuthored = "agent-authored"
)

// ToolSchema holds metadata describing a tool available in the CPN system.
// Retained for backward compatibility with the legacy registration path.
type ToolSchema struct {
	Name         string          `json:"name"`
	Namespace    string          `json:"namespace"`
	Description  string          `json:"description"`
	InputColor   cpn.ColorSet    `json:"input_color"`
	OutputColor  cpn.ColorSet    `json:"output_color"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
	RequiresHITL bool            `json:"requires_hitl"`
	Version      string          `json:"version"`
}

// QualifiedName returns "namespace/name".
func (s *ToolSchema) QualifiedName() string {
	return s.Namespace + "/" + s.Name
}

// ToolEntry is the runtime-registry record introduced by GAP-3. The legacy
// Schema/Executor pair is retained so existing consumers (personality tools,
// FuncRegistry injection, LLM tool assembly) keep working without churn; the
// new flat fields back the persistent registry.
type ToolEntry struct {
	// Legacy fields — populated for all tools, builtin or authored.
	Schema   *ToolSchema
	Executor ToolExecutor

	// GAP-3 runtime-registry fields.
	ID                string
	Namespace         string
	Name              string
	Version           string
	JSONSchema        json.RawMessage
	HelpText          string
	ManPage           string
	BinaryPath        string
	BinarySHA256      string
	Origin            string
	Provenance        persist.Provenance
	RegisteredAt      time.Time
	RegisteredBy      string
	Deprecated        bool
	DeprecatedAt      time.Time
	DeprecationReason string
}

// QualifiedName returns "namespace/name@version" when Version is set, else
// falls back to "namespace/name".
func (e *ToolEntry) QualifiedName() string {
	ns, name := e.resolveNS()
	if e.Version != "" {
		return ns + "/" + name + "@" + e.Version
	}
	return ns + "/" + name
}

// Anchor returns "namespace/name" (no version tag).
func (e *ToolEntry) Anchor() string {
	ns, name := e.resolveNS()
	return ns + "/" + name
}

func (e *ToolEntry) resolveNS() (string, string) {
	ns := e.Namespace
	if ns == "" && e.Schema != nil {
		ns = e.Schema.Namespace
	}
	name := e.Name
	if name == "" && e.Schema != nil {
		name = e.Schema.Name
	}
	return ns, name
}

// ToolFilter constrains List queries.
type ToolFilter struct {
	Origin     string
	Namespace  string
	Deprecated *bool // nil = no filter
}

// ── Registry ───────────────────────────────────────────────────────────────

// Registry is a thread-safe tool registry keyed by anchor (namespace/name)
// with a secondary index over qualified names (namespace/name@version). It
// optionally persists to a ToolRegistryRepository; when nil, it operates in
// pure in-memory mode (legacy behavior preserved).
type Registry struct {
	mu sync.RWMutex

	byAnchor    map[string][]*ToolEntry // anchor → versions, desc semver
	byQualified map[string]*ToolEntry   // ns/name@ver OR ns/name

	sealed bool
	repo   persist.ToolRegistryRepository

	emit func(e ToolRegistryEvent)
}

// ToolRegistryEvent is emitted for tool-registry mutations.
type ToolRegistryEvent struct {
	Type          string // "tool_registered" | "tool_deprecated" | "tool_deprecation_warning"
	QualifiedName string
	Entry         *ToolEntry
	Reason        string
	Timestamp     time.Time
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byAnchor:    make(map[string][]*ToolEntry),
		byQualified: make(map[string]*ToolEntry),
	}
}

// OnEvent installs an event listener. Passing nil clears the listener.
func (r *Registry) OnEvent(f func(e ToolRegistryEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emit = f
}

// Bootstrap loads every persisted row into memory and installs the repo
// handle so subsequent RegisterEntry calls persist. MUST be called before
// any agent-driven RegisterEntry; builtin Register(schema, exec) calls MAY
// run before or after and will overwrite only when the persisted origin is
// also "builtin".
func (r *Registry) Bootstrap(ctx context.Context, repo persist.ToolRegistryRepository) error {
	if repo == nil {
		return fmt.Errorf("tools: Bootstrap: nil repository")
	}
	rows, err := repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("tools: Bootstrap: list all: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repo = repo
	for i := range rows {
		entry := fromPersistEntry(rows[i])
		r.insertLocked(entry)
	}
	return nil
}

// Repository returns the wired repository (may be nil).
func (r *Registry) Repository() persist.ToolRegistryRepository {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.repo
}

// ── Legacy API (unchanged behavior) ────────────────────────────────────────

// Register adds a tool using the legacy Schema/Executor pair. Retained so
// existing call sites keep compiling and behaving identically.
func (r *Registry) Register(schema *ToolSchema, exec ToolExecutor) error {
	if schema == nil || schema.Name == "" || schema.Namespace == "" {
		return ErrInvalidToolSchema
	}

	qn := schema.QualifiedName()

	r.mu.Lock()
	defer r.mu.Unlock()

	if schema.Namespace == "system" && r.sealed {
		return fmt.Errorf("%w: %s", ErrSystemNamespaceSealed, qn)
	}

	if existing := r.anchorBuiltinLocked(qn); existing != nil {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, qn)
	}

	version := schema.Version
	if version == "" {
		version = "0.1.0"
	}
	entry := &ToolEntry{
		Schema:       schema,
		Executor:     exec,
		ID:           uuid.New().String(),
		Namespace:    schema.Namespace,
		Name:         schema.Name,
		Version:      version,
		JSONSchema:   schema.Parameters,
		HelpText:     schema.Description,
		Origin:       OriginBuiltin,
		RegisteredAt: time.Now().UTC(),
		RegisteredBy: "builtin",
	}

	r.insertLocked(entry)
	return nil
}

func (r *Registry) anchorBuiltinLocked(anchor string) *ToolEntry {
	entries, ok := r.byAnchor[anchor]
	if !ok {
		return nil
	}
	for _, e := range entries {
		if e.Origin == OriginBuiltin || e.Origin == "" {
			return e
		}
	}
	return nil
}

// Seal locks the system namespace.
func (r *Registry) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
}

// IsSealed reports whether the system namespace is locked.
func (r *Registry) IsSealed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sealed
}

// Resolve looks up a tool by anchor qualified name ("namespace/name") or
// exact qualified name ("namespace/name@version"). Returns latest
// non-deprecated version when anchor resolution has multiple versions.
func (r *Registry) Resolve(qualifiedName string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveLocked(qualifiedName)
}

func (r *Registry) resolveLocked(qualifiedName string) (*ToolEntry, bool) {
	if entry, ok := r.byQualified[qualifiedName]; ok {
		return entry, true
	}
	if entries, ok := r.byAnchor[qualifiedName]; ok && len(entries) > 0 {
		if latest := pickLatestNonDeprecated(entries); latest != nil {
			return latest, true
		}
		return entries[0], true
	}
	return nil, false
}

// List returns all schemas registered under the given namespace.
func (r *Registry) List(namespace string) []*ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*ToolSchema
	for _, entries := range r.byAnchor {
		for _, e := range entries {
			if e.Namespace != namespace {
				continue
			}
			if e.Schema != nil {
				out = append(out, e.Schema)
			}
		}
	}
	return out
}

// ListAll returns every registered tool schema (legacy).
func (r *Registry) ListAll() []*ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*ToolSchema, 0)
	for _, entries := range r.byAnchor {
		for _, e := range entries {
			if e.Schema != nil {
				out = append(out, e.Schema)
			}
		}
	}
	return out
}

// AsLLMTools converts registered tools to []*cpn.LLMTool. Deprecated entries
// are filtered out.
func (r *Registry) AsLLMTools(namespaces ...string) []*cpn.LLMTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filter := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		filter[ns] = struct{}{}
	}

	out := make([]*cpn.LLMTool, 0)
	for _, entries := range r.byAnchor {
		for _, e := range entries {
			if e.Deprecated {
				continue
			}
			if len(filter) > 0 {
				if _, ok := filter[e.Namespace]; !ok {
					continue
				}
			}
			if e.Schema == nil {
				continue
			}
			out = append(out, &cpn.LLMTool{
				Name:        e.Schema.QualifiedName(),
				Description: e.Schema.Description,
				Parameters:  e.Schema.Parameters,
			})
		}
	}
	return out
}

// registrationName converts a qualified name to a FuncRegistry-safe key.
func registrationName(qn string) string {
	parts := strings.SplitN(qn, "/", 2)
	if len(parts) != 2 {
		return "tool-exec-" + strings.ReplaceAll(qn, ".", "-")
	}
	ns := strings.ReplaceAll(parts[0], ".", "-")
	name := strings.ReplaceAll(parts[1], ".", "-")
	return "tool-exec-" + ns + "-" + name
}

// InjectIntoCPN populates ToolMeta and Executor on CPN transitions from the
// registry.
func (r *Registry) InjectIntoCPN(c *cpn.CPN) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, t := range c.Transitions {
		if t.ToolName == "" {
			continue
		}
		entry, ok := r.resolveLocked(t.ToolName)
		if !ok || entry.Schema == nil {
			continue
		}
		t.ToolMeta = &cpn.ToolMeta{
			Description:  entry.Schema.Description,
			Parameters:   entry.Schema.Parameters,
			RequiresHITL: entry.Schema.RequiresHITL,
			Namespace:    entry.Schema.Namespace,
		}
		if t.Executor == nil && entry.Executor != nil {
			t.Executor = entry.Executor
		}
	}
}

// InjectIntoFuncRegistry registers every tool executor into the given
// FuncRegistry so that CPN topologies can serialize tool references as
// string keys.
func (r *Registry) InjectIntoFuncRegistry(fr *persist.FuncRegistry) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, entries := range r.byAnchor {
		for _, e := range entries {
			if e.Executor == nil {
				continue
			}
			fr.RegisterExecutor(registrationName(e.Anchor()), e.Executor)
		}
	}
}

// ── GAP-3 runtime API ──────────────────────────────────────────────────────

// RegisterEntry adds a rich ToolEntry to the registry. Used by
// NodeKindRegisterTool and the admin REST API.
func (r *Registry) RegisterEntry(ctx context.Context, entry *ToolEntry) error {
	if entry == nil {
		return wrapErr(ErrInvalidInput, "nil entry", nil)
	}
	if entry.Namespace == "" || entry.Name == "" || entry.Version == "" {
		return wrapErr(ErrInvalidInput, "namespace/name/version are required", nil)
	}
	if entry.Origin == "" {
		entry.Origin = OriginAgentAuthored
	}
	if err := validateJSONSchema(entry.JSONSchema); err != nil {
		return wrapErr(ErrSchemaInvalid, err.Error(), err)
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now().UTC()
	}
	if entry.Schema == nil {
		entry.Schema = &ToolSchema{
			Name:        entry.Name,
			Namespace:   entry.Namespace,
			Description: entry.HelpText,
			Parameters:  entry.JSONSchema,
			Version:     entry.Version,
		}
	}

	r.mu.Lock()
	qn := entry.QualifiedName()
	if _, exists := r.byQualified[qn]; exists {
		r.mu.Unlock()
		return wrapErr(ErrDuplicate, qn, nil)
	}
	repo := r.repo
	r.mu.Unlock()

	if repo != nil {
		if err := repo.Upsert(ctx, toPersistEntry(entry)); err != nil {
			if isPersistDuplicate(err) {
				return wrapErr(ErrDuplicate, qn, err)
			}
			return wrapErr(ErrInvalidInput, "persist upsert", err)
		}
	}

	r.mu.Lock()
	// Re-check duplicate under the lock (races with RegisterEntry(2)).
	if _, exists := r.byQualified[qn]; exists {
		r.mu.Unlock()
		return wrapErr(ErrDuplicate, qn, nil)
	}
	r.insertLocked(entry)
	listener := r.emit
	r.mu.Unlock()

	if listener != nil {
		listener(ToolRegistryEvent{
			Type:          "tool_registered",
			QualifiedName: qn,
			Entry:         entry,
			Timestamp:     time.Now().UTC(),
		})
	}
	return nil
}

// Deprecate flags the specified qualified-name-with-version as deprecated.
func (r *Registry) Deprecate(ctx context.Context, qualifiedName, reason string) error {
	if qualifiedName == "" {
		return wrapErr(ErrInvalidInput, "qualified name required", nil)
	}
	_, _, version, ok := persist.SplitQualifiedName(qualifiedName)
	if !ok || version == "" {
		return wrapErr(ErrInvalidInput, "expected namespace/name@version", nil)
	}

	r.mu.Lock()
	entry, exists := r.byQualified[qualifiedName]
	if !exists {
		r.mu.Unlock()
		return wrapErr(ErrNotFound, qualifiedName, nil)
	}
	repo := r.repo
	r.mu.Unlock()

	if repo != nil {
		if err := repo.Deprecate(ctx, qualifiedName, reason); err != nil {
			if err == persist.ErrToolNotFound {
				return wrapErr(ErrNotFound, qualifiedName, err)
			}
			return wrapErr(ErrInvalidInput, "persist deprecate", err)
		}
	}

	r.mu.Lock()
	entry.Deprecated = true
	entry.DeprecatedAt = time.Now().UTC()
	entry.DeprecationReason = reason
	listener := r.emit
	r.mu.Unlock()

	if listener != nil {
		listener(ToolRegistryEvent{
			Type:          "tool_deprecated",
			QualifiedName: qualifiedName,
			Entry:         entry,
			Reason:        reason,
			Timestamp:     time.Now().UTC(),
		})
	}
	return nil
}

// Unregister hard-deletes a qualified-name. Only allowed when the entry's
// Origin is "agent-authored"; any other origin returns ErrForbidden.
func (r *Registry) Unregister(ctx context.Context, qualifiedName string) error {
	if qualifiedName == "" {
		return wrapErr(ErrInvalidInput, "qualified name required", nil)
	}
	r.mu.Lock()
	entry, exists := r.byQualified[qualifiedName]
	if !exists {
		r.mu.Unlock()
		return wrapErr(ErrNotFound, qualifiedName, nil)
	}
	if entry.Origin != OriginAgentAuthored {
		r.mu.Unlock()
		return wrapErr(ErrForbidden, "delete only allowed for origin=agent-authored", nil)
	}
	repo := r.repo
	r.mu.Unlock()

	if repo != nil {
		if err := repo.Delete(ctx, qualifiedName); err != nil {
			if err == persist.ErrToolNotFound {
				return wrapErr(ErrNotFound, qualifiedName, err)
			}
			return wrapErr(ErrInvalidInput, "persist delete", err)
		}
	}

	r.mu.Lock()
	r.removeLocked(entry)
	r.mu.Unlock()
	return nil
}

// Get fetches an entry by qualified name. Version-bearing names resolve
// exactly; anchors resolve to latest non-deprecated.
func (r *Registry) Get(_ context.Context, qualifiedName string) (*ToolEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.resolveLocked(qualifiedName); ok {
		return entry, nil
	}
	return nil, wrapErr(ErrNotFound, qualifiedName, nil)
}

// Latest returns the highest-version non-deprecated entry for (namespace, name).
func (r *Registry) Latest(_ context.Context, namespace, name string) (*ToolEntry, error) {
	anchor := namespace + "/" + name
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, ok := r.byAnchor[anchor]
	if !ok || len(entries) == 0 {
		return nil, wrapErr(ErrNotFound, anchor, nil)
	}
	if latest := pickLatestNonDeprecated(entries); latest != nil {
		return latest, nil
	}
	return nil, wrapErr(ErrNotFound, "no non-deprecated entry for "+anchor, nil)
}

// Versions returns every version for the given (namespace, name) sorted
// ascending by semver.
func (r *Registry) Versions(_ context.Context, namespace, name string) ([]*ToolEntry, error) {
	anchor := namespace + "/" + name
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, ok := r.byAnchor[anchor]
	if !ok {
		return nil, wrapErr(ErrNotFound, anchor, nil)
	}
	out := make([]*ToolEntry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool {
		return persist.CompareSemver(out[i].Version, out[j].Version) < 0
	})
	return out, nil
}

// ListFiltered returns every entry matching the filter. Sorted by namespace
// then name, with highest-version first.
func (r *Registry) ListFiltered(_ context.Context, f ToolFilter) []*ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*ToolEntry
	for _, entries := range r.byAnchor {
		for _, e := range entries {
			if f.Origin != "" && e.Origin != f.Origin {
				continue
			}
			if f.Namespace != "" && e.Namespace != f.Namespace {
				continue
			}
			if f.Deprecated != nil && e.Deprecated != *f.Deprecated {
				continue
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return persist.CompareSemver(out[i].Version, out[j].Version) > 0
	})
	return out
}

// ResolveByName applies the GAP-3 resolution rules:
//   - "ns/name@ver" → exact match
//   - "ns/name"     → latest non-deprecated version
//   - "bare"        → search across namespaces for latest non-deprecated;
//     deterministic tie-break: alphabetical namespace first.
func (r *Registry) ResolveByName(_ context.Context, nameOrQualified string) (*ToolEntry, error) {
	if nameOrQualified == "" {
		return nil, wrapErr(ErrInvalidInput, "empty name", nil)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if strings.Contains(nameOrQualified, "/") {
		if entry, ok := r.resolveLocked(nameOrQualified); ok {
			return entry, nil
		}
		return nil, wrapErr(ErrNotFound, nameOrQualified, nil)
	}

	bareName := nameOrQualified
	var matches []*ToolEntry
	var matchedAnchors []string
	for anchor, entries := range r.byAnchor {
		if len(entries) == 0 || entries[0].Name != bareName {
			continue
		}
		if latest := pickLatestNonDeprecated(entries); latest != nil {
			matches = append(matches, latest)
			matchedAnchors = append(matchedAnchors, anchor)
		}
	}
	if len(matches) == 0 {
		return nil, wrapErr(ErrNotFound, bareName, nil)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	sort.Slice(matches, func(i, j int) bool {
		return matchedAnchors[i] < matchedAnchors[j]
	})
	return matches[0], nil
}

// VerifyBinary re-hashes the BinaryPath against BinarySHA256 and returns
// ErrBinaryDrift on mismatch. Entries with empty BinaryPath/BinarySHA256
// are considered pure-Go and return nil.
func (r *Registry) VerifyBinary(entry *ToolEntry) error {
	if entry == nil || entry.BinaryPath == "" || entry.BinarySHA256 == "" {
		return nil
	}
	f, err := os.Open(entry.BinaryPath)
	if err != nil {
		return wrapErr(ErrBinaryDrift, "open binary", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return wrapErr(ErrBinaryDrift, "read binary", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, entry.BinarySHA256) {
		return wrapErr(ErrBinaryDrift, fmt.Sprintf("%s: expected %s, got %s", entry.BinaryPath, entry.BinarySHA256, got), nil)
	}
	return nil
}

// EmitDeprecationWarning surfaces a tool_deprecation_warning event so
// observers can log invocations of deprecated tools.
func (r *Registry) EmitDeprecationWarning(entry *ToolEntry) {
	if entry == nil {
		return
	}
	r.mu.RLock()
	listener := r.emit
	r.mu.RUnlock()
	if listener == nil {
		return
	}
	listener(ToolRegistryEvent{
		Type:          "tool_deprecation_warning",
		QualifiedName: entry.QualifiedName(),
		Entry:         entry,
		Reason:        entry.DeprecationReason,
		Timestamp:     time.Now().UTC(),
	})
}

// RegisterManifest implements cpn.ToolRegistry — the port the executor uses
// to publish tools authored at runtime.
func (r *Registry) RegisterManifest(ctx context.Context, m cpn.ToolManifest) (cpn.ToolManifestResult, error) {
	entry := &ToolEntry{
		Namespace:    m.Namespace,
		Name:         m.Name,
		Version:      m.Version,
		JSONSchema:   m.Schema,
		HelpText:     m.HelpText,
		ManPage:      m.ManPage,
		BinaryPath:   m.BinaryPath,
		BinarySHA256: m.BinarySHA256,
		Origin:       m.Origin,
		Provenance: persist.Provenance{
			AuthoringCPNID: m.Provenance.AuthoringCPNID,
			FlowHash:       m.Provenance.FlowHash,
			ForgeRunID:     m.Provenance.ForgeRunID,
			PromptDigest:   m.Provenance.PromptDigest,
			SourcePath:     m.Provenance.SourcePath,
			SourceSHA256:   m.Provenance.SourceSHA256,
		},
		RegisteredBy: m.RegisteredBy,
	}
	if err := r.RegisterEntry(ctx, entry); err != nil {
		return cpn.ToolManifestResult{}, err
	}
	return cpn.ToolManifestResult{
		QualifiedName: entry.QualifiedName(),
		ID:            entry.ID,
		RegisteredAt:  entry.RegisteredAt,
	}, nil
}

// ── Internal helpers ───────────────────────────────────────────────────────

func (r *Registry) insertLocked(entry *ToolEntry) {
	if entry.Version == "" {
		entry.Version = "0.1.0"
	}
	qn := entry.QualifiedName()
	anchor := entry.Anchor()
	r.byQualified[qn] = entry
	r.byAnchor[anchor] = appendSortedDesc(r.byAnchor[anchor], entry)
}

func (r *Registry) removeLocked(entry *ToolEntry) {
	delete(r.byQualified, entry.QualifiedName())
	anchor := entry.Anchor()
	entries := r.byAnchor[anchor]
	kept := entries[:0]
	for _, e := range entries {
		if e == entry {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(r.byAnchor, anchor)
	} else {
		r.byAnchor[anchor] = kept
	}
}

func appendSortedDesc(list []*ToolEntry, entry *ToolEntry) []*ToolEntry {
	list = append(list, entry)
	sort.Slice(list, func(i, j int) bool {
		return persist.CompareSemver(list[i].Version, list[j].Version) > 0
	})
	return list
}

func pickLatestNonDeprecated(entries []*ToolEntry) *ToolEntry {
	for _, e := range entries {
		if !e.Deprecated {
			return e
		}
	}
	return nil
}

func toPersistEntry(e *ToolEntry) persist.ToolRegistryEntry {
	return persist.ToolRegistryEntry{
		ID:                e.ID,
		Namespace:         e.Namespace,
		Name:              e.Name,
		Version:           e.Version,
		Schema:            e.JSONSchema,
		HelpText:          e.HelpText,
		ManPage:           e.ManPage,
		BinaryPath:        e.BinaryPath,
		BinarySHA256:      e.BinarySHA256,
		Origin:            e.Origin,
		Provenance:        e.Provenance,
		RegisteredAt:      e.RegisteredAt,
		RegisteredBy:      e.RegisteredBy,
		Deprecated:        e.Deprecated,
		DeprecatedAt:      e.DeprecatedAt,
		DeprecationReason: e.DeprecationReason,
	}
}

func fromPersistEntry(row persist.ToolRegistryEntry) *ToolEntry {
	schema := &ToolSchema{
		Name:        row.Name,
		Namespace:   row.Namespace,
		Description: row.HelpText,
		Parameters:  row.Schema,
		Version:     row.Version,
	}
	return &ToolEntry{
		Schema:            schema,
		ID:                row.ID,
		Namespace:         row.Namespace,
		Name:              row.Name,
		Version:           row.Version,
		JSONSchema:        row.Schema,
		HelpText:          row.HelpText,
		ManPage:           row.ManPage,
		BinaryPath:        row.BinaryPath,
		BinarySHA256:      row.BinarySHA256,
		Origin:            row.Origin,
		Provenance:        row.Provenance,
		RegisteredAt:      row.RegisteredAt,
		RegisteredBy:      row.RegisteredBy,
		Deprecated:        row.Deprecated,
		DeprecatedAt:      row.DeprecatedAt,
		DeprecationReason: row.DeprecationReason,
	}
}

// validateJSONSchema performs a lightweight draft-2020-12 shape check. We
// only require the bytes parse as a JSON object — full validator is deferred.
func validateJSONSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("schema must be a JSON object: %w", err)
	}
	return nil
}

func isPersistDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if err == persist.ErrToolDuplicate {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "already exists") || strings.Contains(msg, "unique constraint")
}
