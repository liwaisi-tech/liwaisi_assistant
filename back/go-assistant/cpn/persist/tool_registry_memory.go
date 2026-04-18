package persist

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryToolRegistryRepository is an in-memory ToolRegistryRepository for
// tests and in-process boot scenarios. It is safe for concurrent use.
type MemoryToolRegistryRepository struct {
	mu      sync.RWMutex
	entries map[string]ToolRegistryEntry // key = namespace/name@version
}

// NewMemoryToolRegistryRepository creates an empty in-memory repository.
func NewMemoryToolRegistryRepository() *MemoryToolRegistryRepository {
	return &MemoryToolRegistryRepository{
		entries: make(map[string]ToolRegistryEntry),
	}
}

var _ ToolRegistryRepository = (*MemoryToolRegistryRepository)(nil)

// Upsert persists the entry. Returns ErrToolDuplicate on conflict.
func (r *MemoryToolRegistryRepository) Upsert(_ context.Context, entry ToolRegistryEntry) error {
	if entry.Namespace == "" || entry.Name == "" || entry.Version == "" {
		return ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := entry.QualifiedName()
	if _, exists := r.entries[key]; exists {
		return ErrToolDuplicate
	}
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now().UTC()
	}
	r.entries[key] = entry
	return nil
}

// Get resolves a qualified name (ns/name@ver exact, or ns/name latest).
func (r *MemoryToolRegistryRepository) Get(ctx context.Context, qualifiedName string) (ToolRegistryEntry, error) {
	ns, name, version, ok := SplitQualifiedName(qualifiedName)
	if !ok {
		return ToolRegistryEntry{}, ErrInvalidInput
	}
	if version != "" {
		return r.GetVersion(ctx, ns, name, version)
	}
	return r.Latest(ctx, ns, name)
}

// GetVersion fetches a specific (namespace, name, version).
func (r *MemoryToolRegistryRepository) GetVersion(_ context.Context, namespace, name, version string) (ToolRegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := namespace + "/" + name + "@" + version
	e, ok := r.entries[key]
	if !ok {
		return ToolRegistryEntry{}, ErrToolNotFound
	}
	return e, nil
}

// ListByNamespace returns every version persisted under the given namespace.
func (r *MemoryToolRegistryRepository) ListByNamespace(_ context.Context, namespace string) ([]ToolRegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ToolRegistryEntry
	for _, e := range r.entries {
		if e.Namespace == namespace {
			out = append(out, e)
		}
	}
	sortEntriesStable(out)
	return out, nil
}

// ListAll returns every persisted row.
func (r *MemoryToolRegistryRepository) ListAll(_ context.Context) ([]ToolRegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolRegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sortEntriesStable(out)
	return out, nil
}

// Deprecate flags a specific (qualified name WITH version) as deprecated.
func (r *MemoryToolRegistryRepository) Deprecate(_ context.Context, qualifiedName, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[qualifiedName]
	if !ok {
		return ErrToolNotFound
	}
	e.Deprecated = true
	e.DeprecatedAt = time.Now().UTC()
	e.DeprecationReason = reason
	r.entries[qualifiedName] = e
	return nil
}

// Latest returns the highest semver among non-deprecated entries with the
// given (namespace, name).
func (r *MemoryToolRegistryRepository) Latest(_ context.Context, namespace, name string) (ToolRegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var candidates []ToolRegistryEntry
	for _, e := range r.entries {
		if e.Namespace == namespace && e.Name == name && !e.Deprecated {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		return ToolRegistryEntry{}, ErrToolNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		return CompareSemver(candidates[i].Version, candidates[j].Version) > 0
	})
	return candidates[0], nil
}

// Delete hard-deletes a qualified name.
func (r *MemoryToolRegistryRepository) Delete(_ context.Context, qualifiedName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[qualifiedName]; !ok {
		return ErrToolNotFound
	}
	delete(r.entries, qualifiedName)
	return nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

// SplitQualifiedName parses "ns/name@ver" or "ns/name". Returns (ns, name, ver, ok).
// An empty ver means caller wants latest.
func SplitQualifiedName(qn string) (namespace, name, version string, ok bool) {
	slash := strings.Index(qn, "/")
	if slash <= 0 || slash == len(qn)-1 {
		return "", "", "", false
	}
	namespace = qn[:slash]
	rest := qn[slash+1:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return namespace, rest, "", true
	}
	if at == 0 || at == len(rest)-1 {
		return "", "", "", false
	}
	return namespace, rest[:at], rest[at+1:], true
}

// CompareSemver returns >0 if a > b, <0 if a < b, 0 if equal. It tolerates
// non-strict semver by comparing dotted components numerically where possible,
// lexicographically otherwise. Anything beyond the third component is compared
// lexicographically so "1.0.0-rc1" sorts below "1.0.0".
func CompareSemver(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		var av, bv string
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		// The 3rd component may carry a pre-release suffix (e.g. "0-rc1").
		var aHead, aTail string
		if dash := strings.Index(av, "-"); dash >= 0 {
			aHead = av[:dash]
			aTail = av[dash:]
		} else {
			aHead = av
		}
		var bHead, bTail string
		if dash := strings.Index(bv, "-"); dash >= 0 {
			bHead = bv[:dash]
			bTail = bv[dash:]
		} else {
			bHead = bv
		}
		if c := compareNumericOrLex(aHead, bHead); c != 0 {
			return c
		}
		// Tail: an empty tail ranks HIGHER than any non-empty tail
		// (release > pre-release).
		if aTail == bTail {
			continue
		}
		if aTail == "" {
			return 1
		}
		if bTail == "" {
			return -1
		}
		if aTail < bTail {
			return -1
		}
		return 1
	}
	return 0
}

func compareNumericOrLex(a, b string) int {
	ai, aOK := atoiSafe(a)
	bi, bOK := atoiSafe(b)
	if aOK && bOK {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}

func atoiSafe(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func sortEntriesStable(s []ToolRegistryEntry) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Namespace != s[j].Namespace {
			return s[i].Namespace < s[j].Namespace
		}
		if s[i].Name != s[j].Name {
			return s[i].Name < s[j].Name
		}
		return CompareSemver(s[i].Version, s[j].Version) < 0
	})
}
