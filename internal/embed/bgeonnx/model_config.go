// Per-model configuration registry. Keeping every model's
// idiosyncrasy (ONNX input signature, pooling strategy, file layout)
// in one place means adding a new model is a single-file change —
// no build-tag fanout, no hidden hardcoded assumptions in session_impl.go.
//
// Add a new model by appending an entry to `registry` below and (if
// the model needs a non-zero ExtraInput type or a new pooling mode)
// implementing the helper here.
//
// This file deliberately has no build tag — registry lookups happen
// even in the default (no-bgeonnx) build so Adapter.Name() /
// Dimension() / MaxInputTokens() report the right values without
// requiring the CGO libraries.

package bgeonnx

import (
	"fmt"
	"sort"
)

// PoolingMode selects how Session collapses [batch, seqLen, hidden]
// down to [batch, hidden]. Each bge-* family was trained with one
// specific choice — using the wrong one tanks recall measurably.
type PoolingMode int

const (
	// PoolingCLS takes the [CLS] (position 0) hidden state. BERT-family
	// embedders trained with sentence-transformers + cls pooling.
	// Example: bge-large-en-v1.5 (1_Pooling/config.json:
	// pooling_mode_cls_token=true).
	PoolingCLS PoolingMode = iota

	// PoolingMean averages all attended token hidden states. Common
	// in older sentence-BERT variants and bge-m3. Mask-aware.
	PoolingMean

	// PoolingLastToken takes the last attended token. Used by
	// decoder-only LLM embedders (Qwen2-based bge-code-v1,
	// e5-mistral-7b, etc.). Not yet wired — adding a model that needs
	// this requires implementing lastTokenPoolNormalize in pooling.go.
	PoolingLastToken
)

func (p PoolingMode) String() string {
	switch p {
	case PoolingCLS:
		return "cls"
	case PoolingMean:
		return "mean"
	case PoolingLastToken:
		return "last_token"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// ExtraInputFn builds a synthetic input tensor that the tokenizer
// doesn't produce but the ONNX graph requires. BERT exports need
// token_type_ids = zeros; Qwen2 / decoder-only exports often need
// position_ids = [0, 1, ..., seqLen-1] broadcast over batch.
//
// Returned slice is row-major [batch * seqLen], int64.
type ExtraInputFn func(batch, seqLen int) []int64

// ZeroExtraInput returns a [batch*seqLen] slice of zeros. Used for
// BERT-family token_type_ids (single-segment embedders).
func ZeroExtraInput(batch, seqLen int) []int64 {
	return make([]int64, batch*seqLen)
}

// PositionIDsExtraInput returns position_ids = [0..seqLen) broadcast
// over batch. Used by decoder-only / Qwen2-family ONNX exports.
// Provided for the bge-code-v1 D2 work; not yet referenced by any
// registry entry.
func PositionIDsExtraInput(batch, seqLen int) []int64 {
	out := make([]int64, batch*seqLen)
	for b := 0; b < batch; b++ {
		base := b * seqLen
		for t := 0; t < seqLen; t++ {
			out[base+t] = int64(t)
		}
	}
	return out
}

// ModelConfig captures everything the bgeonnx adapter needs to know
// about one specific embedding model. Static — populated at compile
// time via the registry below.
type ModelConfig struct {
	// Identity
	Name      string // e.g. "bge-large-en-v1.5"
	Dim       int    // output vector dimension (hidden_size in HF config.json)
	MaxInput  int    // max_position_embeddings — sequences longer get truncated
	Normalize string // "l2" or "" — informational; actual normalize happens inside pooling

	// File layout, relative to ModelDir
	OnnxFile      string // e.g. "onnx/model.onnx" (HF sentence-transformers layout)
	TokenizerFile string // typically "tokenizer.json"

	// ONNX graph signature
	InputOrder []string // exact order the graph expects: ["input_ids", "attention_mask", "token_type_ids"]
	Outputs    []string // typically just ["last_hidden_state"]

	// Inputs the tokenizer doesn't produce (synthesized at Run() time).
	// Keys must be present in InputOrder. The two tokenizer outputs
	// (input_ids, attention_mask) come from TokenizedBatch and do NOT
	// appear in this map.
	ExtraInputs map[string]ExtraInputFn

	// Pooling strategy
	Pooling PoolingMode

	// EstimatedRAMMB is the resident set the embedder needs to load and
	// run a single batch on this model: weights + ORT runtime base +
	// working set + CoreML compile headroom on macOS. The build
	// pipeline's memory guard multiplies this by a 1.5× factor before
	// comparing to host AvailableMB. 0 disables the pre-check for this
	// model (treated as "unknown — proceed").
	EstimatedRAMMB uint64
}

// registry holds every model the bgeonnx adapter supports.
// Adding a new model is a single-file change to this map.
var registry = map[string]ModelConfig{
	"bge-large-en-v1.5": {
		Name:          "bge-large-en-v1.5",
		Dim:           1024,
		MaxInput:      512,
		Normalize:     "l2",
		OnnxFile:      "onnx/model.onnx",
		TokenizerFile: "tokenizer.json",
		InputOrder:    []string{"input_ids", "attention_mask", "token_type_ids"},
		Outputs:       []string{"last_hidden_state"},
		ExtraInputs: map[string]ExtraInputFn{
			"token_type_ids": ZeroExtraInput,
		},
		Pooling: PoolingCLS,
		// 1.3 GB FP32 weights + ORT runtime (~300 MB) + working set
		// (~500 MB for batch=32, seq=512) + CoreML compile spike
		// (observed ~2 GB transient on Apple Silicon). Round up to
		// 5000 MB. Conservative — the guard prefers refusing a job
		// to OOM-killing the host.
		EstimatedRAMMB: 5000,
	},
	// Future entries (each adds 15-25 lines, no other file changes):
	// "bge-code-v1": {
	//     Name: "bge-code-v1", Dim: 1536, MaxInput: 32768, Normalize: "l2",
	//     OnnxFile: "onnx/model.onnx", TokenizerFile: "tokenizer.json",
	//     InputOrder: []string{"input_ids", "attention_mask", "position_ids"},
	//     Outputs: []string{"last_hidden_state"},
	//     ExtraInputs: map[string]ExtraInputFn{"position_ids": PositionIDsExtraInput},
	//     Pooling: PoolingLastToken,  // requires implementing lastTokenPoolNormalize
	// },
	// "bge-m3": {
	//     ..., Pooling: PoolingMean, ...
	// },
}

// DefaultModelName is used when Open() is called without ModelName
// or ModelDir hints. Pick the smallest, most reliable model so a
// blank-config invocation succeeds.
const DefaultModelName = "bge-large-en-v1.5"

// LookupModel returns the config for `name`, or an error listing
// known models if unknown.
func LookupModel(name string) (ModelConfig, error) {
	cfg, ok := registry[name]
	if !ok {
		return ModelConfig{}, fmt.Errorf("bgeonnx: unknown model %q (known: %v)", name, registeredNames())
	}
	return cfg, nil
}

func registeredNames() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
