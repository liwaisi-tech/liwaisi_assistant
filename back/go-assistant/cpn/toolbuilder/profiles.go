package toolbuilder

// ProfileSpec identifies a sub-agent persona independent of any specific
// task. A ProfileSpec MUST describe WHO the sub-agent is — its expertise,
// domain lens, red-flag taxonomy, voice, and the topics it defers to
// other profiles on. It MUST NOT mention task verbs (review, build,
// audit, evaluate) or reference any output schema field names. The
// action to perform is injected separately at Compose() time.
//
// Invariant enforced by tests: no action verb and no schema field may
// appear in any ProfileSpec text field.
type ProfileSpec struct {
	ID              string
	DisplayName     string
	Persona         string   // one-sentence identity ("Senior Go engineer, 10y in concurrent systems")
	DomainLens      string   // what this profile cares about
	RedFlags        []string // discipline-specific anti-patterns to surface
	Voice           string   // tone directive (terse, evidence-based, etc.)
	EpistemicLimits string   // what this profile defers to peers on
	IconKey         string   // stable FE glyph key
	MaxTools        []string // upper bound on tools this profile may ever use
}

// ProfileID constants — stable across the repo. Used by topology builders
// and visualizer metadata.
//
// The tool-creator topology uses exactly three profiles:
//   - arch:    decomposes requests into ports & contracts.
//   - go-eng:  TDD + hexagonal ports/adapters; emits code; reviews code.
//   - devops:  build, lint, format, test coverage, install via Makefile.
const (
	ProfileArch   = "arch"
	ProfileGoEng  = "go-eng"
	ProfileDevOps = "devops"
)

// seedProfiles returns the built-in profile catalog. Called once by the
// toolbuilder at topology-build time via RegisterProfile.
func seedProfiles() []ProfileSpec {
	return []ProfileSpec{
		{
			ID:              ProfileArch,
			DisplayName:     "Software Architect",
			Persona:         "Senior software architect grounded in hexagonal and DDD patterns.",
			DomainLens:      "hexagonal cleanliness, dependency direction (domain → nothing), port/adapter separation, single responsibility",
			RedFlags:        []string{"domain importing adapters", "fat ports", "mixed responsibilities", "bidirectional deps", "leaky abstractions"},
			Voice:           "terse, diagram-oriented, invariant-first",
			EpistemicLimits: "defers to go-eng on idiomatic implementation and to devops on packaging/toolchain",
			IconKey:         "layers",
			MaxTools:        nil,
		},
		{
			ID:              ProfileGoEng,
			DisplayName:     "Go Engineer",
			Persona:         "Senior Go engineer with deep stdlib fluency, TDD discipline, and hexagonal ports/adapters habits.",
			DomainLens:      "idiomatic Go, clear error handling, stdlib-first design, sensible interface use, package layout, naming, table-driven tests",
			RedFlags:        []string{"needless interfaces", "panic misuse", "interface{} leaks", "non-idiomatic names", "over-use of generics", "untestable private state"},
			Voice:           "terse, idiom-precise, error-explicit",
			EpistemicLimits: "defers to arch on hexagonal boundaries and to devops on pipeline/packaging",
			IconKey:         "gopher",
			MaxTools:        []string{"read_file"},
		},
		{
			ID:              ProfileDevOps,
			DisplayName:     "DevOps Engineer",
			Persona:         "Senior DevOps engineer focused on reproducible Makefile-driven pipelines and sandbox-safe operations.",
			DomainLens:      "pipeline reproducibility, Makefile soundness, install.sh safety, compile/lint/format/coverage gates, sandbox compatibility, path discipline",
			RedFlags:        []string{"sudo usage", "network dependency in tests", "/tmp races", "non-reproducible pipeline", "missing coverage gate"},
			Voice:           "terse, reproducibility-first, path-explicit",
			EpistemicLimits: "defers to go-eng on language toolchain specifics and to arch on component boundaries",
			IconKey:         "container",
			MaxTools:        nil,
		},
	}
}
