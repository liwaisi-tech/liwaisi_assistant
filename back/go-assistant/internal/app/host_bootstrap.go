package app

// host_bootstrap.go — tiny helpers split out so session_service.go stays
// focused on session lifecycle while the GAP-2 bootstrap path has its own
// slot. Keeps test overrides trivial.

import (
	"os"
)

// osReadFileHostID is a seam so tests can stub out /etc/machine-id.
var osReadFileHostID = os.ReadFile

// osHostname is a seam for tests.
var osHostname = os.Hostname

// osGetenvHostID is a seam so tests can inject the LIWAISI_HOST_ID override
// without touching real process env.
var osGetenvHostID = os.Getenv

// fallbackMachineIDHash mirrors the deterministic FNV-derived identifier
// used by the topology's own fallback so the bootstrap key and the
// post-discovery key agree when /etc/machine-id is unreadable.
func fallbackMachineIDHash(hostname string) string {
	if hostname == "" {
		hostname = "unknown-host"
	}
	h := uint64(1469598103934665603)
	for _, b := range []byte(hostname + "|fallback") {
		h ^= uint64(b)
		h *= 1099511628211
	}
	// Keep the format identical to topologies_host_discovery.go.
	return formatFallbackHash(h)
}

func formatFallbackHash(h uint64) string {
	const hex = "0123456789abcdef"
	out := []byte("fallback-0000000000000000")
	for i := 0; i < 16; i++ {
		out[len(out)-1-i] = hex[h&0xf]
		h >>= 4
	}
	return string(out)
}
