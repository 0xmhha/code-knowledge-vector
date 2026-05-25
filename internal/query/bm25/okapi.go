package bm25

import (
	"math"
	"sort"
)

// Default Okapi BM25 hyperparameters. K1 controls term-frequency
// saturation (higher = more weight to repeated matches); B controls
// length normalization (0 = ignore length, 1 = full normalization).
// 1.5 / 0.75 is the most widely cited starting point in the literature
// and matches both bleve and rank_bm25 defaults.
const (
	DefaultK1 = 1.5
	DefaultB  = 0.75
)

// Okapi is the standard BM25Okapi scorer. Concurrent reads (Score, TopK)
// after a completed Index are safe; do NOT call Index concurrently with
// a reader. Build once, query many.
//
// In CKV the corpus is built per-Search call from the vector-retrieved
// candidate set (~30 documents), so "concurrent reads" simply means the
// Engine.Search owner runs Score in a single goroutine; the parameters
// remain useful documentation for any future consumer.
type Okapi struct {
	K1 float64
	B  float64

	// Indexed corpus state. All maps are populated by Index and treated
	// as read-only afterwards.
	docIDs    []string
	docLen    map[string]int
	avgDocLen float64
	termDocs  map[string]int            // term → number of docs containing term
	docTerms  map[string]map[string]int // docID → term → tf
	n         int
}

// NewOkapi returns a scorer with default hyperparameters. Override K1/B
// directly on the returned struct before calling Index.
func NewOkapi() *Okapi {
	return &Okapi{K1: DefaultK1, B: DefaultB}
}

// Index registers the corpus. Empty input is allowed — the scorer will
// return 0 for all queries until a non-empty Index call replaces state.
func (o *Okapi) Index(docs []Document) {
	o.n = len(docs)
	o.docIDs = make([]string, 0, o.n)
	o.docLen = make(map[string]int, o.n)
	o.termDocs = make(map[string]int)
	o.docTerms = make(map[string]map[string]int, o.n)

	totalLen := 0
	for _, d := range docs {
		if d.ID == "" {
			continue
		}
		o.docIDs = append(o.docIDs, d.ID)
		o.docLen[d.ID] = len(d.Tokens)
		totalLen += len(d.Tokens)

		seen := make(map[string]struct{}, len(d.Tokens))
		terms := make(map[string]int, len(d.Tokens))
		for _, t := range d.Tokens {
			if t == "" {
				continue
			}
			terms[t]++
			if _, dup := seen[t]; !dup {
				seen[t] = struct{}{}
				o.termDocs[t]++
			}
		}
		o.docTerms[d.ID] = terms
	}
	if len(o.docIDs) > 0 {
		o.avgDocLen = float64(totalLen) / float64(len(o.docIDs))
	}
}

// idf returns the smoothing IDF for a query term. The +1 inside the log
// prevents negative scores when a term appears in more than half the
// corpus, matching modern Lucene/Elasticsearch behaviour.
func (o *Okapi) idf(term string) float64 {
	if o.n == 0 {
		return 0
	}
	df := float64(o.termDocs[term])
	N := float64(len(o.docIDs))
	return math.Log(1 + (N-df+0.5)/(df+0.5))
}

// Score returns the BM25 score for one document under the given query.
// Both K1 and B come from the Okapi struct so callers can tune at
// runtime without recreating the scorer.
func (o *Okapi) Score(query []string, docID string) float64 {
	terms, ok := o.docTerms[docID]
	if !ok {
		return 0
	}
	if o.avgDocLen == 0 {
		return 0
	}
	dl := float64(o.docLen[docID])
	score := 0.0
	for _, q := range query {
		if q == "" {
			continue
		}
		tf := float64(terms[q])
		if tf == 0 {
			continue
		}
		idf := o.idf(q)
		norm := 1 - o.B + o.B*dl/o.avgDocLen
		score += idf * (tf * (o.K1 + 1)) / (tf + o.K1*norm)
	}
	return score
}

// TopK returns the top-k documents by score, descending. Documents with
// score 0 (no matching terms) are excluded. k <= 0 means "all matches".
//
// CKV's Rerank uses Score directly rather than TopK because it needs
// every candidate's rank to feed RRF — including zero-scoring ones —
// not just the top-k. TopK is kept for parity with the CKG interface
// and for ad-hoc debugging.
func (o *Okapi) TopK(query []string, k int) []ScoredDoc {
	out := make([]ScoredDoc, 0, len(o.docIDs))
	for _, id := range o.docIDs {
		s := o.Score(query, id)
		if s > 0 {
			out = append(out, ScoredDoc{ID: id, Score: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		// Stable tiebreak on ID so identical-score results are deterministic.
		return out[i].ID < out[j].ID
	})
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
}

// Compile-time check that *Okapi satisfies Scorer.
var _ Scorer = (*Okapi)(nil)
