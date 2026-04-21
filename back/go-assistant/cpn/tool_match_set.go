package cpn

// ToolMatch is one ranked retrieval match for a given intent. The real
// producer is spec-architecture-brae-tool-retriever (not yet implemented);
// this type is introduced in the JIT builder chunk so Compose has a stable
// input shape. Retriever-owned fields may be added without touching JIT
// callers.
type ToolMatch struct {
	// QualifiedName is the canonical "namespace/name@version" tool id.
	QualifiedName string
	// Score is the BM25 / hybrid retrieval score; higher is better.
	Score float64
	// Hashtags is the matched subset of the tool's hashtags.
	Hashtags []string
}

// ToolMatchSet is the ordered collection of retriever results. The Digest
// is a sha256 hex string computed over the canonical (qualified_name,
// hashtag) tuple list; callers that mutate Matches MUST recompute Digest.
type ToolMatchSet struct {
	Matches []ToolMatch
	Digest  string
}
