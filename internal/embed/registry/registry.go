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

	// QueryInstruct is the task description used to build an asymmetric model's
	// query-side prompt ("Instruct: {QueryInstruct}\nQuery: {q}", Qwen3). Empty
	// for symmetric models (bge-*), which embed queries and passages the same
	// way — those get no query prefix.
	QueryInstruct string

	// KnownDims is the standard ladder of MRL truncation dimensions for an
	// MRL-trained model (ascending, native dim last). --embed-dim must be one of
	// these, keeping indexes on a consistent, comparable set of dimensions. Nil
	// for non-MRL models, where only the native Dim is valid.
	KnownDims []int

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
const DefaultModelName = "bge-m3"

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

	// qwen3-embedding entries are Ollama-only: no ONNX export is configured
	// (OnnxFile/HFRepo empty), so `ckv model fetch` and the bgeonnx backend
	// reject them. The ollama adapter reads Dim/MaxInput from here so the
	// truncation budget matches the model's 32k context window.
	"qwen3-embedding:0.6b": {
		Name:          "qwen3-embedding:0.6b",
		Dim:           1024,
		MaxInput:      32768,
		Normalize:     "l2",
		Pooling:       PoolingLastToken,
		QueryInstruct: qwen3CodeInstruct,
		KnownDims:     []int{256, 512, 1024},
	},
	"qwen3-embedding:4b": {
		Name:          "qwen3-embedding:4b",
		Dim:           2560,
		MaxInput:      32768,
		Normalize:     "l2",
		Pooling:       PoolingLastToken,
		QueryInstruct: qwen3CodeInstruct,
		KnownDims:     []int{512, 1024, 2560},
	},
}

// KnownDims returns a model's standard MRL truncation dimensions (ascending,
// native last), or nil when the model has no MRL ladder (only its native dim is
// valid). Unknown models return nil.
func KnownDims(modelName string) []int {
	if cfg, err := Lookup(modelName); err == nil {
		return cfg.KnownDims
	}
	return nil
}

// qwen3CodeInstruct is the query-side task description for Qwen3-Embedding on a
// code corpus. Qwen3 prepends "Instruct: {task}\nQuery: {q}" to queries only;
// passages are embedded raw. The task nudges the query embedding toward
// code-retrieval intent.
const qwen3CodeInstruct = "Given a natural-language question about a codebase, retrieve the most relevant code."

// QueryInstruct returns a model's query-side task instruction, or "" when the
// model embeds queries and passages symmetrically (no query prefix). Unknown
// models return "".
func QueryInstruct(modelName string) string {
	if cfg, err := Lookup(modelName); err == nil {
		return cfg.QueryInstruct
	}
	return ""
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
