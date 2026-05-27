package query

import (
	"context"
	"fmt"

	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// StoreSearchService runs approximate nearest neighbor search against
// the vector store. Over-fetches by overfetchFactor to give downstream
// reranking and filtering enough candidates.
type StoreSearchService struct {
	store *sqlitevec.Store
}

// Run performs vector search and returns raw candidate hits.
func (s *StoreSearchService) Run(ctx context.Context, queryVec []float32, k int, filter types.Filter) ([]types.Hit, error) {
	overfetch := k * overfetchFactor
	hits, err := s.store.Search(ctx, queryVec, overfetch, filter)
	if err != nil {
		return nil, fmt.Errorf("store search: %w", err)
	}
	return hits, nil
}

// RunContext populates sc.RawHits from sc.QueryVec.
func (s *StoreSearchService) RunContext(ctx context.Context, sc *SearchContext) error {
	k := sc.Options.K
	if k <= 0 {
		k = DefaultK
	}
	hits, err := s.Run(ctx, sc.QueryVec, k, sc.Options.Filter)
	if err != nil {
		return err
	}
	sc.RawHits = hits
	return nil
}
