package query

import (
	"context"

	"github.com/0xmhha/code-knowledge-vector/internal/query/bm25"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// KeywordIndex is a lazily-built in-memory BM25 index over every chunk
// in the store. It rebuilds on first KeywordSearch after Engine.Open;
// subsequent searches reuse the cached index.
//
// Rebuild on Open is the right trade-off for CKV's expected scale:
// rebuilding 50k chunks takes well under a second on modern hardware,
// and the index then serves arbitrary keyword queries with no per-call
// IO. For sustained high-write workloads a future revision can move
// this into the persistence layer.
//
// Build serialization is handled by Engine.kwMu — KeywordIndex itself
// is read-only after construction so it needs no internal lock.
type KeywordIndex struct {
	scorer *bm25.Okapi
	idToCh map[string]types.Chunk // chunk_id → Chunk (for hit reconstruction)
}

// buildKeywordIndex reads every chunk from the store and builds a fresh
// BM25 index over (symbol_name + file + text). The triple is the same
// shape that an FTS5 backend would search.
func buildKeywordIndex(ctx context.Context, store *sqlitevec.Store) (*KeywordIndex, error) {
	stats, err := store.Stats(ctx)
	if err != nil {
		return nil, err
	}
	if stats.ChunkCount == 0 {
		return &KeywordIndex{scorer: bm25.NewOkapi(), idToCh: map[string]types.Chunk{}}, nil
	}
	_ = stats

	// Pull every chunk via LookupByFileOrdered — there is no global
	// iterator on the Store, but reading file-by-file via a one-pass
	// scan keeps the API surface tight.
	files, err := store.AllFiles(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]bm25.Document, 0, stats.ChunkCount)
	idToCh := make(map[string]types.Chunk, stats.ChunkCount)
	for _, f := range files {
		chunks, lerr := store.LookupByFileOrdered(ctx, f)
		if lerr != nil {
			return nil, lerr
		}
		for _, c := range chunks {
			tokens := bm25.Tokenize(c.SymbolName + " " + c.File + " " + c.Text)
			docs = append(docs, bm25.Document{ID: c.ID, Tokens: tokens})
			idToCh[c.ID] = c
		}
	}

	sc := bm25.NewOkapi()
	sc.Index(docs)
	return &KeywordIndex{scorer: sc, idToCh: idToCh}, nil
}

// KeywordSearch returns the top-k BM25 hits for the natural-language
// query. The first call after Engine.Open builds the in-memory index
// (cached for subsequent calls). Filter is applied post-rank — we
// over-fetch by 3x when a filter is set, mirroring Store.Search.
func (e *Engine) KeywordSearch(ctx context.Context, query string, k int, filter types.Filter) ([]Hit, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	if k <= 0 {
		return nil, nil
	}

	idx, err := e.keywordIndex(ctx)
	if err != nil {
		return nil, err
	}
	tokens := bm25.Tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	fetch := k
	if !filter.IsZero() {
		fetch = k * 3
	}
	scored := idx.scorer.TopK(tokens, fetch)

	out := make([]Hit, 0, k)
	rank := 0
	for _, sd := range scored {
		c, ok := idx.idToCh[sd.ID]
		if !ok {
			continue
		}
		if !filter.Matches(c) {
			continue
		}
		rank++
		h := toResponseHit(types.Hit{Chunk: c, Score: types.HitScore{
			Normalized: normalizeBM25(sd.Score),
			BM25Score:  sd.Score,
			HybridRank: rank,
		}}, "")
		out = append(out, h)
		if len(out) >= k {
			break
		}
	}
	return out, nil
}

// keywordIndex returns the cached index, building it on first call.
// Subsequent callers wait for the build to finish via the mutex.
func (e *Engine) keywordIndex(ctx context.Context) (*KeywordIndex, error) {
	e.kwMu.Lock()
	defer e.kwMu.Unlock()
	if e.kwIdx != nil {
		return e.kwIdx, nil
	}
	idx, err := buildKeywordIndex(ctx, e.store)
	if err != nil {
		return nil, err
	}
	e.kwIdx = idx
	return idx, nil
}

// normalizeBM25 squashes a raw BM25 score into [0, 1] via a soft
// monotonic transform. Score 0 maps to 0; large scores saturate near
// 1. The exact curve is informative only — consumers should rely on
// the rank ordering, not the absolute value.
func normalizeBM25(score float64) float64 {
	if score <= 0 {
		return 0
	}
	// 1 - 1/(1+score) keeps the function strictly increasing and
	// bounded, with score=1 → 0.5, score=5 → ~0.83, score→∞ → 1.
	return 1 - 1/(1+score)
}
