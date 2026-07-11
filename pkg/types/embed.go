package types

import (
	"context"
	"fmt"
)

// EmbeddingIdentity describes the vector space an Embedder produces. It is
// model-agnostic: each embedder fills it from its own configuration (e.g. a
// model registry), so adding or swapping an embedding model needs no change
// here — the identity flows from the model definition.
type EmbeddingIdentity struct {
	Provider  string // backend that produced the vectors, e.g. "ollama", "bgeonnx", "mock"
	Model     string // model name, e.g. "bge-m3"
	Dim       int    // vector dimension
	Pooling   string // "cls" | "mean" | "last_token"; "" when the backend does not expose it
	Normalize string // "l2" | "none"; "" when unknown
}

// Checksum is a stable identity string for the embedding space. Two embedders
// that produce comparable vectors yield the same Checksum; any difference
// (provider, model, dim, pooling, normalization) yields a different one. It is
// recorded in the manifest at build time and compared on Open so a
// silently-incompatible index/embedder pair (e.g. Ollama bge-m3 vs ONNX
// bge-m3) is rejected with a reindex hint instead of returning meaningless
// similarity scores.
func (id EmbeddingIdentity) Checksum() string {
	return fmt.Sprintf("provider=%s;model=%s;dim=%d;pooling=%s;normalize=%s",
		id.Provider, id.Model, id.Dim, id.Pooling, id.Normalize)
}

// Embedder turns text into a fixed-dimension vector. Implementations:
//   - internal/embed/mock      — deterministic hash-based, for tests
//   - internal/embed/bgeonnx   — ONNX-backed local embedder; supports
//     a model registry (see model_config.go),
//     currently bge-large-en-v1.5 by default.
//   - pkg/embed/ollama         — Ollama HTTP API backend.
//
// Embedder interface contract:
//   - Identity reports the embedding space (provider/model/dim/pooling/
//     normalization). Every backend implements it from its own model
//     definition, so a new model or provider conforms to the same contract
//     and gets index-compatibility enforcement (query.Open) for free.
//     Name() and Dimension() are kept for convenience and MUST agree with
//     Identity().Model and Identity().Dim.
//   - Name returns a stable identifier persisted in the manifest
//     (e.g. "bge-large-en-v1.5"). Mismatch on rebuild → IndexUnavailable.
//   - Dimension is the vector length. Used to size the sqlite-vec column.
//   - MaxInputTokens is the model's context limit; the chunker truncates
//     overlong text up front (signature stays at the head).
//   - Embed is batched. Implementations choose internal batching (CPU≈32,
//     GPU≈256) but the caller MAY pass arbitrary-size slices.
type Embedder interface {
	Identity() EmbeddingIdentity
	Name() string
	Dimension() int
	MaxInputTokens() int
	Embed(ctx context.Context, batch []string) ([][]float32, error)
}

// QueryEmbedder is an optional Embedder capability for asymmetric models —
// models whose query representation differs from their passage representation.
// Retrieval code type-asserts an Embedder to QueryEmbedder and calls EmbedQuery
// for the query side; embedders with no query/passage asymmetry simply do not
// implement it and the caller falls back to Embed. Qwen3-Embedding is the
// motivating case: it recommends a query-side "Instruct:" prompt while passages
// are embedded raw. Passages continue to go through Embed.
type QueryEmbedder interface {
	Embedder
	EmbedQuery(ctx context.Context, queries []string) ([][]float32, error)
}
