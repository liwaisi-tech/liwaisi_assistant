package cpn

// Lexicon is the hexagonal port exposing the controlled hashtag vocabulary to
// the cpn core and its consumers. The implementation lives in cpn/tools and is
// injected at server bootstrap (spec-architecture-brae-toolbox-taxonomy.md
// REQ-006, CON-005).
type Lexicon interface {
	// IsKnown reports whether tag is present in the lexicon. Tag MUST be
	// pre-normalised per REQ-NORM-001.
	IsKnown(tag string) bool
	// Kind returns the classification ("kind" | "domain") of tag, or ok=false
	// when tag is absent.
	Kind(tag string) (kind string, ok bool)
	// Describe returns the one-line description for tag, or ok=false when
	// absent.
	Describe(tag string) (desc string, ok bool)
	// All returns a snapshot of every entry in stable order.
	All() []LexiconEntry
}

// LexiconEntry is one controlled-vocabulary record.
type LexiconEntry struct {
	Tag         string `yaml:"tag" json:"tag"`
	Kind        string `yaml:"kind" json:"kind"`
	Description string `yaml:"description" json:"description"`
}
