package types

import "context"

// Embedder turns text into a fixed-dimension vector. Implementations:
//   - internal/embed/mock      — deterministic hash-based, for tests
//   - internal/embed/bgeonnx   — ONNX-backed local embedder; supports
//                                a model registry (see model_config.go),
//                                currently bge-large-en-v1.5 by default.
//
// Plan §3:
//   - Name returns a stable identifier persisted in the manifest
//     (e.g. "bge-large-en-v1.5"). Mismatch on rebuild → IndexUnavailable.
//   - Dimension is the vector length. Used to size the sqlite-vec column.
//   - MaxInputTokens is the model's context limit; the chunker truncates
//     overlong text up front (signature stays at the head).
//   - Embed is batched. Implementations choose internal batching (CPU≈32,
//     GPU≈256) but the caller MAY pass arbitrary-size slices.
type Embedder interface {
	Name() string
	Dimension() int
	MaxInputTokens() int
	Embed(ctx context.Context, batch []string) ([][]float32, error)
}
