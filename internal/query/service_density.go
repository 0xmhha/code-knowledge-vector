package query

import (
	"context"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DensityService adjusts snippet length to fit within a token budget.
// Longer snippets are progressively compressed (full → signature+context
// → signature-only) until the total fits.
type DensityService struct{}

// Run adjusts density for combined hits (primary + examples) under the
// given token budget. Returns formatted Hit slice and total tokens used.
func (s *DensityService) Run(_ context.Context, hits []types.Hit, budget int, maxDensity DensityTier, sigContextLines int) ([]Hit, int) {
	return DensityAdjustWith(hits, budget, maxDensity, sigContextLines)
}

// RunContext reads sc.PrimaryHits + sc.ExampleHits, adjusts density,
// and writes sc.FinalHits + sc.FinalExamples + sc.TokensUsed.
func (s *DensityService) RunContext(ctx context.Context, sc *SearchContext) error {
	budget := sc.Options.BudgetTokens
	if budget == 0 {
		budget = DefaultBudgetTokens
	}

	combined := make([]types.Hit, 0, len(sc.PrimaryHits)+len(sc.ExampleHits))
	combined = append(combined, sc.PrimaryHits...)
	combined = append(combined, sc.ExampleHits...)

	adjusted, tokensUsed := s.Run(ctx, combined, budget, sc.Options.MaxDensity, sc.Options.SignatureContextLines)
	sc.TokensUsed = tokensUsed

	if len(adjusted) >= len(sc.PrimaryHits) {
		sc.FinalHits = adjusted[:len(sc.PrimaryHits)]
		sc.FinalExamples = adjusted[len(sc.PrimaryHits):]
	} else {
		sc.FinalHits = adjusted
	}
	return nil
}
