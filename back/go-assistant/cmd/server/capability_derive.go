package main

// capability_derive.go — pure-Go translation from raw probe results to the
// derived atomic capabilities listed in REQ-040.
//
// Spec: spec/spec-architecture-host-discovery-capability-registry.md §3.
//
// Keeping this out of the topology factory keeps it trivially unit-testable
// (no CPN, no HostAdapter, no Postgres dependency).

import (
	"strconv"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// minGoVersion is the floor enforced by can-compile-go (REQ-040).
const minGoVersion = "1.22"

// DeriveCapabilities evaluates the REQ-040 rules against the probe results
// and returns the canonical set of derived capabilities, sorted by name.
//
// The caller (t-derive) passes through identity + kernel for forward
// compatibility with future rules (e.g. distro-specific capabilities) — the
// v1 rule set ignores them but the signature is stable.
//
// reads like the spec row it implements.
//
//nolint:gocognit // the rule table is intentionally flat so each capability
func DeriveCapabilities(_ persist.HostIdentity, _ persist.HostKernel, binaries []persist.BinaryProbe) []persist.Capability {
	// Index by name for O(1) lookup. Probes that never ran (missing entries
	// from the probe registry) are treated as "not present".
	byName := make(map[string]persist.BinaryProbe, len(binaries))
	for _, b := range binaries {
		byName[b.Name] = b
	}
	present := func(names ...string) ([]string, bool) {
		var hit []string
		for _, n := range names {
			if b, ok := byName[n]; ok && b.Present {
				hit = append(hit, n)
			}
		}
		return hit, len(hit) > 0
	}

	var caps []persist.Capability

	// can-compile-c ← gcc ∨ cc ∨ clang
	derived, sat := present("gcc", "cc", "clang")
	caps = append(caps, persist.Capability{
		Name:        "can-compile-c",
		Satisfied:   sat,
		DerivedFrom: orSingleton(derived, "gcc", "cc", "clang"),
	})

	// can-compile-go ← go present AND version ≥ 1.22
	goProbe, hasGo := byName["go"]
	canGo := hasGo && goProbe.Present && goVersionAtLeast(goProbe.Version, minGoVersion)
	caps = append(caps, persist.Capability{
		Name:        "can-compile-go",
		Satisfied:   canGo,
		DerivedFrom: []string{"go"},
	})

	// can-run-python ← python3
	_, canPy := present("python3")
	caps = append(caps, persist.Capability{
		Name:        "can-run-python",
		Satisfied:   canPy,
		DerivedFrom: []string{"python3"},
	})

	// can-run-node ← node
	_, canNode := present("node")
	caps = append(caps, persist.Capability{
		Name:        "can-run-node",
		Satisfied:   canNode,
		DerivedFrom: []string{"node"},
	})

	// can-fetch-url ← curl ∨ wget
	derived, sat = present("curl", "wget")
	caps = append(caps, persist.Capability{
		Name:        "can-fetch-url",
		Satisfied:   sat,
		DerivedFrom: orSingleton(derived, "curl", "wget"),
	})

	// can-sandbox ← bwrap ∨ firejail
	derived, sat = present("bwrap", "firejail")
	caps = append(caps, persist.Capability{
		Name:        "can-sandbox",
		Satisfied:   sat,
		DerivedFrom: orSingleton(derived, "bwrap", "firejail"),
	})

	// can-version-control ← git
	_, canGit := present("git")
	caps = append(caps, persist.Capability{
		Name:        "can-version-control",
		Satisfied:   canGit,
		DerivedFrom: []string{"git"},
	})

	// can-pack ← tar
	_, canTar := present("tar")
	caps = append(caps, persist.Capability{
		Name:        "can-pack",
		Satisfied:   canTar,
		DerivedFrom: []string{"tar"},
	})

	return caps
}

// orSingleton returns `hits` when non-empty, else `fallback...` so the
// DerivedFrom field always lists the probes that were CONSIDERED, not only
// those that fired. Tests pin on this invariant.
func orSingleton(hits []string, fallback ...string) []string {
	if len(hits) > 0 {
		return hits
	}
	return fallback
}

// goVersionAtLeast reports whether the parsed `go version` semver is >= floor
// (e.g. "1.22"). Best-effort: unparseable versions return false.
//
// Inputs are of the form "1.22.3", "1.22", "1.22rc1" — we split on "." and
// compare the first two components numerically. The patch level is ignored
// because the floor is expressed as major.minor.
func goVersionAtLeast(got, floor string) bool {
	gmaj, gmin, ok := splitMajorMinor(got)
	if !ok {
		return false
	}
	fmaj, fmin, ok := splitMajorMinor(floor)
	if !ok {
		return false
	}
	if gmaj != fmaj {
		return gmaj > fmaj
	}
	return gmin >= fmin
}

func splitMajorMinor(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	// Trim pre-release tags from the minor (e.g. "22rc1").
	minStr := parts[1]
	for i, ch := range minStr {
		if ch < '0' || ch > '9' {
			minStr = minStr[:i]
			break
		}
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minVer, err := strconv.Atoi(minStr)
	if err != nil {
		return 0, 0, false
	}
	return maj, minVer, true
}
