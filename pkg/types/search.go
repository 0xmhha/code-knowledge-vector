package types

import "slices"

// Filter narrows a vector search by metadata. All fields are optional; an
// empty field is treated as "any". Filters are AND-combined.
//
// Plan §6.3:
//   - Language: "go" | "typescript" | "solidity" | "markdown"
//   - PathGlob: filepath.Match-style glob (e.g. "cmd/**/*.go" support via
//     doublestar lands later; for W1/W2 plain filepath.Match suffices)
//   - SymbolKinds: e.g. {Function, Method}
//   - CommitHash: pin to a specific historical commit's chunks
type Filter struct {
	Language    string       `json:"language,omitempty"`
	PathGlob    string       `json:"path,omitempty"`
	SymbolKinds []SymbolKind `json:"symbol_kinds,omitempty"`
	CommitHash  string       `json:"commit_hash,omitempty"`
}

// IsZero reports whether the filter would match every chunk. Used by store
// implementations to skip the post-filter step entirely on the hot path.
func (f Filter) IsZero() bool {
	return f.Language == "" && f.PathGlob == "" && len(f.SymbolKinds) == 0 && f.CommitHash == ""
}

// Matches reports whether c satisfies every set field of f. Implemented
// here so both the store layer (post-filter) and the query layer (sanity
// check) share one definition.
//
// NOTE: PathGlob uses filepath.Match semantics (single-star, no "**").
// W3 may swap to doublestar; keeping this contract small for now.
func (f Filter) Matches(c Chunk) bool {
	if f.Language != "" && f.Language != c.Language {
		return false
	}
	if f.CommitHash != "" && f.CommitHash != c.CommitHash {
		return false
	}
	if f.PathGlob != "" {
		ok, err := matchPath(f.PathGlob, c.File)
		if err != nil || !ok {
			return false
		}
	}
	if len(f.SymbolKinds) > 0 && !slices.Contains(f.SymbolKinds, c.SymbolKind) {
		return false
	}
	return true
}

// Hit is a single search result. Score values are normalized so callers
// can compare across backends; raw distance is preserved for RRF input.
type Hit struct {
	Chunk Chunk    `json:"chunk"`
	Score HitScore `json:"score"`
	// StaleCitation is set by the citation-enforcement step when the
	// chunk's recorded commit_hash differs from the source tree's
	// current git HEAD. The hit is still returned — the file usually
	// still has useful content at a different commit — but downstream
	// consumers can warn the user or downgrade the snippet shape.
	StaleCitation bool `json:"stale_citation,omitempty"`
}

// HitScore exposes both the normalized score (higher = better, range [0,1])
// and the raw cosine distance (lower = better, range [0,2]). The RRF fuser
// upstream consumes Rank; lower-layer query callers display Normalized.
//
// NEW-9 / ADR-006 adds two omitempty fields for the optional BM25 rerank
// pass. They stay zero (and absent from JSON) when Options.EnableBM25Rerank
// is off, preserving the schema for callers that haven't opted in.
type HitScore struct {
	Normalized     float64 `json:"normalized"`      // 1 - distance/2, in [0,1]
	VectorDistance float64 `json:"vector_distance"` // raw cosine distance, in [0,2]
	VectorRank     int     `json:"vector_rank"`     // 1-based within this query's vector hits
	BM25Score      float64 `json:"bm25_score,omitempty"`  // NEW-9: candidate-set BM25, 0 when rerank disabled or no token match
	HybridRank     int     `json:"hybrid_rank,omitempty"` // NEW-9: 1-based position after RRF fusion; 0 when rerank disabled
}

// Stats reports index health. Returned by VectorStore.Stats and surfaced
// via the MCP `cks.ops.health` tool.
type Stats struct {
	ChunkCount      int    `json:"chunk_count"`
	EmbeddingModel  string `json:"embedding_model"`
	EmbeddingDim    int    `json:"embedding_dim"`
	IndexedHead     string `json:"indexed_head"`
	BuiltAt         string `json:"built_at"`
	SchemaVersion   string `json:"schema_version"`
}
