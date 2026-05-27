package types

import "context"

// VectorStore is the persistence + ANN search surface. Implementations:
//   - internal/store/sqlitevec — SQLite + vec0 virtual table
//   - internal/store/memory    — in-RAM map (tests + dev loop)
//
// All methods are safe to call from a single goroutine; concurrent
// callers must serialize themselves (the indexer pipeline is sequential
// per file).
type VectorStore interface {
	// Upsert inserts or replaces chunks keyed by Chunk.ID. The vector is
	// derived from chunk.Text via the configured Embedder before calling.
	// Note the (chunk, embedding) pairing is positional and equal-length.
	Upsert(ctx context.Context, chunks []Chunk, embeddings [][]float32) error

	// DeleteByFile removes every chunk whose File equals path. Used by
	// the incremental indexer and by the file-rename safety path.
	DeleteByFile(ctx context.Context, path string) error

	// Search returns the top-k nearest chunks under cosine distance,
	// post-filtered by `filter`. k is the desired result count; the
	// implementation may over-fetch (e.g. 3*k) for re-rank head-room.
	Search(ctx context.Context, query []float32, k int, filter Filter) ([]Hit, error)

	// Stats reports indexed counts and the embedding model identity
	// stored at build time. Cheap (single SQL roundtrip).
	Stats(ctx context.Context) (Stats, error)

	// Close releases the backing handle. Idempotent.
	Close() error
}
