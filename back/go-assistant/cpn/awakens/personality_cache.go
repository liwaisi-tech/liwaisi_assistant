package awakens

import (
	"container/list"
	"context"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// DefaultPersonalityCacheCap is the LRU cap for MemoryPersonalityCache.
const DefaultPersonalityCacheCap = 64

// PersonalityCache is the prefix-cache contract used by SC-07 to avoid
// re-rendering the "## Environment awareness" block on every turn ≥ 2.
// Implementations are safe for concurrent use.
type PersonalityCache interface {
	Get(digest string) (block string, ok bool)
	Put(digest, block string)
	Invalidate(digest string)
}

// MemoryPersonalityCache is an LRU map-backed PersonalityCache. The digest
// itself is the key; stale digests are never queried again and naturally age
// out under LRU pressure (REQ-704).
type MemoryPersonalityCache struct {
	mu    sync.Mutex
	cap   int
	order *list.List
	items map[string]*list.Element
}

type personalityEntry struct {
	digest string
	block  string
}

// NewMemoryPersonalityCache returns a MemoryPersonalityCache with the given
// cap (≤ 0 selects DefaultPersonalityCacheCap).
func NewMemoryPersonalityCache(cap int) *MemoryPersonalityCache {
	if cap <= 0 {
		cap = DefaultPersonalityCacheCap
	}
	return &MemoryPersonalityCache{
		cap:   cap,
		order: list.New(),
		items: make(map[string]*list.Element, cap),
	}
}

// Get returns the cached block for digest and promotes it.
func (c *MemoryPersonalityCache) Get(digest string) (string, bool) {
	if digest == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[digest]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(el)
	return el.Value.(*personalityEntry).block, true
}

// Put inserts or refreshes the digest → block mapping and evicts the LRU
// tail when the cap is exceeded.
func (c *MemoryPersonalityCache) Put(digest, block string) {
	if digest == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[digest]; ok {
		el.Value.(*personalityEntry).block = block
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&personalityEntry{digest: digest, block: block})
	c.items[digest] = el
	for c.order.Len() > c.cap {
		tail := c.order.Back()
		if tail == nil {
			break
		}
		c.order.Remove(tail)
		delete(c.items, tail.Value.(*personalityEntry).digest)
	}
}

// Invalidate drops the entry for digest, if present.
func (c *MemoryPersonalityCache) Invalidate(digest string) {
	if digest == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[digest]; ok {
		c.order.Remove(el)
		delete(c.items, digest)
	}
}

// Len returns the current number of cached entries.
func (c *MemoryPersonalityCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// CatalogueCap is the default byte cap for the rendered catalogue block
// (CON-002 in spec-architecture-brae-awakening-toolbox-extension.md).
const CatalogueCap = 4096

// SnapshotRepo is the narrow read-only port BuildEnvironmentAwarenessFromSnapshot
// needs. *persist.HostCapabilityRepository satisfies it.
type SnapshotRepo interface {
	LatestForHost(ctx context.Context, hostID string) (persist.HostCapabilitySnapshot, error)
}

// BuildEnvironmentAwarenessFromSnapshot fetches the latest snapshot (REQ-701),
// renders the "## Environment awareness" block via BuildToolboxCatalogue
// (REQ-702), and prefix-caches it by PersonalityDigest (REQ-703).
//
// Returns ("", "", nil) when repo is nil, the snapshot is missing/empty, or
// any error occurs — callers degrade gracefully (do not fail the request).
func BuildEnvironmentAwarenessFromSnapshot(
	ctx context.Context,
	repo SnapshotRepo,
	hostID string,
	toolboxes []tools.ToolboxManifest,
	lexiconExcerpt []byte,
	cache PersonalityCache,
) (block string, digest string, err error) {
	if repo == nil || hostID == "" {
		return "", "", nil
	}
	snap, lerr := repo.LatestForHost(ctx, hostID)
	if lerr != nil {
		return "", "", nil
	}
	if snap.HostID == "" && snap.Identity.User == "" && snap.Kernel.OS == "" {
		return "", "", nil
	}

	if len(toolboxes) == 0 {
		legacy := EnvironmentAwarenessBlock(snap)
		if legacy == "" {
			return "", "", nil
		}
		d := PersonalityDigest(nil, []byte(legacy), nil, lexiconExcerpt)
		if cache != nil {
			if cached, ok := cache.Get(d); ok {
				return cached, d, nil
			}
			cache.Put(d, legacy)
		}
		return legacy, d, nil
	}

	osLine, shellLine, present, absent := SplitSnapshotForCatalogue(snap)
	preDigest := PersonalityDigest(
		renderCatalogueBytesForDigest(toolboxes),
		[]byte(osLine), []byte(shellLine), lexiconExcerpt,
	)
	if cache != nil {
		if cached, ok := cache.Get(preDigest); ok {
			return cached, preDigest, nil
		}
	}

	rendered, digestOut, _, berr := BuildToolboxCatalogue(
		osLine, shellLine, present, absent, toolboxes, lexiconExcerpt, CatalogueCap,
	)
	if berr != nil || rendered == "" {
		legacy := EnvironmentAwarenessBlock(snap)
		if legacy == "" {
			return "", "", nil
		}
		d := PersonalityDigest(nil, []byte(legacy), nil, lexiconExcerpt)
		if cache != nil {
			cache.Put(d, legacy)
		}
		return legacy, d, nil
	}

	if cache != nil {
		cache.Put(digestOut, rendered)
	}
	return rendered, digestOut, nil
}

// renderCatalogueBytesForDigest renders the `### Toolboxes` subsection
// bytes used as the first digest input. Keeps the cache-key computation
// aligned with BuildToolboxCatalogue's REQ-009 digest.
func renderCatalogueBytesForDigest(tb []tools.ToolboxManifest) []byte {
	cp := make([]tools.ToolboxManifest, len(tb))
	copy(cp, tb)
	sortToolboxesForRender(cp)
	return []byte(renderCatalogueBytes(cp))
}

// SplitSnapshotForCatalogue destructures a HostCapabilitySnapshot into the
// sentence-formatted inputs expected by BuildToolboxCatalogue.
func SplitSnapshotForCatalogue(snap persist.HostCapabilitySnapshot) (osLine, shellLine string, present, absent []string) {
	if snap.HostID == "" && snap.Identity.User == "" && snap.Kernel.OS == "" {
		return "", "", nil, nil
	}
	osName := snap.Kernel.OS
	if v, ok := snap.Kernel.OSRelease["NAME"]; ok && v != "" {
		osName = v
	}
	osLine = "OS: " + osName
	if ver := snap.Kernel.OSRelease["VERSION_ID"]; ver != "" {
		osLine += " " + ver
	}
	if snap.Kernel.Arch != "" {
		osLine += " (" + snap.Kernel.Arch + ")"
	}
	if snap.Kernel.Kernel != "" {
		osLine += ", kernel " + snap.Kernel.Kernel
	}
	osLine += "."
	if snap.Identity.Shell != "" {
		shellLine = "Shell: " + snap.Identity.Shell + "."
	}
	present, absent = splitBinaries(snap.Binaries)
	return osLine, shellLine, present, absent
}
