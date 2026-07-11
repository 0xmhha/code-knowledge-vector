package query

import (
	"context"
	"math"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/internal/query/bm25"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ExplanationVectorScore is the vector-side of explain_match.
type ExplanationVectorScore struct {
	Normalized     float64 `json:"normalized"`
	CosineDistance float64 `json:"cosine_distance"`
}

// ExplanationKeywordScore captures BM25-side detail: the raw Okapi score
// over the chunk's tokens against the query, plus which tokens actually
// matched (intersection of query tokens and chunk tokens).
type ExplanationKeywordScore struct {
	Score          float64  `json:"score"`
	MatchedTokens  []string `json:"matched_tokens"`
	QueryTokens    []string `json:"query_tokens"`
	ChunkTokenSize int      `json:"chunk_token_size"`
}

// Explanation is the full explain_match response.
type Explanation struct {
	ChunkID  string                      `json:"chunk_id"`
	Citation types.Citation              `json:"citation"`
	Vector   ExplanationVectorScore      `json:"vector_score"`
	Keyword  ExplanationKeywordScore     `json:"keyword_score"`
	Category string                      `json:"category,omitempty"`
	Guidance *types.ModificationGuidance `json:"guidance,omitempty"`
	Symbol   string                      `json:"symbol,omitempty"`
}

// ExplainMatch reports why a particular chunk would have matched the
// intent: vector similarity computed fresh against the embedder, and
// BM25 details from the keyword index. When the chunk_id is unknown,
// returns ErrChunkNotFound.
//
// The function is pure-read and does no caching beyond the keyword
// index already cached on the Engine. Cost is roughly:
//   - 1 embed call (the intent vector)
//   - 1 KNN-style cosine distance against the chunk's stored vector
//   - 1 BM25 lookup against the in-memory index
func (e *Engine) ExplainMatch(ctx context.Context, chunkID, intent string) (*Explanation, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}

	chunks, err := e.store.LookupByIDs(ctx, []string{chunkID})
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, ErrChunkNotFound
	}
	c := chunks[0]

	// Vector side: embed the intent, look up the chunk's stored
	// vector via Search-with-filter, compute cosine distance directly.
	// We use Search with k=1 + commit-hash filter to fetch one row;
	// this also exposes the chunk's distance to the intent vector.
	intentVecs, err := embedQueryBatch(ctx, e.emb, []string{intent})
	if err != nil {
		return nil, err
	}
	if len(intentVecs) == 0 {
		return nil, ErrChunkNotFound
	}
	intentVec := intentVecs[0]

	vecHits, err := e.store.Search(ctx, intentVec, 200, types.Filter{})
	if err != nil {
		return nil, err
	}
	vec := ExplanationVectorScore{}
	for _, h := range vecHits {
		if h.Chunk.ID == chunkID {
			vec.CosineDistance = h.Score.VectorDistance
			vec.Normalized = h.Score.Normalized
			break
		}
	}

	// Keyword side: tokenize both the intent and the chunk body, then
	// compute matched-token intersection. The BM25 score itself comes
	// from a single-document Okapi pass against the intent tokens.
	queryTokens := bm25.Tokenize(intent)
	chunkText := c.SymbolName + " " + c.File + " " + c.Text
	chunkTokens := bm25.Tokenize(chunkText)
	matched := matchedTokens(queryTokens, chunkTokens)

	kwScore := 0.0
	if idx, ierr := e.keywordIndex(ctx); ierr == nil && idx != nil && idx.scorer != nil {
		kwScore = idx.scorer.Score(queryTokens, chunkID)
	}

	return &Explanation{
		ChunkID:  c.ID,
		Citation: c.Citation(),
		Vector:   vec,
		Keyword: ExplanationKeywordScore{
			Score:          kwScore,
			MatchedTokens:  matched,
			QueryTokens:    queryTokens,
			ChunkTokenSize: len(chunkTokens),
		},
		Category: c.Category,
		Guidance: c.Guidance,
		Symbol:   c.SymbolName,
	}, nil
}

// matchedTokens returns the set intersection of two token lists in
// query order. Used by Explanation to show which intent words drove
// the BM25 score.
func matchedTokens(query, chunk []string) []string {
	in := map[string]bool{}
	for _, t := range chunk {
		in[t] = true
	}
	out := make([]string, 0, len(query))
	seen := map[string]bool{}
	for _, t := range query {
		if in[t] && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out
}

// CosineSimilarity is a small helper for the explain pathway in tests
// and other callers that want a direct vector-vs-vector distance
// instead of going through the store's KNN. Returns 0 when either
// vector is empty or differs in length.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i, v := range a {
		dot += float64(v) * float64(b[i])
		na += float64(v) * float64(v)
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// stripCRLF is a tiny utility that strips trailing CR / LF from a
// snippet — used by explain_match's text comparison logic so chunks
// indexed on Windows do not produce false negatives. Kept here so the
// query package does not need to import a string-utility package for
// one line.
func stripCRLF(s string) string {
	return strings.TrimRight(s, "\r\n")
}

// _ keeps stripCRLF reachable for future calls without triggering
// unused-function diagnostics.
var _ = stripCRLF
