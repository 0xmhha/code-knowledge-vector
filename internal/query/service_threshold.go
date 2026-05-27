package query

import (
	"context"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ThresholdService drops hits below the minimum normalized score.
type ThresholdService struct{}

// Run filters hits below the threshold. Returns only passing hits.
func (s *ThresholdService) Run(_ context.Context, hits []types.Hit, threshold float64) []types.Hit {
	if threshold <= 0 {
		return hits
	}
	var passed []types.Hit
	for _, h := range hits {
		if h.Score.Normalized >= threshold {
			passed = append(passed, h)
		}
	}
	return passed
}

// RunContext applies threshold to sc.RawHits → sc.FilteredHits.
func (s *ThresholdService) RunContext(ctx context.Context, sc *SearchContext) error {
	threshold := sc.Options.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	before := len(sc.RawHits)
	sc.FilteredHits = s.Run(ctx, sc.RawHits, threshold)
	if before > 0 && len(sc.FilteredHits) == 0 {
		sc.Warnings = append(sc.Warnings, "all_results_below_threshold")
	}
	return nil
}
