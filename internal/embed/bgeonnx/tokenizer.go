package bgeonnx

import "context"

// Tokenizer turns a batch of strings into the token tensors ONNX
// Runtime expects. The shape `[][]int64` is row-major: outer index is
// batch position, inner index is token position. Truncation /
// padding-to-max is the tokenizer's responsibility, not the session's.
//
// Production impl (D1-FU-2): `daulet/tokenizers` wrapping the
// HuggingFace Rust tokenizers crate; reads tokenizer.json directly.
type Tokenizer interface {
	// Tokenize returns input_ids + attention_mask for batch. maxLen
	// is the hard upper bound (8192 for bge-code-v1) — tokens beyond
	// it MUST be truncated, never silently retained.
	Tokenize(ctx context.Context, batch []string, maxLen int) (TokenizedBatch, error)
}

// TokenizedBatch is the on-the-wire shape for one Embed() call.
// All slices are sized [batchSize][seqLen] where seqLen is uniform
// across the batch (padded with 0 attention).
type TokenizedBatch struct {
	InputIDs       [][]int64
	AttentionMask  [][]int64
	// TokenTypeIDs is optional — bge-* models don't use it, but bge-code-v1
	// occasionally appears with a vestigial input. We allocate when
	// the session requests it.
	TokenTypeIDs [][]int64
}

// stubTokenizer returns ErrNotImplemented. Used until D1-FU-2 lands.
type stubTokenizer struct{}

func (stubTokenizer) Tokenize(_ context.Context, _ []string, _ int) (TokenizedBatch, error) {
	return TokenizedBatch{}, ErrNotImplemented
}
