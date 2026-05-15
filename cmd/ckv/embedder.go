package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/bgeonnx"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// resolveEmbedder picks the embedder backend for build/query/mcp/eval
// from the per-command --embedder flag. mock is the safe default —
// works with no system dependencies and matches the eval baseline.
// bgeonnx requires the model + libonnxruntime; see docs/d1-onnx-poc.md.
//
// modelDir is the user-supplied --model-dir (passed through to bgeonnx).
// Empty modelDir lets bgeonnx fall back to its conventional cache
// location (~/.cache/ckv/models/<name>).
func resolveEmbedder(name, modelDir string) (types.Embedder, func(), error) {
	noop := func() {}
	switch name {
	case "", "mock":
		return mock.Default(), noop, nil
	case "bgeonnx":
		a, err := bgeonnx.Open(bgeonnx.Options{ModelDir: modelDir})
		if err != nil {
			return nil, noop, fmt.Errorf("embedder bgeonnx: %w", err)
		}
		// bgeonnx today returns ErrNotImplemented from Embed(); let
		// the caller see that as a runtime error rather than failing
		// at Open. Tell the user once so they can prepare.
		fmt.Fprintln(os.Stderr, "ckv: --embedder=bgeonnx is scaffolded; "+
			"Embed() will fail until D1-FU-1/-2 wires the ONNX runtime. "+
			"See docs/d1-onnx-poc.md §3.2.")
		return a, func() { _ = a.Close() }, nil
	default:
		return nil, noop, errors.New("unknown --embedder " + name + " (supported: mock, bgeonnx)")
	}
}
