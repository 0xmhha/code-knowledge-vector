package eval

import (
	"context"
	"fmt"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// DefaultK is the top-K used by Run when Options.K is zero.
const DefaultK = 5

// Options control one eval pass.
type Options struct {
	K         int     // top-K for recall counting (default 5)
	Threshold float64 // pass-through to query.Options.Threshold; <0 disables
	SrcRoot   string  // pass-through for citation enforcement; empty → manifest default
	// EnableBM25Rerank toggles BM25 candidate-rerank on the eval pass.
	// Defaults false so existing baselines are preserved by default.
	// Both A and B legs of an A/B comparison use the same fixture +
	// engine, only this flag differs.
	EnableBM25Rerank bool
}

// Result is the full eval pass output.
type Result struct {
	Fixture   string     `json:"fixture"`
	K         int        `json:"k"`
	Aggregate Aggregate  `json:"aggregate"`
	PerQuery  []PerQuery `json:"per_query"`
}

// Run executes every query in fx against eng and returns a Result.
// Errors during a single query are folded into PerQuery (FoundRank=0,
// HitsReturned=0) so one bad fixture doesn't abort the whole pass.
func Run(ctx context.Context, eng *query.Engine, fx *Fixture, opts Options) (*Result, error) {
	if eng == nil {
		return nil, fmt.Errorf("eval: nil engine")
	}
	if fx == nil {
		return nil, fmt.Errorf("eval: nil fixture")
	}
	k := opts.K
	if k <= 0 {
		k = DefaultK
	}

	out := &Result{K: k}
	out.PerQuery = make([]PerQuery, 0, len(fx.Queries))

	for _, q := range fx.Queries {
		resp, err := eng.Search(ctx, q.Intent, query.Options{
			K:                k,
			Threshold:        opts.Threshold,
			SrcRoot:          opts.SrcRoot,
			EnableBM25Rerank: opts.EnableBM25Rerank,
		})
		if err != nil {
			// Treat a per-query failure as a miss; preserve the
			// query id so the report still lists it.
			out.PerQuery = append(out.PerQuery, PerQuery{
				QueryID: q.ID,
				Intent:  q.Intent,
			})
			continue
		}
		out.PerQuery = append(out.PerQuery, Score(q, resp, k, opts.SrcRoot))
	}
	out.Aggregate = Summarize(out.PerQuery)
	return out, nil
}
