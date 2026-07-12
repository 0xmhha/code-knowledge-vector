//go:build darwin && tokenizers

package coreml

/*
#cgo LDFLAGS: -framework CoreML -framework Foundation
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"context"
	"fmt"
	"math"
	"unsafe"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Open loads a CoreML model (.mlpackage or .mlmodelc) and its
// tokenizer, returning an Adapter that runs inference on ANE/GPU/CPU.
func Open(opts Options) (*Adapter, error) {
	if opts.ModelPath == "" {
		return nil, fmt.Errorf("coreml: model path is required")
	}
	if opts.Dim <= 0 {
		return nil, fmt.Errorf("coreml: dimension must be > 0")
	}
	if opts.MaxSeqLen <= 0 {
		opts.MaxSeqLen = 512
	}

	cPath := C.CString(opts.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	result := C.ckv_coreml_load(cPath)
	if result != 0 {
		return nil, fmt.Errorf("coreml: failed to load model from %s (error %d)", opts.ModelPath, result)
	}

	a := &Adapter{
		modelPath: opts.ModelPath,
		modelName: opts.ModelName,
		dim:       opts.Dim,
		maxSeqLen: opts.MaxSeqLen,
	}

	if opts.TokenizerPath != "" {
		tk, err := newTokenizer(opts.TokenizerPath)
		if err != nil {
			C.ckv_coreml_unload()
			return nil, err
		}
		a.tokenizer = tk
	}
	return a, nil
}

func (a *Adapter) Name() string        { return a.modelName }
func (a *Adapter) Dimension() int      { return a.dim }
func (a *Adapter) MaxInputTokens() int { return a.maxSeqLen }

// Identity reports the embedding space. CoreML pooling/normalization are
// baked into the compiled model and not exposed here, so those fields are
// left empty; Provider+Model+Dim distinguish a CoreML-built index.
func (a *Adapter) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{
		Provider: "coreml",
		Model:    a.modelName,
		Dim:      a.dim,
	}
}

func (a *Adapter) Close() error {
	if a.tokenizer != nil {
		_ = a.tokenizer.Close()
	}
	C.ckv_coreml_unload()
	return nil
}

// Embed tokenizes the input texts and runs CoreML inference.
// Each text is independently tokenized, padded to maxSeqLen, and
// passed through the model. The output hidden states are pooled
// (CLS token) and L2-normalized.
func (a *Adapter) Embed(_ context.Context, batch []string) ([][]float32, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	if a.tokenizer == nil {
		return nil, fmt.Errorf("coreml: tokenizer not configured — pass TokenizerPath in Options")
	}

	batchSize := len(batch)
	seqLen := a.maxSeqLen

	inputIDs := make([]int64, batchSize*seqLen)
	attMask := make([]int64, batchSize*seqLen)

	for i, text := range batch {
		ids, mask, err := a.tokenizer.Encode(text, seqLen)
		if err != nil {
			return nil, fmt.Errorf("coreml: tokenize[%d]: %w", i, err)
		}
		base := i * seqLen
		copy(inputIDs[base:base+seqLen], ids)
		copy(attMask[base:base+seqLen], mask)
	}

	// Allocate output buffer [batch, seqLen, dim]
	outputSize := batchSize * seqLen * a.dim
	output := make([]float32, outputSize)

	rc := C.ckv_coreml_predict(
		(*C.int64_t)(unsafe.Pointer(&inputIDs[0])),
		(*C.int64_t)(unsafe.Pointer(&attMask[0])),
		C.int(batchSize),
		C.int(seqLen),
		C.int(a.dim),
		(*C.float)(unsafe.Pointer(&output[0])),
	)
	if rc != 0 {
		return nil, fmt.Errorf("coreml: predict failed (error %d)", rc)
	}

	// Pool: CLS token (position 0) + L2 normalize
	result := make([][]float32, batchSize)
	for i := range batchSize {
		vec := make([]float32, a.dim)
		base := i * seqLen * a.dim
		copy(vec, output[base:base+a.dim])
		l2Normalize(vec)
		result[i] = vec
	}
	return result, nil
}

func l2Normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return
	}
	for i := range vec {
		vec[i] /= norm
	}
}
