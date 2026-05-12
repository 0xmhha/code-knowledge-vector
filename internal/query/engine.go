// Package query is the read path: open an index built by internal/build
// and serve semantic_search. It owns:
//
//   - manifest validation on Open (dim + model identity must match the
//     caller's Embedder)
//   - over-fetch + threshold drop + citation enforcement
//   - snippet density compression under a token budget
//
// Designed to be importable by both `ckv query` (CLI) and the future
// MCP server (`ckv mcp`) without code duplication.
package query
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultThreshold drops hits whose normalized score is below this
// value. Plan §6.4. 0.4 is conservative — with the mock embedder it
// trims obvious noise without rejecting real matches.
const DefaultThreshold = 0.4

// DefaultBudgetTokens is the response-side token budget. Plan §6.1 +
// §7.3. 4000 tokens ≈ 16K chars of snippet content across all hits.
const DefaultBudgetTokens = 4000

// DefaultK is the top-K when the caller omits it.
const DefaultK = 10

// overfetchFactor is the multiplier used when over-fetching from the
// store so the post-filter / citation / threshold pipeline has enough
// candidates to satisfy K. Plan §6.1.
const overfetchFactor = 3

// ErrIndexUnavailable signals that the on-disk index cannot be served
// by the supplied Embedder — most commonly because the indexed model
// differs from the query-time model. Surfaces directly through MCP as
// the IndexUnavailable error variant (plan §8.4).
var ErrIndexUnavailable = errors.New("query: index unavailable")

// Options configure a single Search invocation. Zero values resolve to
// the documented defaults.
type Options struct {
	K            int          // top-K (0 → DefaultK)
	Filter       types.Filter // pre-filter (lang / path / kind / commit)
	BudgetTokens int          // snippet density budget (0 → DefaultBudgetTokens)
	Threshold    float64      // min normalized score (0 → DefaultThreshold; <0 disables)
	SrcRoot      string       // absolute path used by citation enforcement;
	                          // when empty, the manifest's SrcRoot is used.
}

// Hit is the response-shaped record: only what callers (LLM, CLI) need.
// We deliberately omit Chunk.Text — Snippet is the budget-adjusted view.
type Hit struct {
	ChunkID    string           `json:"chunk_id"`
	Citation   types.Citation   `json:"citation"`
	Snippet    string           `json:"snippet"`
	Score      types.HitScore   `json:"score"`
	Language   string           `json:"language"`
	Symbol     string           `json:"symbol,omitempty"`
	SymbolKind types.SymbolKind `json:"symbol_kind,omitempty"`
	CKGNodeID  string           `json:"ckg_node_id,omitempty"`
}

// Response is the full search response — hits plus diagnostics so MCP
// callers can report freshness/budget without an extra round trip.
type Response struct {
	Hits     []Hit            `json:"hits"`
	Warnings []string         `json:"warnings,omitempty"`
	Metadata ResponseMetadata `json:"metadata"`
}

// ResponseMetadata mirrors plan §7.3 (EvidencePack) so the MCP layer
// can pass this through to LLM callers verbatim.
type ResponseMetadata struct {
	TokensUsed     int    `json:"tokens_used"`
	IndexedHeadCKV string `json:"indexed_head_ckv"`
	Fresh          bool   `json:"fresh"`
}

// Engine is the long-lived query handler. Hold one per --out directory.
// Concurrency-safe: store.Search is read-only; we never mutate Engine
// state after Open (footprint Logger is sync-safe by contract).
type Engine struct {
	store   *sqlitevec.Store
	emb     types.Embedder
	man     *manifest.Manifest
	srcRoot string
	fp      *footprint.Logger
}

// OpenOption customizes Engine construction (functional options).
type OpenOption func(*Engine)

// WithFootprint attaches a logger to the Engine. Every Search emits a
// span (query.search.start / query.search.done) including intent hash,
// hit count, citation drops, and latency. Nil-safe.
func WithFootprint(fp *footprint.Logger) OpenOption {
	return func(e *Engine) { e.fp = fp }
}

