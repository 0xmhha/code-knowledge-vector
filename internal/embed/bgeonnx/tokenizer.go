package bgeonnx

import "context"

// Tokenizer turns a batch of strings into the token tensors ONNX
// Runtime expects. The shape `[][]int64` is row-major: outer index is
// batch position, inner index is token position. Truncation /
// padding-to-max is the tokenizer's responsibility, not the session's.
//
// Production impl: `daulet/tokenizers` wrapping the HuggingFace Rust
// tokenizers crate; reads the model's tokenizer.json directly.
type Tokenizer interface {
	// Tokenize returns input_ids + attention_mask for batch. maxLen
	// is the hard upper bound (model-specific, e.g. 512 for
	// bge-large-en-v1.5) — tokens beyond it MUST be truncated, never
	// silently retained.
	Tokenize(ctx context.Context, batch []string, maxLen int) (TokenizedBatch, error)
}

// TokenizedBatch is the on-the-wire shape for one Embed() call.
// All slices are sized [batchSize][seqLen] where seqLen is uniform
// across the batch (padded with 0 attention). TokenTypeIDs is left
// nil — extra inputs the ONNX graph needs (token_type_ids for BERT,
// position_ids for Qwen2, etc.) are synthesized in Session.Run from
// ModelConfig.ExtraInputs.
type TokenizedBatch struct {
	InputIDs      [][]int64
	AttentionMask [][]int64
	TokenTypeIDs  [][]int64 // reserved; populated only if a model registers it via Tokenize
}

// stubTokenizer returns ErrNotImplemented — used when the package is
// built without `-tags bgeonnx` so callers can't accidentally rely on
// uninitialized state.
type stubTokenizer struct{}

func (stubTokenizer) Tokenize(_ context.Context, _ []string, _ int) (TokenizedBatch, error) {
	return TokenizedBatch{}, ErrNotImplemented
}
