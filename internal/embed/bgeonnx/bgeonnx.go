// Package bgeonnx is the production Embedder for bge-code-v1 over ONNX.
// W2-T4 ships the interface + identity (Name/Dim/MaxInputTokens) so the
// store can be built and tested end-to-end with the mock; the actual
// inference path lands in S1-W3 once the runtime decision (onnxruntime-go
// vs python-subprocess) is finalized.
//
// Until then, Embed returns ErrNotImplemented — callers wire the mock
// embedder for the integration test or check explicit for this error.
package bgeonnx

import (
	"context"
	"errors"
)

// ErrNotImplemented marks calls that need the actual ONNX runtime to be
// installed. Returned by Embed() in W2. W3 swaps the body for real
// inference and removes this error path.
var ErrNotImplemented = errors.New("bgeonnx: ONNX runtime not wired yet (S1-W3)")

// Model identity is fixed at compile time so the manifest schema sees a
// consistent string before/after the W3 runtime swap.
const (
	ModelName       = "bge-code-v1"
	ModelDim        = 1024
	ModelMaxInput   = 8192
	ModelNormalize  = "l2"
)

// Adapter is the future production Embedder. Today it only carries
// identity; calling Embed will fail.
type Adapter struct {
	modelPath string
}

// Open prepares the adapter for the given on-disk model file. The path
// is recorded for W3 (model checksum verification) but not opened yet.
func Open(modelPath string) (*Adapter, error) {
	if modelPath == "" {
		return nil, errors.New("bgeonnx: empty model path")
	}
	return &Adapter{modelPath: modelPath}, nil
}

func (a *Adapter) Name() string        { return ModelName }
func (a *Adapter) Dimension() int      { return ModelDim }
func (a *Adapter) MaxInputTokens() int { return ModelMaxInput }

// Embed will compute bge-code-v1 embeddings once the runtime is wired.
// Today it returns ErrNotImplemented. Callers that need a working
// embedder in W2 should use internal/embed/mock instead.
func (a *Adapter) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrNotImplemented
}
