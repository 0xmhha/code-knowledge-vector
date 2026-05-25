package bm25

import (
	"fmt"
	"sort"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultRRFK is the canonical RRF k constant (Cormack, Clarke, Buettcher
// 2009 — "Reciprocal Rank Fusion outperforms Condorcet and individual
// rank learning methods"). 60 is the value the original paper proposed
// and the value Elasticsearch / OpenSearch default to.
const DefaultRRFK = 60

// Candidate pairs a vector-retrieved Hit with the BM25 corpus text for
// that candidate. CKV's caller (engine.Search) builds Corpus per D3-B:
// chunk.SymbolName + signature first line. Whole chunk.Text is not used
// because tokenizing 30 long bodies dominates the rerank latency and
// the noise overwhelms the symbol-level signal.
type Candidate struct {
	Hit    types.Hit
	Corpus string
}

// Result is one row in the reordered output. Hit.Score has BM25Score and
// HybridRank populated; the wrapping fields duplicate them for callers
// that prefer the rerank-package shape (e.g. footprint summarization).
type Result struct {
	Hit         types.Hit
	BM25Score   float64
	HybridRank  int // 1-based, after RRF
	OriginalIdx int // 0-based position in the input (= vector rank - 1)
}

// Stats summarizes the rerank effect for footprint logging. Designed for
// single-fingerprint log lines: every field is a scalar an operator can
// grep / aggregate without joining multiple log entries.
type Stats struct {
	CandidatesIn     int     // = len(input)
	CandidatesOut    int     // = len(output); should equal input
	RankChanges      int     // count of candidates whose HybridRank != OriginalIdx+1
	Top1ScoreDelta   float64 // Normalized score: final top-1 minus original top-1; positive when BM25 pulled a still-strong vector match to top, negative when it demoted vector
	Top1ChunkID      string  // first 12 hex chars of new top-1 chunk_id, for fingerprint drift detection
	BM25Disabled     bool    // true when Rerank was a no-op (empty input / empty intent / no query tokens)
}

// Rerank applies candidate-set BM25 to the input hits and combines with
// the existing vector ordering via Reciprocal Rank Fusion. Inputs are
// assumed already sorted by vector score descending (the contract the
// store layer satisfies); each input's vector rank is taken as its
// position + 1.
//
// The IDF in this BM25 is computed over the candidate set only, not the
// global chunk corpus — ADR-006 documents this candidate-set bias. The
// signal is still useful as a *rerank within candidates* because IDF
// rewards terms that vary across the local set, which is what
// differentiates one candidate from another.
//
// On empty input, empty intent, or zero query tokens the function
// returns the input unchanged (each result carries its original index
// as HybridRank, BM25Score = 0). Callers can detect the no-op via
// Stats.BM25Disabled.
func Rerank(candidates []Candidate, intent string) ([]Result, Stats) {
	n := len(candidates)
	stats := Stats{CandidatesIn: n, CandidatesOut: n}
	out := make([]Result, n)
	for i, c := range candidates {
		out[i] = Result{Hit: c.Hit, OriginalIdx: i, HybridRank: i + 1}
	}
	if n == 0 || intent == "" {
		stats.BM25Disabled = true
		if n > 0 {
			stats.Top1ChunkID = firstChunkIDPrefix(out[0].Hit)
		}
		return out, stats
	}
	queryTokens := Tokenize(intent)
	if len(queryTokens) == 0 {
		stats.BM25Disabled = true
		stats.Top1ChunkID = firstChunkIDPrefix(out[0].Hit)
		return out, stats
	}

	// Build the candidate-set BM25 corpus. The doc IDs are derived from
	// the candidate's position so we can map back unambiguously when the
	// caller's Corpus strings collide (which they shouldn't for distinct
	// chunks, but defending the contract is cheap).
	docs := make([]Document, n)
	docIDOf := func(i int) string { return fmt.Sprintf("c%d", i) }
	for i, c := range candidates {
		docs[i] = Document{ID: docIDOf(i), Tokens: Tokenize(c.Corpus)}
	}
	scorer := NewOkapi()
	scorer.Index(docs)

	// Score each candidate. We need both the score (for BM25Score field
	// + the top1_score_delta fingerprint) and the BM25 rank (for RRF
	// fusion). Stable-sort the BM25 ranks so candidates with identical
	// scores (very common when the query misses most candidates) retain
	// their vector ordering.
	type ranked struct {
		idx  int
		bm25 float64
	}
	bm25Sorted := make([]ranked, n)
	for i := range candidates {
		bm25Sorted[i] = ranked{idx: i, bm25: scorer.Score(queryTokens, docIDOf(i))}
	}
	sort.SliceStable(bm25Sorted, func(i, j int) bool {
		if bm25Sorted[i].bm25 != bm25Sorted[j].bm25 {
			return bm25Sorted[i].bm25 > bm25Sorted[j].bm25
		}
		return bm25Sorted[i].idx < bm25Sorted[j].idx
	})
	bm25RankByIdx := make([]int, n)
	bm25ScoreByIdx := make([]float64, n)
	for rank, r := range bm25Sorted {
		bm25RankByIdx[r.idx] = rank + 1
		bm25ScoreByIdx[r.idx] = r.bm25
	}

	// RRF fusion. vector_rank is i+1 (input order); bm25_rank from the
	// table above. Tiebreak on original idx keeps the vector ordering
	// when fusion scores collide.
	type fused struct {
		idx    int
		fusion float64
	}
	fusedRank := make([]fused, n)
	for i := 0; i < n; i++ {
		vectorRank := i + 1
		bm25Rank := bm25RankByIdx[i]
		fusedRank[i] = fused{
			idx:    i,
			fusion: 1.0/float64(DefaultRRFK+vectorRank) + 1.0/float64(DefaultRRFK+bm25Rank),
		}
	}
	sort.SliceStable(fusedRank, func(i, j int) bool {
		if fusedRank[i].fusion != fusedRank[j].fusion {
			return fusedRank[i].fusion > fusedRank[j].fusion
		}
		return fusedRank[i].idx < fusedRank[j].idx
	})

	// Emit the reordered result. We mutate a copy of the Hit so the
	// caller's input slice is untouched (Hit contains Chunk which is
	// large; we only copy the small Score struct field).
	reordered := make([]Result, n)
	rankChanges := 0
	for newRank, f := range fusedRank {
		h := candidates[f.idx].Hit
		h.Score.BM25Score = bm25ScoreByIdx[f.idx]
		h.Score.HybridRank = newRank + 1
		reordered[newRank] = Result{
			Hit:         h,
			BM25Score:   bm25ScoreByIdx[f.idx],
			HybridRank:  newRank + 1,
			OriginalIdx: f.idx,
		}
		if newRank != f.idx {
			rankChanges++
		}
	}

	stats.RankChanges = rankChanges
	if n > 0 {
		stats.Top1ChunkID = firstChunkIDPrefix(reordered[0].Hit)
		stats.Top1ScoreDelta = reordered[0].Hit.Score.Normalized - candidates[0].Hit.Score.Normalized
	}
	return reordered, stats
}

// BuildCorpusText is the canonical D3-B corpus shape: symbol_name +
// the first non-empty line of the chunk text (typically the signature).
// Centralized so engine.Search and tests don't drift on the spec.
//
// When the chunk has no symbol name (rare for code chunks, common for
// doc / header chunks), the first text line stands in for symbol.
func BuildCorpusText(h types.Hit) string {
	sym := h.Chunk.SymbolName
	firstLine := firstNonEmptyLine(h.Chunk.Text)
	switch {
	case sym != "" && firstLine != "":
		return sym + " " + firstLine
	case sym != "":
		return sym
	default:
		return firstLine
	}
}

func firstNonEmptyLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[:i]
			if line = trimSpace(line); line != "" {
				return line
			}
			s = s[i+1:]
			i = -1
		}
	}
	return trimSpace(s)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func firstChunkIDPrefix(h types.Hit) string {
	id := h.Chunk.ID
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
