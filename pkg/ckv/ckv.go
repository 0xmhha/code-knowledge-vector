// Package ckv is the stable, in-process Go API to a ckv vector index.
//
// Downstream consumers (CKS composer, custom tools, future SDKs) used
// to reach the query engine by spawning the `ckv mcp` binary and
// proxying calls over stdio. That works as a bridge but introduces a
// subprocess hop, a stdio buffer, and lifecycle management the
// consumer has to write. Importing pkg/ckv eliminates all three —
// one Open call returns an Engine you can call SemanticSearch on
// directly.
//
// The package wraps internal/query and is intentionally narrow:
//
//   - Open: load + validate an index directory.
//   - Engine.SemanticSearch: run a query.
//   - Engine.Manifest: read the on-disk identity.
//   - Engine.Close: release the underlying store.
//   - MockEmbedder helpers for callers that don't need real semantics
//     (tests, smoke checks).
//
// Embedder selection lives outside this package: callers pass any
// types.Embedder. For bgeonnx (ONNX runtime), import the bgeonnx
// package directly — its build tag mechanism stays where it belongs.
package ckv

import (
	"context"
	"errors"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ErrIndexUnavailable signals that the on-disk index cannot be served
// by the supplied Embedder. Most commonly: the indexed model differs
// from the query-time model, or there is no manifest at path. Use
// errors.Is to test for it.
var ErrIndexUnavailable = query.ErrIndexUnavailable

// SearchOptions configures a single SemanticSearch call. Zero values
// resolve to documented defaults (K=10, Threshold=0.4, BudgetTokens=4000).
// Aliased from internal/query.Options so consumers don't pull
// internal/query directly.
type SearchOptions = query.Options

// Response is the full search payload — hits, optional examples, and
// metadata (tokens used, indexed head, freshness).
type Response = query.Response

// Hit is a single ranked result: chunk citation, budget-adjusted
// snippet, normalized score, and symbol metadata.
type Hit = query.Hit

// Manifest reports the index's identity at build time: embedding
// model name and dimension, indexed git head, chunk count, source
// root. Engine.Manifest returns a copy.
type Manifest = manifest.Manifest

// OpenOptions configures Open. Embedder is required; the field exists
// as a struct rather than a positional argument so future options
// (footprint logger, read-only flag, etc.) can land without breaking
// existing callers.
type OpenOptions struct {
	// Embedder must match the embedder used at index-build time.
	// Mismatch (different name or dimension) makes Open return an
	// error wrapping ErrIndexUnavailable.
	Embedder types.Embedder
}

// Engine is the in-process ckv query handle. One per index directory.
// Safe to share across goroutines — SemanticSearch is read-only and
// the underlying store handles concurrent reads.
type Engine struct {
	inner *query.Engine
}

// Open loads the index at path, validates the manifest, and returns
// an Engine ready for SemanticSearch.
//
// path is the directory that contains vector.db and manifest.json
// — same value the CLI accepts via --out. Open returns an error
// wrapping ErrIndexUnavailable when:
//   - the directory has no manifest (run `ckv build` first), or
//   - the manifest's embedding identity disagrees with opts.Embedder.
func Open(path string, opts OpenOptions) (*Engine, error) {
	if opts.Embedder == nil {
		return nil, errors.New("ckv: OpenOptions.Embedder is required")
	}
	inner, err := query.Open(path, opts.Embedder)
	if err != nil {
		return nil, err
	}
	return &Engine{inner: inner}, nil
}

// SemanticSearch runs the full retrieval pipeline (embed → over-fetch
// → threshold drop → citation enforcement → snippet density → top-K)
// and returns the response.
//
// intent must be non-empty. ctx cancellation is honored at the
// embedding and store-read boundaries. Calling SemanticSearch after
// Close returns an error.
func (e *Engine) SemanticSearch(ctx context.Context, intent string, opts SearchOptions) (*Response, error) {
	if e == nil || e.inner == nil {
		return nil, errors.New("ckv: engine is closed")
	}
	return e.inner.Search(ctx, intent, opts)
}

// Manifest returns a copy of the loaded manifest. Useful for
// freshness comparisons (caller can diff Manifest().IndexedHead
// against the source tree's current git HEAD).
func (e *Engine) Manifest() Manifest {
	if e == nil || e.inner == nil {
		return Manifest{}
	}
	return e.inner.Manifest()
}

// Close releases the underlying store. Idempotent — calling twice is
// safe; subsequent SemanticSearch calls return an error.
func (e *Engine) Close() error {
	if e == nil || e.inner == nil {
		return nil
	}
	err := e.inner.Close()
	e.inner = nil
	return err
}

// Warmup forces the embedder to pay its cold-start cost (ONNX session
// load, CoreML compile, tokenizer init) by running a no-op embed.
// Recommended once at startup so the first user-facing call doesn't
// pay the multi-second compile spike.
//
// Calling Warmup after Close returns an error. Idempotent on a live
// Engine — subsequent calls just round-trip a short embed.
func (e *Engine) Warmup(ctx context.Context) error {
	if e == nil || e.inner == nil {
		return errors.New("ckv: engine is closed")
	}
	return e.inner.Warmup(ctx)
}

// MockEmbedder returns ckv's deterministic mock embedder — the same
// implementation that backs `ckv build --embedder=mock`. Suitable for
// tests, smoke checks, and downstream integration tests that need a
// no-dependency Embedder.
func MockEmbedder() types.Embedder {
	return mock.Default()
}

// NewMockEmbedder returns a mock embedder with the given dimension
// and name. Useful for exercising the model-mismatch error path:
// build an index with MockEmbedder(), then try to Open with
// NewMockEmbedder(dim, "different") and observe ErrIndexUnavailable.
func NewMockEmbedder(dim int, name string) types.Embedder {
	return mock.New(dim, name)
}
