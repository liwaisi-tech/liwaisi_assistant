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

// WorkspaceLayoutHeader is the sub-section that tells the agent how its
// $HOME is organised. The folders are created at image build time by the
// Dockerfile and mirror infra/host/brae/LAYOUT.md — keep the two in sync.
const WorkspaceLayoutHeader = "## Workspace layout"

// WorkspaceLayoutBlock is a compact, token-efficient description of brae's
// opinionated $HOME layout. It's a compile-time constant because the layout
// is baked into the image — no runtime discovery is needed, so injecting it
// costs only the tokens on the wire and zero probe / persistence overhead.
const WorkspaceLayoutBlock = WorkspaceLayoutHeader + `
Your $HOME (/home/brae) is organised. Write each file to the matching folder:
- ~/workspace  — all coding work. One subfolder per project.
- ~/bin        — compiled executables you produce (on $PATH).
- ~/Documents  — long-form text, specs, markdown, pdfs.
- ~/Notes      — short scratch notes kept across turns.
- ~/Music      — audio (.wav .mp3 .flac .ogg).
- ~/Images     — images (.png .jpg .svg .webp).
- ~/Videos     — video (.mp4 .webm .mkv).
- ~/Downloads  — files fetched from the network; classify and move later.
- ~/.brae/tmp  — single-turn scratch space. Never use /tmp or $HOME root.
Env vars $BRAE_WORKSPACE, $BRAE_BIN, $BRAE_MUSIC, $BRAE_IMAGES, $BRAE_VIDEOS,
$BRAE_DOCUMENTS, $BRAE_NOTES, $BRAE_DOWNLOADS, $BRAE_TMP point at each folder.
Full reference: ~/LAYOUT.md (machine-readable: ~/.brae/layout.json).
`

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
	b.WriteByte('\n')
	b.WriteString(WorkspaceLayoutBlock)
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
