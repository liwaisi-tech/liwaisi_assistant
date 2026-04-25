package cpn

import (
	"sort"
	"sync"
	"time"
)

// FlowLibrary stores crystallized CPN topologies keyed by topology hash.
// Phase 1 shipped Register/Get by hash only. The architect lane
// (spec-architecture-cpn-agent-architect §4.2, §11) extends it with a
// FlowSignature index so the "does an existing CPN fit this request?" step
// is deterministic and LLM-free.
//
// Uses sync.RWMutex — read-heavy (Get/FindBy*), write-infrequent
// (Register*) (GUD-003).
type FlowLibrary struct {
	mu       sync.RWMutex
	flows    map[string]*FlowLibraryEntry // hash → entry
	byDigest map[string]string            // signature digest → hash
}

// FlowLibraryEntry bundles a crystallised topology with its structural
// signature and minimal provenance. Stored by value except for the *CPN
// which remains a pointer (topologies are not reconstructable cheaply).
type FlowLibraryEntry struct {
	Hash         string        // topology hash key
	CPN          *CPN          // crystallised topology
	Signature    FlowSignature // structural signature (see flow_signature.go)
	RegisteredAt time.Time     // when Register* first saw this hash
	Origin       string        // "jit" | "custom" | "user" | "agent-authored"
}

// NewFlowLibrary creates an empty FlowLibrary.
func NewFlowLibrary() *FlowLibrary {
	return &FlowLibrary{
		flows:    make(map[string]*FlowLibraryEntry),
		byDigest: make(map[string]string),
	}
}

// Register stores a CPN topology under the given hash (REQ-011).
// If the hash already exists, the entry is overwritten (last write wins).
// Retained for back-compat with the Phase-1 callers that have no signature
// yet; the entry is stored with an empty FlowSignature and Origin "custom".
func (fl *FlowLibrary) Register(c *CPN, hash string) {
	fl.RegisterEntry(&FlowLibraryEntry{
		Hash:      hash,
		CPN:       c,
		Signature: FlowSignature{Template: "custom"},
		Origin:    "custom",
	})
}

// RegisterWithSignature stores a topology together with a pre-computed
// signature. Use this path from the JIT composer or the architect so the
// library's signature index stays populated.
func (fl *FlowLibrary) RegisterWithSignature(c *CPN, hash string, sig FlowSignature, origin string) {
	fl.RegisterEntry(&FlowLibraryEntry{
		Hash:      hash,
		CPN:       c,
		Signature: sig,
		Origin:    origin,
	})
}

// RegisterEntry is the low-level write path. Normalises RegisteredAt, assigns
// an Origin default, and keeps the byDigest index consistent with flows.
func (fl *FlowLibrary) RegisterEntry(entry *FlowLibraryEntry) {
	if entry == nil || entry.Hash == "" {
		return
	}
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now()
	}
	if entry.Origin == "" {
		entry.Origin = "custom"
	}
	// Evict any previous digest pointer for this hash so lookups cannot
	// return stale mappings after an overwrite.
	if prev, ok := fl.flows[entry.Hash]; ok && prev != nil && prev.Signature.Digest != "" {
		if fl.byDigest[prev.Signature.Digest] == entry.Hash {
			delete(fl.byDigest, prev.Signature.Digest)
		}
	}
	fl.flows[entry.Hash] = entry
	if entry.Signature.Digest != "" {
		fl.byDigest[entry.Signature.Digest] = entry.Hash
	}
}

// Get retrieves a CPN topology by hash (REQ-011).
// Returns nil, false if the hash is not registered.
func (fl *FlowLibrary) Get(hash string) (*CPN, bool) {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	entry, ok := fl.flows[hash]
	if !ok || entry == nil {
		return nil, false
	}
	return entry.CPN, true
}

// GetEntry returns the full library entry (signature + metadata) for a hash.
func (fl *FlowLibrary) GetEntry(hash string) (*FlowLibraryEntry, bool) {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	entry, ok := fl.flows[hash]
	if !ok || entry == nil {
		return nil, false
	}
	// Return a shallow copy to prevent external callers mutating library state.
	cp := *entry
	return &cp, true
}

// FindByDigest returns the entry whose signature matches the given digest.
// O(1) lookup backed by byDigest.
func (fl *FlowLibrary) FindByDigest(digest string) (*FlowLibraryEntry, bool) {
	if digest == "" {
		return nil, false
	}
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	hash, ok := fl.byDigest[digest]
	if !ok {
		return nil, false
	}
	entry, ok := fl.flows[hash]
	if !ok || entry == nil {
		return nil, false
	}
	cp := *entry
	return &cp, true
}

// FindCovering returns every library entry whose signature covers the given
// requirements (hashtags ⊆ sig.Hashtags, caps ⊆ sig.RequiredCaps, input
// colours ⊆ sig.InputColors). Results are sorted by registration recency
// (newest first) so ties are broken deterministically without needing
// flow_ranking wired in here.
//
// Callers that want score-weighted ranking feed the result slice into
// flow_ranking.RankScore using ExecutionRecords retrieved elsewhere.
func (fl *FlowLibrary) FindCovering(hashtags, caps, inputColors []string) []*FlowLibraryEntry {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	out := make([]*FlowLibraryEntry, 0, len(fl.flows))
	for _, entry := range fl.flows {
		if entry == nil {
			continue
		}
		if !entry.Signature.Covers(hashtags, caps, inputColors) {
			continue
		}
		cp := *entry
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RegisteredAt.After(out[j].RegisteredAt)
	})
	return out
}

// ListEntries returns a snapshot of all entries, sorted by registration
// recency (newest first). The returned slice is safe to mutate.
func (fl *FlowLibrary) ListEntries() []*FlowLibraryEntry {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	out := make([]*FlowLibraryEntry, 0, len(fl.flows))
	for _, entry := range fl.flows {
		if entry == nil {
			continue
		}
		cp := *entry
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RegisteredAt.After(out[j].RegisteredAt)
	})
	return out
}

// Propose returns candidate CPNs for a client.
// Phase 1 stub: always returns nil (CON-003, REQ-011).
func (fl *FlowLibrary) Propose(_ string) []*CPN {
	return nil
}

// Len returns the number of registered flows.
func (fl *FlowLibrary) Len() int {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	return len(fl.flows)
}
