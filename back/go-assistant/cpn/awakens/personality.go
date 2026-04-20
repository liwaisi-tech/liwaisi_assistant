package awakens

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// EnvironmentAwarenessHeader is the fixed section header used in the
// personality pipeline (PAT-003). Downstream prompt assembly locates and
// prunes this section by substring when the token budget is tight.
const EnvironmentAwarenessHeader = "## Environment awareness"

// EnvironmentAwarenessBlock renders the snapshot as the "Environment
// awareness" block injected into turn ≥ 2 system prompts (REQ-007, AC-007).
// Returns "" when snap is empty / zero.
//
// The block enumerates OS, shell, present binaries and absent binaries
// deterministically (sorted) so prompt diffs stay legible in tests.
func EnvironmentAwarenessBlock(snap persist.HostCapabilitySnapshot) string {
	if snap.HostID == "" && snap.Identity.User == "" && snap.Kernel.OS == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(EnvironmentAwarenessHeader)
	b.WriteByte('\n')

	osName := snap.Kernel.OS
	if v, ok := snap.Kernel.OSRelease["NAME"]; ok && v != "" {
		osName = v
	}
	ver := snap.Kernel.OSRelease["VERSION_ID"]
	fmt.Fprintf(&b, "OS: %s", strings.TrimSpace(osName))
	if ver != "" {
		fmt.Fprintf(&b, " %s", ver)
	}
	if snap.Kernel.Arch != "" {
		fmt.Fprintf(&b, " (%s)", snap.Kernel.Arch)
	}
	if snap.Kernel.Kernel != "" {
		fmt.Fprintf(&b, ", kernel %s", snap.Kernel.Kernel)
	}
	b.WriteString(".\n")

	if snap.Identity.Shell != "" {
		fmt.Fprintf(&b, "Shell: %s.\n", snap.Identity.Shell)
	}

	present, absent := splitBinaries(snap.Binaries)
	if len(present) > 0 {
		fmt.Fprintf(&b, "Available: %s.\n", strings.Join(present, ", "))
	}
	if len(absent) > 0 {
		fmt.Fprintf(&b, "NOT available: %s.\n", strings.Join(absent, ", "))
	}
	b.WriteString("If the user asks you to use a missing tool, state it is absent and propose an alternative.\n")
	return b.String()
}

// splitBinaries separates the binary list into sorted present/absent slices.
func splitBinaries(bins []persist.BinaryProbe) (present, absent []string) {
	for _, bp := range bins {
		if bp.Present {
			present = append(present, bp.Name)
		} else {
			absent = append(absent, bp.Name)
		}
	}
	sort.Strings(present)
	sort.Strings(absent)
	return present, absent
}