// Open loads the index at outDir and validates the manifest against
// the supplied Embedder. Mismatches produce ErrIndexUnavailable with a
// human-readable cause; callers (CLI / MCP) surface a "ckv reindex"
// hint to the user. opts apply after the engine is constructed but
// before it is returned.
func Open(outDir string, emb types.Embedder, opts ...OpenOption) (*Engine, error) {
	if emb == nil {
		return nil, errors.New("query: nil Embedder")
	}
	man, err := manifest.Load(outDir)
	if err != nil {
		if errors.Is(err, manifest.ErrNotFound) {
			return nil, fmt.Errorf("%w: no manifest at %s — run `ckv build` first", ErrIndexUnavailable, outDir)
		}
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	if man.EmbeddingDim != emb.Dimension() {
		return nil, fmt.Errorf("%w: dim mismatch (index=%d, embedder=%d) — run `ckv build` to reindex",
			ErrIndexUnavailable, man.EmbeddingDim, emb.Dimension())
	}
	if man.EmbeddingModel != "" && man.EmbeddingModel != emb.Name() {
		return nil, fmt.Errorf("%w: model mismatch (index=%q, embedder=%q) — run `ckv build` to reindex",
			ErrIndexUnavailable, man.EmbeddingModel, emb.Name())
	}

	dbPath := filepath.Join(outDir, "vector.db")
	store, err := sqlitevec.Open(dbPath, emb.Dimension())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	e := &Engine{
		store:   store,
		emb:     emb,
		man:     man,
		srcRoot: man.SrcRoot,
		fp:      footprint.Discard(),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.fp == nil {
		e.fp = footprint.Discard()
	}
	return e, nil
}

// Close releases the underlying store. Idempotent.
func (e *Engine) Close() error {
	if e == nil || e.store == nil {
		return nil
	}
	err := e.store.Close()
	e.store = nil
	return err
}

// Manifest returns a copy of the loaded manifest. Callers (ckv freshness)
// use this to compare on-disk identity without reopening.
func (e *Engine) Manifest() manifest.Manifest {
	if e.man == nil {
		return manifest.Manifest{}
	}
	return *e.man
}

// Search runs the full pipeline: embed → over-fetch → threshold drop →
// citation enforcement → snippet density → top-K trim.
//
// Returns an empty Hits slice (never nil) when nothing passes the
// gates; the Warnings field surfaces the cause (e.g. all_results_below_threshold).
func (e *Engine) Search(ctx context.Context, intent string, opts Options) (*Response, error) {
	if e == nil || e.store == nil {
		return nil, errors.New("query: engine is closed")
	}
	if intent == "" {
		return nil, errors.New("query: empty intent")
	}

	doneSearch := e.fp.Span("query.search",
		"intent_hash", intentHash(intent),
		"intent_preview", preview(intent, 80),
		"k", opts.K,
		"threshold", opts.Threshold,
		"language", opts.Filter.Language,
	)

	k := opts.K
	if k <= 0 {
		k = DefaultK
	}
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	budget := opts.BudgetTokens
	if budget <= 0 {
		budget = DefaultBudgetTokens
	}

	// Step 1: embed
	vecs, err := e.emb.Embed(ctx, []string{intent})
	if err != nil {
		return nil, fmt.Errorf("embed intent: %w", err)
	}
	if len(vecs) == 0 {
		return nil, errors.New("query: embedder returned no vector")
	}

	// Step 2: store-side ANN with over-fetch
	overfetch := k * overfetchFactor
	rawHits, err := e.store.Search(ctx, vecs[0], overfetch, opts.Filter)
	if err != nil {
		return nil, fmt.Errorf("store search: %w", err)
	}

	warnings := []string{}

	// Step 3: threshold drop (after store-side ANN so we keep the rank
	// monotonicity for downstream RRF input).
	var passed []types.Hit
	for _, h := range rawHits {
		if threshold > 0 && h.Score.Normalized < threshold {
			continue
		}
		passed = append(passed, h)
	}
	if len(rawHits) > 0 && len(passed) == 0 {
		warnings = append(warnings, "all_results_below_threshold")
	}

	// Step 4: citation enforcement — drop any hit whose file we can't
	// resolve against the recorded src_root. Plan §5 + §7.4.
	srcRoot := opts.SrcRoot
	if srcRoot == "" {
		srcRoot = e.srcRoot
	}
	enforced, droppedCitations := EnforceCitations(passed, srcRoot)
	if droppedCitations > 0 {
		warnings = append(warnings, fmt.Sprintf("dropped_%d_unverified_citations", droppedCitations))
	}

	// Step 5: snippet density under budget. We trim to k *before*
	// budgeting so the budget only applies to the visible hits.
	if len(enforced) > k {
		enforced = enforced[:k]
	}
	hits, tokensUsed := DensityAdjust(enforced, budget)

	response := &Response{
		Hits:     hits,
		Warnings: warnings,
		Metadata: ResponseMetadata{
			TokensUsed:     tokensUsed,
			IndexedHeadCKV: e.man.IndexedHead,
			Fresh:          true, // freshness check arrives with `ckv freshness`
		},
	}
	doneSearch(
		"hits", len(hits),
		"citation_drops", droppedCitations,
		"warnings", warnings,
		"tokens_used", tokensUsed,
		"top_file", topFile(hits),
	)
	return response, nil
}

// intentHash is the SHA256 prefix of the intent string. Stable across
// runs so working memory can dedupe repeat questions. We log only the
// hex prefix (12 chars) to keep log volume manageable.
func intentHash(intent string) string {
	sum := sha256.Sum256([]byte(intent))
	return hex.EncodeToString(sum[:6]) // 12 hex chars
}

// preview truncates intent for human-readable logs without dumping the
// whole prompt. The full intent is recoverable via intent_hash + the
// caller's own log of the original request.
func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// topFile returns the file of the top-ranked hit, or empty when none.
// Useful single field for grep-friendly footprint audit.
func topFile(hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	return hits[0].Citation.File
}
