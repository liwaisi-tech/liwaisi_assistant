package prompts

import (
	"embed"
	"fmt"
	"strings"
)

// roleFiles holds the per-role prompt bodies. Files live under
// roles/<role>.md and are embedded at compile time so the binary has no
// runtime FS dependency. New roles are added by dropping a markdown file
// into the directory and registering it in roleSources.
//
// See spec-architecture-brae-context-and-tool-hygiene.md REQ-012.
//
//go:embed roles/*.md
var roleFiles embed.FS

// Role is the canonical key used to look up a per-transition prompt.
// It mirrors cpn.TransitionRole to keep the call site simple, but the
// prompts package does not import cpn (cycle avoidance) — callers pass
// the string value.
type Role string

const (
	RoleAssistant  Role = "assistant"
	RoleClassify   Role = "classify"
	RoleValidate   Role = "validate"
	RoleSynthesize Role = "synthesize"
	RoleArchitect  Role = "architect"
	RolePlan       Role = "plan"
)

// roleSources maps a Role to the embedded filename. Adding a role
// requires (a) a new markdown file under roles/, (b) a constant above,
// (c) an entry here. The For() function returns the assistant fallback
// for any unknown role so adding callers is never a breaking change.
var roleSources = map[Role]string{
	RoleAssistant:  "roles/assistant.md",
	RoleClassify:   "roles/classify.md",
	RoleValidate:   "roles/validate.md",
	RoleSynthesize: "roles/synthesize.md",
	RoleArchitect:  "roles/architect.md",
	RolePlan:       "roles/plan.md",
}

// For returns the rendered system-prompt body for role. Panics at startup
// (via init) rather than at runtime if any registered file is missing,
// so a typo'd embed path fails CI rather than the first user request.
func For(role Role) string {
	if body, ok := roleCache[role]; ok {
		return body
	}
	return roleCache[RoleAssistant]
}

// roleCache is populated once in init from roleSources so the hot path
// is a map lookup with no FS read.
var roleCache map[Role]string

func init() {
	roleCache = make(map[Role]string, len(roleSources))
	for role, path := range roleSources {
		body, err := roleFiles.ReadFile(path)
		if err != nil {
			// embed.FS.ReadFile only fails for missing files. Failing
			// loud at init time surfaces typos in roleSources during
			// CI rather than at the first user request.
			panic(fmt.Sprintf("prompts: registered role %q points at missing file %q: %v",
				role, path, err))
		}
		roleCache[role] = strings.TrimRight(string(body), "\n") + "\n"
	}
}
