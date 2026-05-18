// Package bgeonnx is the production Embedder for bge-code-v1 over ONNX
// Runtime. The package is organized as three concerns:
//
//   - bgeonnx.go (this file): the Embedder facade — identity, lifecycle,
//     and the Embed() call that orchestrates tokenizer → session → pool.
//   - tokenizer.go: turns []string into (input_ids, attention_mask).
//     Today: an interface + a stub. Production impl arrives via
//     daulet/tokenizers (see docs/d1-onnx-poc.md §2.2).
//   - session.go: turns tokens into 1024-d vectors via ONNX Runtime.
//     Today: an interface + a stub. Production impl arrives via
//     yalue/onnxruntime_go behind the `bgeonnx` build tag.
//
// Why scaffolded instead of fully implemented in this commit: the
// runtime + tokenizer libraries each need their system dependency
// (libonnxruntime.dylib + libtokenizers.so) plus the ~520 MB model
// artifact downloaded out of band. Building the scaffold first lets
// the operator follow the runbook in docs/d1-onnx-poc.md §3.2 to
// finish the integration in a focused 30-minute spike.
//
// During the scaffold period, Embed() returns ErrNotImplemented so
// callers (cmd/ckv `--embedder=bgeonnx` users) get a clear "you
// haven't finished the install" signal instead of mysterious zero
// vectors.
package bgeonnx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrNotImplemented is returned by Embed() until the production
// Session + Tokenizer have been wired (see D1-FU-1 / D1-FU-2 in
// docs/d1-onnx-poc.md §6).
var ErrNotImplemented = errors.New("bgeonnx: ONNX runtime not wired yet — see docs/d1-onnx-poc.md §3.2")

// Model identity. Stable across the runtime wiring swap so manifest
// schemas stay backward-compatible. Values reflect bge-large-en-v1.5
// (BertModel, 24 layers, 335M params); see docs/d1-onnx-poc.md.
const (
	ModelName      = "bge-large-en-v1.5"
	ModelDim       = 1024
	ModelMaxInput  = 512
	ModelNormalize = "l2"
)

// Files we expect at ModelDir. The HuggingFace repo for
// bge-large-en-v1.5 ships the ONNX export under `onnx/model.onnx`
// alongside the PyTorch weights, so we point straight at it instead
// of requiring an `optimum-cli` conversion step.
const (
	fileModel     = "onnx/model.onnx"
	fileTokenizer = "tokenizer.json"
)

// Adapter is the Embedder facade. Hold one per process (Sessions are
// expensive to construct and threadsafe for inference).
type Adapter struct {
	modelDir  string
	tokenizer Tokenizer
	session   Session
}

// Options control adapter construction. ModelDir is required and
// must contain model.onnx + tokenizer.json. Tokenizer / Session
// override the default factories — used by tests and by the smoke
// build-tag variant.
type Options struct {
	ModelDir  string
	Tokenizer Tokenizer
	Session   Session
}

// Open validates the model directory and constructs an Adapter. The
// default Tokenizer + Session implementations return ErrNotImplemented
// from their work methods so callers know the runtime is not yet
// active. Production wiring replaces both via Options.
func Open(opts Options) (*Adapter, error) {
	if opts.ModelDir == "" {
		// Fall back to the documented cache location so users with
		// the model in the conventional spot don't need a flag.
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("bgeonnx: resolve model dir: %w", err)
		}
		opts.ModelDir = filepath.Join(home, ".cache", "ckv", "models", ModelName)
	}
	for _, f := range []string{fileModel, fileTokenizer} {
		if _, err := os.Stat(filepath.Join(opts.ModelDir, f)); err != nil {
			return nil, fmt.Errorf("bgeonnx: %s missing in %s — see docs/d1-onnx-poc.md §3.2", f, opts.ModelDir)
		}
	}
	a := &Adapter{
		modelDir:  opts.ModelDir,
		tokenizer: opts.Tokenizer,
		session:   opts.Session,
	}
	if a.tokenizer == nil {
		tk, err := defaultTokenizer(opts.ModelDir)
		if err != nil {
			return nil, fmt.Errorf("bgeonnx: default tokenizer: %w", err)
		}
		a.tokenizer = tk
	}
	if a.session == nil {
		s, err := defaultSession(opts.ModelDir)
		if err != nil {
			return nil, fmt.Errorf("bgeonnx: default session: %w", err)
		}
		a.session = s
	}
	return a, nil
}

// Close releases the underlying Session and Tokenizer. The Tokenizer
// interface deliberately omits Close — only some implementations need
// it (the HF binding holds a Rust object) — so we test for io.Closer
// dynamically. Idempotent.
func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	var firstErr error
	if c, ok := a.tokenizer.(io.Closer); ok {
		if err := c.Close(); err != nil {
			firstErr = err
		}
	}
	if a.session != nil {
		if err := a.session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	a.tokenizer = nil
	a.session = nil
	return firstErr
}

func (a *Adapter) Name() string        { return ModelName }
func (a *Adapter) Dimension() int      { return ModelDim }
func (a *Adapter) MaxInputTokens() int { return ModelMaxInput }

// Embed orchestrates tokenizer → session → pooling. Both inner pieces
// today return ErrNotImplemented so this function does too; once the
// runtime wires up D1-FU-1 / D1-FU-2 the orchestration here stays
// unchanged.
func (a *Adapter) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	if a == nil {
		return nil, errors.New("bgeonnx: nil adapter")
	}
	if len(batch) == 0 {
		return nil, nil
	}
	tokens, err := a.tokenizer.Tokenize(ctx, batch, ModelMaxInput)
	if err != nil {
		return nil, fmt.Errorf("bgeonnx: tokenize: %w", err)
	}
	return a.session.Run(ctx, tokens)
}
