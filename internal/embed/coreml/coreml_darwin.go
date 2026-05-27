//go:build darwin

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
)

// Open loads a CoreML model (.mlpackage or .mlmodelc) and returns an
// Adapter that runs inference on the ANE/GPU/CPU via CoreML.
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

	return &Adapter{
		modelPath: opts.ModelPath,
		modelName: opts.ModelName,
		dim:       opts.Dim,
		maxSeqLen: opts.MaxSeqLen,
	}, nil
}

func (a *Adapter) Name() string        { return a.modelName }
func (a *Adapter) Dimension() int      { return a.dim }
func (a *Adapter) MaxInputTokens() int { return a.maxSeqLen }

func (a *Adapter) Close() error {
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

	batchSize := len(batch)
	seqLen := a.maxSeqLen

	// Tokenize: for now, simple whitespace split + truncate/pad.
	// Production use should wire libtokenizers via the same CGO path
	// as bgeonnx. This placeholder enables end-to-end testing of the
	// CoreML inference path.
	inputIDs := make([]int64, batchSize*seqLen)
	attMask := make([]int64, batchSize*seqLen)

	for i, text := range batch {
		tokens := simpleTokenize(text, seqLen)
		base := i * seqLen
		for j, tok := range tokens {
			inputIDs[base+j] = int64(tok)
			attMask[base+j] = 1
		}
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

// simpleTokenize is a placeholder tokenizer that maps bytes to token IDs.
// Production should use libtokenizers for proper BPE/WordPiece tokenization.
func simpleTokenize(text string, maxLen int) []int {
	// CLS=101, SEP=102, bytes offset by 1000
	tokens := []int{101} // [CLS]
	for _, b := range []byte(text) {
		if len(tokens) >= maxLen-1 {
			break
		}
		tokens = append(tokens, int(b)+1000)
	}
	tokens = append(tokens, 102) // [SEP]
	// Pad to maxLen
	for len(tokens) < maxLen {
		tokens = append(tokens, 0)
	}
	return tokens[:maxLen]
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
