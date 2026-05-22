package eval

import (
	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// PerQuery is the scoring result for one fixture entry.
type PerQuery struct {
	QueryID         string  `json:"query_id"`
	Intent          string  `json:"intent"`
	FoundRank       int     `json:"found_rank"`         // 1-based rank of first correct hit; 0 if absent
	HitsReturned    int     `json:"hits_returned"`
	TopHitFile      string  `json:"top_hit_file"`
	TopHitScore     float64 `json:"top_hit_score"`
	ReciprocalRank  float64 `json:"reciprocal_rank"`    // 1/found_rank, or 0
	CitationCorrect bool    `json:"citation_correct"`   // hits-with-matching-file have valid file+line in expected range
	// Hallucination metrics. Populated only when SrcRoot is set on the
	// eval Options — without a source tree we can't verify a hit's
	// snippet against actual file content. HallucinationCount is the
	// number of returned hits whose snippet did not survive VerifyHit
	// (file_missing / out_of_range / snippet_not_found).
	HallucinationCount int    `json:"hallucination_count,omitempty"`
	HallucinationReason string `json:"hallucination_reason,omitempty"` // first non-empty reason across hits, for triage
}

// Aggregate is the corpus-level summary across all queries.
type Aggregate struct {
	Total            int     `json:"total"`
	Found            int     `json:"found"` // queries with ≥1 correct hit in top-K
	RecallAt1        float64 `json:"recall_at_1"`
	RecallAt3        float64 `json:"recall_at_3"`
	RecallAt5        float64 `json:"recall_at_5"`
	MRR              float64 `json:"mrr"`
	CitationAccuracy float64 `json:"citation_accuracy"` // mean(citation_correct over queries that found a hit)
	// HallucinationRate is the fraction of returned hits across all
	// queries whose snippet did not align with the source file. 0
	// means perfect — every snippet appears at the cited location.
	// Populated only when Options.SrcRoot is set.
	HallucinationRate float64 `json:"hallucination_rate,omitempty"`
	HallucinationHits int     `json:"hallucination_hits,omitempty"` // numerator
	TotalHits         int     `json:"total_hits,omitempty"`         // denominator (returned hits, not queries)
}

// Score compares one query's response against its expected target.
// k is the effective top-K used for recall counting. When srcRoot is
// non-empty, every returned hit is also verified against the source
// tree (Phase 3 / D5) and the per-query hallucination_count is
// populated. Empty srcRoot leaves hallucination fields zero.
func Score(q Query, resp *query.Response, k int, srcRoot string) PerQuery {
	out := PerQuery{
		QueryID:      q.ID,
		Intent:       q.Intent,
		HitsReturned: len(resp.Hits),
	}
	if len(resp.Hits) > 0 {
		out.TopHitFile = resp.Hits[0].Citation.File
		out.TopHitScore = resp.Hits[0].Score.Normalized
	}
	limit := k
	if limit > len(resp.Hits) {
		limit = len(resp.Hits)
	}
	for i := 0; i < limit; i++ {
		h := resp.Hits[i]
		if hitMatches(h, q.Expected) {
			out.FoundRank = i + 1
			out.ReciprocalRank = 1.0 / float64(out.FoundRank)
			out.CitationCorrect = true
			break
		}
	}
	if srcRoot != "" {
		verdicts, halluc := query.VerifyResponse(resp, srcRoot)
		out.HallucinationCount = halluc
		// First non-empty reason gives operators a single triage hint
		// without scrolling through every verdict.
		for _, v := range verdicts {
			if !v.Verified && v.Reason != "" {
				out.HallucinationReason = v.Reason
				break
			}
		}
	}
	return out
}

// hitMatches reports whether hit.citation references the expected
// file and the line range overlaps expected.LineRange.
func hitMatches(h query.Hit, exp Expected) bool {
	if h.Citation.File != exp.File {
		return false
	}
	return rangesOverlap(h.Citation.StartLine, h.Citation.EndLine, exp.LineRange[0], exp.LineRange[1])
}

func rangesOverlap(a1, a2, b1, b2 int) bool {
	return a1 <= b2 && b1 <= a2
}

// Summarize computes corpus-level metrics from per-query scores. k is
// the K used at query time; recall@1/3/5 are derived from FoundRank.
// Hallucination metrics are populated only when per-query
// HallucinationCount values are present (Score was called with a
// non-empty srcRoot).
func Summarize(perQ []PerQuery) Aggregate {
	a := Aggregate{Total: len(perQ)}
	if a.Total == 0 {
		return a
	}
	var sumRR float64
	var foundWithCitation int
	var anyHallucData bool
	for _, p := range perQ {
		if p.FoundRank > 0 {
			a.Found++
			sumRR += p.ReciprocalRank
			if p.CitationCorrect {
				foundWithCitation++
			}
			if p.FoundRank <= 1 {
				a.RecallAt1++
			}
			if p.FoundRank <= 3 {
				a.RecallAt3++
			}
			if p.FoundRank <= 5 {
				a.RecallAt5++
			}
		}
		a.TotalHits += p.HitsReturned
		if p.HitsReturned > 0 {
			anyHallucData = anyHallucData || true
		}
		a.HallucinationHits += p.HallucinationCount
		// HallucinationCount > 0 always implies hallucination data
		// was collected; non-zero HitsReturned alone doesn't (Score
		// may have skipped verify when srcRoot was empty). The
		// HallucinationReason field is the authoritative signal.
		if p.HallucinationReason != "" {
			anyHallucData = true
		}
	}
	total := float64(a.Total)
	a.RecallAt1 /= total
	a.RecallAt3 /= total
	a.RecallAt5 /= total
	a.MRR = sumRR / total
	if a.Found > 0 {
		a.CitationAccuracy = float64(foundWithCitation) / float64(a.Found)
	}
	if anyHallucData && a.TotalHits > 0 {
		a.HallucinationRate = float64(a.HallucinationHits) / float64(a.TotalHits)
	} else {
		// Reset hits/halluc to 0 so JSON omitempty hides them when no
		// verification ran. Operators reading 0/0 would misread it as
		// "perfect" — instead omit entirely.
		a.TotalHits = 0
		a.HallucinationHits = 0
	}
	return a
}
