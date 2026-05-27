// Package registry holds the model configuration catalog. Every
// supported embedding model is registered here with its identity
// (name, dimension, max input tokens), file layout, ONNX graph
// signature, pooling strategy, and download source.
//
// Backend-agnostic: both the ONNX adapter and future backends
// (Ollama, CoreML) read model metadata from this package. Adding a
// new model is a single registry entry — no code changes in backend
// packages.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// PoolingMode selects how the inference backend collapses the
// per-token hidden states [batch, seqLen, hidden] into a single
// sentence vector [batch, hidden].
type PoolingMode int

const (
	// PoolingCLS takes the [CLS] token (position 0) hidden state.
	// Used by BERT-family models (bge-large-en-v1.5).
	PoolingCLS PoolingMode = iota

	// PoolingMean averages all attended token hidden states.
	// Used by sentence-BERT variants and bge-m3.
	PoolingMean

	// PoolingLastToken takes the last attended token. Used by
	// decoder-only models (Qwen2-based, e5-mistral).
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
// doesn't produce but the ONNX graph requires. BERT models need
// token_type_ids (zeros); decoder-only models need position_ids.
//
// Returned slice is row-major [batch * seqLen], int64.
type ExtraInputFn func(batch, seqLen int) []int64

// ZeroExtraInput returns a [batch*seqLen] slice of zeros.
// Used for BERT-family token_type_ids (single-segment input).
func ZeroExtraInput(batch, seqLen int) []int64 {
	return make([]int64, batch*seqLen)
}

// PositionIDsExtraInput returns position_ids [0..seqLen) broadcast
// over batch. Used by decoder-only ONNX exports (Qwen2, Mistral).
func PositionIDsExtraInput(batch, seqLen int) []int64 {
	out := make([]int64, batch*seqLen)
	for b := range batch {
		base := b * seqLen
		for t := range seqLen {
			out[base+t] = int64(t)
		}
	}
	return out
}

// ModelConfig describes one embedding model: identity, file layout,
// ONNX graph signature, pooling strategy, and download source.
type ModelConfig struct {
	Name      string // stable identifier, e.g. "bge-large-en-v1.5"
	Dim       int    // output vector dimension
	MaxInput  int    // max input tokens (sequences longer get truncated)
	Normalize string // "l2" or ""

	// File layout (relative to model directory)
	OnnxFile      string // e.g. "onnx/model.onnx"
	TokenizerFile string // e.g. "tokenizer.json"

	// ONNX graph signature
	InputOrder  []string                // input tensor names in order
	Outputs     []string                // output tensor names
	ExtraInputs map[string]ExtraInputFn // synthetic inputs beyond tokenizer output

	// Pooling strategy for sentence vector extraction
	Pooling PoolingMode

	// Download source
	HFRepo string // HuggingFace repository, e.g. "BAAI/bge-large-en-v1.5"

	// Memory estimate in MB for the build pipeline's pre-flight check.
	// Includes weights + runtime + working set + compile headroom.
	// 0 disables the check for this model.
	EstimatedRAMMB uint64
}

// FetchFiles returns the relative paths the downloader needs to fetch.
func (c ModelConfig) FetchFiles() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range []string{c.OnnxFile, c.TokenizerFile} {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// DefaultModelDir returns ~/.cache/ckv/models/<name>.
func (c ModelConfig) DefaultModelDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "ckv", "models", c.Name), nil
}

// DefaultModelName is used when no model is explicitly specified.
const DefaultModelName = "bge-large-en-v1.5"

// models is the global model catalog.
var models = map[string]ModelConfig{
	"bge-large-en-v1.5": {
		Name:          "bge-large-en-v1.5",
		Dim:           1024,
		MaxInput:      512,
		Normalize:     "l2",
		OnnxFile:      "onnx/model.onnx",
		TokenizerFile: "tokenizer.json",
		HFRepo:        "BAAI/bge-large-en-v1.5",
		InputOrder:    []string{"input_ids", "attention_mask", "token_type_ids"},
		Outputs:       []string{"last_hidden_state"},
		ExtraInputs: map[string]ExtraInputFn{
			"token_type_ids": ZeroExtraInput,
		},
		Pooling:        PoolingCLS,
		EstimatedRAMMB: 5000,
	},
	"bge-m3": {
		Name:          "bge-m3",
		Dim:           1024,
		MaxInput:      8192,
		Normalize:     "l2",
		OnnxFile:      "onnx/model.onnx",
		TokenizerFile: "tokenizer.json",
		HFRepo:        "BAAI/bge-m3",
		InputOrder:    []string{"input_ids", "attention_mask", "token_type_ids"},
		Outputs:       []string{"last_hidden_state"},
		ExtraInputs: map[string]ExtraInputFn{
			"token_type_ids": ZeroExtraInput,
		},
		Pooling:        PoolingCLS,
		EstimatedRAMMB: 2200,
	},
	"embeddinggemma-300m": {
		Name:          "embeddinggemma-300m",
		Dim:           768,
		MaxInput:      2048,
		Normalize:     "l2",
		OnnxFile:      "onnx/model.onnx",
		TokenizerFile: "tokenizer.json",
		HFRepo:        "google/embeddinggemma-300m",
		InputOrder:    []string{"input_ids", "attention_mask", "token_type_ids"},
		Outputs:       []string{"last_hidden_state"},
		ExtraInputs: map[string]ExtraInputFn{
			"token_type_ids": ZeroExtraInput,
		},
		Pooling:        PoolingMean,
		EstimatedRAMMB: 2500,
	},
}

// Lookup returns the config for the named model, or an error listing
// all known models.
func Lookup(name string) (ModelConfig, error) {
	cfg, ok := models[name]
	if !ok {
		return ModelConfig{}, fmt.Errorf("unknown model %q (known: %v)", name, Names())
	}
	return cfg, nil
}

// List returns all registered model configs, sorted by name.
func List() []ModelConfig {
	out := make([]ModelConfig, 0, len(models))
	for _, cfg := range models {
		out = append(out, cfg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns sorted model names.
func Names() []string {
	names := make([]string, 0, len(models))
	for k := range models {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
