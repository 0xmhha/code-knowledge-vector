// Package bm25 provides candidate-set BM25 rerank for CKV's query path.
//
// The Okapi implementation + code-aware tokenizer are adapted from the
// sibling repo github.com/0xmhha/code-knowledge-graph at pkg/bm25 (2026-05-26
// snapshot). Per that package's header note, the algorithm is hand-written
// from the BM25 description (Robertson, Walker 1994) and is not derivative
// of any third-party library; we copy rather than depend across repos so the
// CKV build does not require a CKG checkout.
//
// CKV-specific surface (this package, not in CKG):
//   - rerank.go    candidate-set Rerank + RRF fusion (k=60 default)
//
// ADR-006 (Proposed) governs the use of this package. CKV remains
// dense-only at the schema layer (ADR-003); this is a temporary
// candidate-rerank overlay measured for impact before the supersede
// decision lands.
package bm25

// Document is one indexable record. Tokens are pre-tokenized — call
// Tokenize for the package's standard code-aware splitter, or supply
// custom tokens for a domain-specific corpus.
type Document struct {
	ID     string
	Tokens []string
}

// ScoredDoc pairs a document ID with its BM25 score for the most recent
// query. Score is always >= 0; documents with no matching terms are not
// returned by TopK.
type ScoredDoc struct {
	ID    string
	Score float64
}

// Scorer is the contract every BM25 implementation in this package
// satisfies. The two-phase pattern (Index then Score / TopK) lets callers
// build the corpus once per query and reuse for many lookups.
type Scorer interface {
	// Index registers the full corpus. Repeated calls overwrite earlier
	// state — Scorer is not append-only.
	Index(docs []Document)
	// Score returns the BM25 score for one document under one query.
	// Returns 0 when docID is unknown or no query term matches.
	Score(query []string, docID string) float64
	// TopK returns the top-k documents by score, descending. k <= 0
	// returns every matching document.
	TopK(query []string, k int) []ScoredDoc
}
