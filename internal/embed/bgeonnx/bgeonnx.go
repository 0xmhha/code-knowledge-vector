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
	"os"
	"path/filepath"
)

// ErrNotImplemented is returned by Embed() until the production
// Session + Tokenizer have been wired (see D1-FU-1 / D1-FU-2 in
// docs/d1-onnx-poc.md §6).
var ErrNotImplemented = errors.New("bgeonnx: ONNX runtime not wired yet — see docs/d1-onnx-poc.md §3.2")

// Model identity. Stable across the runtime wiring swap so manifest
// schemas stay backward-compatible.
const (
	ModelName      = "bge-code-v1"
	ModelDim       = 1024
	ModelMaxInput  = 8192
	ModelNormalize = "l2"
)

// Files we expect at ModelDir. Mirror docs/d1-onnx-poc.md §2.3.
// `config.json` is also part of the model bundle but Open() doesn't
// require it — the tokenizer + ONNX runtime read it lazily if present.
const (
	fileModel     = "model.onnx"
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
		a.tokenizer = stubTokenizer{}
	}
	if a.session == nil {
		a.session = stubSession{}
	}
	return a, nil
}

// Close releases the underlying Session. Idempotent.
func (a *Adapter) Close() error {
	if a == nil || a.session == nil {
		return nil
	}
	err := a.session.Close()
	a.session = nil
	return err
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
