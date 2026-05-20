// Package bgeonnx is the production Embedder backend running ONNX
// models locally via CGO. The package is organized into three concerns:
//
//   - bgeonnx.go (this file): the Embedder facade — identity, lifecycle,
//     Embed() orchestration. Model-agnostic.
//   - model_config.go: per-model registry of ONNX input signature,
//     pooling strategy, file layout. Adding a new model is one entry.
//   - tokenizer.go / tokenizer_impl.go: text → (input_ids, attention_mask).
//   - session.go / session_impl.go: tokens → pooled+normalized vectors.
//
// Default build (no `bgeonnx` tag) keeps the stub variants so users
// without libonnxruntime + libtokenizers can still build CKV with the
// mock embedder. `-tags bgeonnx` wires the real CGO implementations.
package bgeonnx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrNotImplemented is returned by stub Tokenizer / Session when
// callers compile without `-tags bgeonnx`. Makes "you didn't enable
// the real backend" a clear runtime signal rather than a mysterious
// zero-vector output.
var ErrNotImplemented = errors.New("bgeonnx: ONNX runtime not wired — rebuild with -tags bgeonnx (see docs/d1-installation-guide.md)")

// Adapter is the Embedder facade. One per process: Session
// construction is expensive (~1.5s cold start) so callers hold this
// long-lived and share it across queries.
type Adapter struct {
	modelDir  string
	modelCfg  ModelConfig
	tokenizer Tokenizer
	session   Session
}

// Options control adapter construction.
//
// ModelDir + ModelName resolution order:
//
//  1. ModelName explicit  → registry lookup; ModelDir defaults to
//     ~/.cache/ckv/models/<ModelName>.
//  2. ModelDir explicit, ModelName empty → infer ModelName from
//     ModelDir's basename, then registry lookup.
//  3. Both empty → DefaultModelName + ~/.cache/ckv/models/<default>.
//
// Tokenizer / Session override the default factories. Tests inject
// stubs here to bypass CGO; production callers leave them nil.
type Options struct {
	ModelDir  string
	ModelName string // optional; inferred from ModelDir basename if blank
	Tokenizer Tokenizer
	Session   Session
}

// Open validates the model directory + registry config and constructs
// an Adapter.
func Open(opts Options) (*Adapter, error) {
	cfg, modelDir, err := resolveModel(opts)
	if err != nil {
		return nil, err
	}

	for _, f := range []string{cfg.OnnxFile, cfg.TokenizerFile} {
		if _, err := os.Stat(filepath.Join(modelDir, f)); err != nil {
			return nil, fmt.Errorf("bgeonnx: %s missing in %s — see docs/d1-installation-guide.md", f, modelDir)
		}
	}

	a := &Adapter{
		modelDir:  modelDir,
		modelCfg:  cfg,
		tokenizer: opts.Tokenizer,
		session:   opts.Session,
	}
	if a.tokenizer == nil {
		tk, err := defaultTokenizer(modelDir, cfg)
		if err != nil {
			return nil, fmt.Errorf("bgeonnx: default tokenizer: %w", err)
		}
		a.tokenizer = tk
	}
	if a.session == nil {
		s, err := defaultSession(modelDir, cfg)
		if err != nil {
			return nil, fmt.Errorf("bgeonnx: default session: %w", err)
		}
		a.session = s
	}
	return a, nil
}

func resolveModel(opts Options) (ModelConfig, string, error) {
	name := opts.ModelName
	if name == "" && opts.ModelDir != "" {
		name = filepath.Base(opts.ModelDir)
	}
	if name == "" {
		name = DefaultModelName
	}
	cfg, err := LookupModel(name)
	if err != nil {
		return ModelConfig{}, "", err
	}
	modelDir := opts.ModelDir
	if modelDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ModelConfig{}, "", fmt.Errorf("bgeonnx: resolve model dir: %w", err)
		}
		modelDir = filepath.Join(home, ".cache", "ckv", "models", cfg.Name)
	}
	return cfg, modelDir, nil
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

// Identity methods (Embedder interface).
func (a *Adapter) Name() string        { return a.modelCfg.Name }
func (a *Adapter) Dimension() int      { return a.modelCfg.Dim }
func (a *Adapter) MaxInputTokens() int { return a.modelCfg.MaxInput }

// EstimatedRAMMB exposes ModelConfig.EstimatedRAMMB to callers via duck
// typing. The build pipeline's memory guard reads this through an
// anonymous interface, so we deliberately don't widen the public
// Embedder interface — embedders without an estimate (the mock) just
// don't implement this method and the guard treats them as unknown.
func (a *Adapter) EstimatedRAMMB() uint64 {
	if a == nil {
		return 0
	}
	return a.modelCfg.EstimatedRAMMB
}

// ModelDir returns the directory the adapter loaded model.onnx +
// tokenizer.json from. Empty for a nil adapter. Read by the health
// endpoint via duck typing — embedders that don't have a model dir
// (the mock) don't implement this method and the field stays empty
// in the health response.
func (a *Adapter) ModelDir() string {
	if a == nil {
		return ""
	}
	return a.modelDir
}

// EstimatedRAMMB resolves opts the same way Open() does but stops at
// the registry lookup — no file I/O, no ONNX session, no CoreML
// compile. Returns the registered estimate in MB, or 0 when the
// model can't be resolved. Intended for a CLI-layer memory pre-check
// that runs before Open() pays the multi-GB load + compile cost.
func EstimatedRAMMB(opts Options) uint64 {
	cfg, _, err := resolveModel(opts)
	if err != nil {
		return 0
	}
	return cfg.EstimatedRAMMB
}

// Provider reports the underlying session's execution backend
// ("cpu", "coreml", "coreml-fallback-to-cpu", "stub"). The build
// pipeline uses this to label footprint events so a slow run can be
// diagnosed against the backend that produced it. Returns empty when
// the adapter or its session is nil.
func (a *Adapter) Provider() string {
	if a == nil || a.session == nil {
		return ""
	}
	return a.session.Provider()
}

// Embed orchestrates tokenizer → session. Pooling and normalization
// happen inside Session per the model's ModelConfig.Pooling.
func (a *Adapter) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	if a == nil {
		return nil, errors.New("bgeonnx: nil adapter")
	}
	if len(batch) == 0 {
		return nil, nil
	}
	tokens, err := a.tokenizer.Tokenize(ctx, batch, a.modelCfg.MaxInput)
	if err != nil {
		return nil, fmt.Errorf("bgeonnx: tokenize: %w", err)
	}
	return a.session.Run(ctx, tokens)
}
