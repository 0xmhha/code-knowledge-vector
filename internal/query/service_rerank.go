package query

import (
	"context"

	"github.com/0xmhha/code-knowledge-vector/internal/query/bm25"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// RerankService reorders candidate hits using BM25 + RRF fusion.
// Operates on the candidate set only (no global corpus), so cost is
// proportional to the number of candidates, not the index size.
type RerankService struct{}

// Run reranks candidates by BM25 + RRF fusion against the given intent.
// Returns reordered hits and statistics.
func (s *RerankService) Run(_ context.Context, candidates []types.Hit, intent string) ([]types.Hit, bm25.Stats) {
	if len(candidates) == 0 {
		return candidates, bm25.Stats{}
	}
	cands := make([]bm25.Candidate, len(candidates))
	for i, h := range candidates {
		cands[i] = bm25.Candidate{Hit: h, Corpus: bm25.BuildCorpusText(h)}
	}
	results, stats := bm25.Rerank(cands, intent)
	reordered := make([]types.Hit, len(results))
	for i, r := range results {
		reordered[i] = r.Hit
	}
	return reordered, stats
}

// RunContext reranks sc.RawHits if EnableBM25Rerank is set.
func (s *RerankService) RunContext(ctx context.Context, sc *SearchContext) error {
	if !sc.Options.EnableBM25Rerank || len(sc.RawHits) == 0 {
		return nil
	}
	reordered, _ := s.Run(ctx, sc.RawHits, sc.Intent)
	sc.RawHits = reordered
	return nil
}
