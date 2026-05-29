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
	"strings"
	"sync"

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/freshness"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultThreshold drops hits whose normalized score is below this
// value. 0.4 is conservative — with the mock embedder it
// trims obvious noise without rejecting real matches.
const DefaultThreshold = 0.4

// DefaultBudgetTokens is the response-side token budget.
// 4000 tokens ≈ 16K chars of snippet content across all hits.
const DefaultBudgetTokens = 4000

// DefaultK is the top-K when the caller omits it.
const DefaultK = 10

// overfetchFactor is the multiplier used when over-fetching from the
// store so the post-filter / citation / threshold pipeline has enough
// candidates to satisfy K.
const overfetchFactor = 3

// Options configure a single Search invocation. Zero values resolve to
// the documented defaults.
type Options struct {
	K            int          // top-K (0 → DefaultK)
	Filter       types.Filter // pre-filter (lang / path / kind / commit)
	BudgetTokens int          // snippet density budget (0 → DefaultBudgetTokens)
	Threshold    float64      // min normalized score (0 → DefaultThreshold; <0 disables)
	SrcRoot      string       // absolute path used by citation enforcement;
	// when empty, the manifest's SrcRoot is used.

	// ExamplesK splits test-file hits out of the main Hits slice into a
	// separate Examples slice in the response. Up to ExamplesK test
	// chunks pass through — distinct from K, which counts only the
	// non-test (primary implementation) hits. Defaults to 0 → no
	// separation, every hit goes through Hits as before.
	//
	// Why: an LLM coding agent gets cleaner signal when primary code
	// (the actual implementation it should mimic) is separate from
	// usage examples (tests that show how the code is called). With
	// them mixed, top-5 can be diluted by 2-3 test results that compete
	// for context window space.
	ExamplesK int

	// TraceID is a caller-supplied correlation ID propagated to the
	// footprint span and echoed in Response.Metadata.TraceID. Useful
	// when a single user action fans out into multiple Search calls
	// (CKS multiplex, retries) and the operator wants to grep all
	// related log entries together. Empty → engine generates one from
	// the intent hash + a monotonic suffix.
	TraceID string

	// DryRun, when true, validates the request (intent, embedder,
	// manifest identity) but skips embedding, store search, citation
	// enforcement, and density adjustment. The response carries
	// metadata only — Hits and Examples are empty. Useful for caller-
	// side budget / plan validation without paying the query cost
	// or polluting the footprint log with hot-path entries.
	DryRun bool

	// MaxDensity caps the snippet rendering tier (B3 ladder).
	// "" / DensityFull → no cap, downgrade only under budget pressure
	// (the documented default). DensitySignature5 → start every hit at
	// signature+N and downgrade to signature_only under pressure.
	// DensitySignatureOnly → emit signatures only, never bodies; useful
	// when the caller already has the chunk body cached and just wants
	// pointers (e.g. CLI list-mode).
	MaxDensity DensityTier

	// SignatureContextLines tunes the SignatureWithContext tier (number
	// of non-blank lines kept after the signature). 0 →
	// DefaultSignatureContextLines (5). Larger values keep more body
	// in the middle tier at the cost of fewer hits fitting the budget.
	SignatureContextLines int

	// Aliases is the vocabulary-bridge glossary applied to the intent
	// at the very start of Search. When non-nil, ExpandQuery
	// widens the intent with code-keyword aliases before embedding —
	// useful for Korean / domain-vague queries against an English code
	// corpus. Nil leaves the intent untouched (off by default).
	//
	// The expanded intent is what gets embedded and what appears in
	// the query.embed sub-span fingerprint; the *original* intent is
	// still echoed in the query.search top-level span for log triage.
	Aliases AliasMap

	// EnableBM25Rerank turns on the optional BM25 rerank pass between
	// store.search and threshold.drop. When true, the engine builds a
	// candidate-set BM25 over the vector hits (corpus = chunk.SymbolName
	// + first text line), scores each candidate against the *original*
	// intent (alias expansion is embed-side only), and reorders by RRF
	// fusion of vector + BM25 ranks. Default false — vector-only
	// behavior preserved as the baseline.
	//
	// The flag intentionally lives at the query-call level rather than
	// the Engine: comparison runs (`--bm25-rerank` vs no-flag) share
	// the same Engine + index, only the rerank step toggles.
	EnableBM25Rerank bool

	// EnableScoreBoost applies signature/doc/recent/package multipliers
	// to the rerank pass. Default false.
	EnableScoreBoost bool
	Boost            BoostOptions

	// EnableMetadataEnrichment attaches git log history to each hit.
	// Default false (calls git log per file, has IO cost).
	EnableMetadataEnrichment bool
	MaxHistoryCommits        int // 0 → 5
}

// Hit is the response-shaped record: only what callers (LLM, CLI) need.
// We deliberately omit Chunk.Text — Snippet is the budget-adjusted view.
type Hit struct {
	ChunkID  string         `json:"chunk_id"`
	Citation types.Citation `json:"citation"`
	Snippet  string         `json:"snippet"`
	// Density names which 3-tier ladder rung this Snippet was rendered
	// at (DensityFull / DensitySignature5 / DensitySignatureOnly).
	// Useful for downstream UIs that want to badge compressed hits or
	// for eval pipelines counting how often the budget forced a
	// downgrade. Omitted when empty (e.g. callers building Hit by hand).
	Density DensityTier `json:"density,omitempty"`
	// StaleCitation propagates from types.Hit: the chunk's recorded
	// commit_hash disagrees with the current source-tree HEAD. The
	// citation file/lines still resolve — the content there may have
	// shifted since the index was built. Consumers can render a badge
	// or schedule a reindex. Omitted when false.
	StaleCitation bool             `json:"stale_citation,omitempty"`
	Score         types.HitScore   `json:"score"`
	Language      string           `json:"language"`
	IsTest        bool             `json:"is_test,omitempty"`
	Symbol        string           `json:"symbol,omitempty"`
	SymbolKind    types.SymbolKind `json:"symbol_kind,omitempty"`
	CKGNodeID     string           `json:"ckg_node_id,omitempty"`
	// Category and Guidance are populated by the policy loader at build
	// time. Category labels the chunk's domain ("consensus", "state",
	// ...); Guidance lists what the agent should also review, test, and
	// watch out for when modifying this code. Both omitted when the
	// chunk did not match any policy rule (or no policy was loaded).
	Category string                      `json:"category,omitempty"`
	Guidance *types.ModificationGuidance `json:"guidance,omitempty"`

	// GitHistory holds recent commits touching this hit's file.
	// Populated when Options.EnableMetadataEnrichment is set.
	GitHistory []CommitSummary `json:"git_history,omitempty"`
}

// Response is the full search response — hits plus diagnostics so MCP
// callers can report freshness/budget without an extra round trip.
type Response struct {
	Hits []Hit `json:"hits"`
	// Examples holds test-file hits separated out from Hits when
	// Options.ExamplesK > 0. Nil/empty otherwise. The ranking inside
	// Examples follows the same score order as Hits — top of Examples
	// is the most-similar test result.
	Examples []Hit            `json:"examples,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
	Metadata ResponseMetadata `json:"metadata"`
}

// ResponseMetadata holds diagnostic metadata so the MCP layer
// can pass this through to LLM callers verbatim.
type ResponseMetadata struct {
	TokensUsed     int    `json:"tokens_used"`
	IndexedHeadCKV string `json:"indexed_head_ckv"`
	Fresh          bool   `json:"fresh"`
	// TraceID echoes Options.TraceID (or the engine-generated fallback)
	// so callers can correlate this response with their footprint log
	// entries. Always set; non-empty.
	TraceID string `json:"trace_id,omitempty"`
	// DryRun mirrors Options.DryRun so callers reading only the
	// response can tell whether Hits actually came from the index.
	DryRun bool `json:"dry_run,omitempty"`
}

// Engine is the long-lived query handler. Hold one per --out directory.
// Concurrency-safe: store.Search is read-only; we never mutate Engine
// state after Open (footprint Logger is sync-safe by contract).
type Engine struct {
	store   *sqlitevec.Store
	emb     types.Embedder
	man     *manifest.Manifest
	srcRoot string
	outDir  string
	fp      *footprint.Logger

	// Core services (independently callable)
	embedSvc     *EmbedService
	searchSvc    *StoreSearchService
	rerankSvc    *RerankService
	boostSvc     *BoostService
	thresholdSvc *ThresholdService
	densitySvc   *DensityService
	enrichSvc    *EnrichService

	// Lazy-built BM25 keyword index for KeywordSearch.
	kwMu  sync.Mutex
	kwIdx *KeywordIndex
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
		store:        store,
		emb:          emb,
		man:          man,
		srcRoot:      man.SrcRoot,
		outDir:       outDir,
		fp:           footprint.Discard(),
		embedSvc:     &EmbedService{emb: emb},
		searchSvc:    &StoreSearchService{store: store},
		rerankSvc:    &RerankService{},
		boostSvc:     &BoostService{},
		thresholdSvc: &ThresholdService{},
		densitySvc:   &DensityService{},
		enrichSvc:    &EnrichService{},
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

// LookupPRsByFile delegates to the store's PR breadcrumb lookup.
func (e *Engine) LookupPRsByFile(ctx context.Context, file string) ([]types.PRRef, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	return e.store.LookupPRsByFile(ctx, file)
}

// NarrowCandidates loads the chunks for the given IDs and returns the
// subset that matches the filter. Score fields are zero-valued (this
// is a metadata refinement step, not a ranking step). Useful for
// multi-hop retrieval flows where an earlier semantic_search produced
// hit IDs and a follow-up wants to drop everything outside a category
// or path.
func (e *Engine) NarrowCandidates(ctx context.Context, ids []string, filter types.Filter) ([]Hit, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	chunks, err := e.store.LookupByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(chunks))
	for _, c := range chunks {
		if !filter.Matches(c) {
			continue
		}
		out = append(out, toResponseHit(types.Hit{Chunk: c}, ""))
	}
	return out, nil
}

// ExpandInFile returns chunks immediately surrounding chunkID inside
// the same file. The window is [match - before, match + after] in
// chunk-index space (chunks ordered by start_line). When chunkID is
// unknown, returns ErrChunkNotFound. Score is zero on the returned
// hits — they are context, not ranked results.
//
// before / after of 0 returns just the source chunk. before or after
// over the edge are silently clamped (no error for "ran out of chunks").
func (e *Engine) ExpandInFile(ctx context.Context, chunkID string, before, after int) ([]Hit, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	seed, err := e.store.LookupByIDs(ctx, []string{chunkID})
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return nil, ErrChunkNotFound
	}
	siblings, err := e.store.LookupByFileOrdered(ctx, seed[0].File)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, c := range siblings {
		if c.ID == chunkID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrChunkNotFound
	}
	lo := idx - before
	if lo < 0 {
		lo = 0
	}
	hi := idx + after + 1
	if hi > len(siblings) {
		hi = len(siblings)
	}
	window := siblings[lo:hi]
	out := make([]Hit, 0, len(window))
	for _, c := range window {
		out = append(out, toResponseHit(types.Hit{Chunk: c}, ""))
	}
	return out, nil
}

// ErrChunkNotFound is returned when ExpandInFile / NarrowCandidates
// is asked about a chunk ID that does not exist in the index.
var ErrChunkNotFound = errors.New("chunk not found")

// InvariantHit pairs a ChunkInvariant chunk with its parsed tier so
// MCP callers can filter without reparsing the back-reference.
type InvariantHit struct {
	ChunkID    string                      `json:"chunk_id"`
	File       string                      `json:"file"`
	StartLine  int                         `json:"start_line"`
	EndLine    int                         `json:"end_line"`
	Marker     string                      `json:"marker"`              // e.g. "CRITICAL", "panic"
	Tier       types.InvariantTier         `json:"tier"`                // 1, 2, or 3
	Text       string                      `json:"text"`                // the invariant statement
	Category   string                      `json:"category,omitempty"`  // inherited from source chunk's policy
	Guidance   *types.ModificationGuidance `json:"guidance,omitempty"`  // ditto
	SourceChunk string                     `json:"source_chunk_id,omitempty"`
}

// FindInvariants returns invariants matching the filter. tierMin (1, 2,
// or 3) drops anything below that confidence tier. The tier is read
// from the source chunk's InvariantRef back-pointer when the source
// is in the index; otherwise the invariant is included at its declared
// SymbolName-based tier where possible.
func (e *Engine) FindInvariants(ctx context.Context, file, category string, tierMin int) ([]InvariantHit, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	chunks, err := e.store.FindInvariants(ctx, file, category)
	if err != nil {
		return nil, err
	}
	if tierMin <= 0 {
		tierMin = int(types.InvariantTierExistingMarker)
	}

	out := make([]InvariantHit, 0, len(chunks))
	for _, c := range chunks {
		// SymbolName carries the marker name (CRITICAL, INVARIANT, ...)
		// by emit-time convention. Tier inference is best-effort: the
		// extractor emits ChunkInvariant chunks but the back-reference
		// (which carries the tier) lives on the source chunk. We assume
		// existing markers when only the chunk is visible — refinement
		// happens later when the agent calls expand_in_file.
		tier := classifyMarkerTier(c.SymbolName)
		if int(tier) < tierMin {
			continue
		}
		out = append(out, InvariantHit{
			ChunkID:   c.ID,
			File:      c.File,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Marker:    c.SymbolName,
			Tier:      tier,
			Text:      c.Text,
			Category:  c.Category,
			Guidance:  c.Guidance,
		})
	}
	return out, nil
}

// ConventionHit returns the raw ConventionStats plus the human summary
// the agent can present without further processing.
type ConventionHit struct {
	ChunkID string         `json:"chunk_id"`
	File    string         `json:"file"`
	Package string         `json:"package"`
	Summary string         `json:"summary"`
	Stats   map[string]any `json:"stats"`
}

// GetConventions returns convention chunks under the package prefix.
// Each Hit carries the deterministic prose summary plus the raw stats
// map so the agent can use whichever shape its prompt expects.
func (e *Engine) GetConventions(ctx context.Context, packagePrefix string) ([]ConventionHit, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	chunks, err := e.store.FindConventions(ctx, packagePrefix)
	if err != nil {
		return nil, err
	}
	out := make([]ConventionHit, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, ConventionHit{
			ChunkID: c.ID,
			File:    c.File,
			Package: c.SymbolName, // emitted as package path
			Summary: c.Text,
			Stats:   c.ConventionStats,
		})
	}
	return out, nil
}

// classifyMarkerTier maps an extractor-stamped marker name onto its
// InvariantTier. Conservative: anything unknown is treated as Tier 3
// (heuristic) so the agent receives the right confidence signal.
func classifyMarkerTier(marker string) types.InvariantTier {
	switch marker {
	case "CRITICAL", "IMPORTANT", "WARNING", "Deprecated":
		return types.InvariantTierExistingMarker
	case "INVARIANT", "CONSENSUS", "SECURITY":
		return types.InvariantTierNewMarker
	default:
		return types.InvariantTierHeuristic
	}
}

// Embedder returns the underlying embedder. Callers (MCP index handler)
// reuse it to avoid re-initializing the embedder for build/reindex.
func (e *Engine) Embedder() types.Embedder {
	if e == nil {
		return nil
	}
	return e.emb
}

// OutDir returns the data directory the engine was opened from.
func (e *Engine) OutDir() string {
	if e == nil {
		return ""
	}
	return e.outDir
}

// Manifest returns a copy of the loaded manifest. Callers (ckv freshness)
// use this to compare on-disk identity without reopening.
func (e *Engine) Manifest() manifest.Manifest {
	if e.man == nil {
		return manifest.Manifest{}
	}
	return *e.man
}

// CheckFreshness compares the loaded manifest's IndexedHead against
// the source tree's current git HEAD and returns ErrFreshnessStale
// (wrapped with the diff) when they differ. Returns nil when fresh,
// no error and no report when git is unavailable (callers who need
// the Report directly should use internal/freshness.Check).
//
// Caller guidance for ErrFreshnessStale: stale results are usually
// still usable for most retrieval (recent edits affect a small subset
// of files), but the caller should schedule `ckv build` when convenient.
func (e *Engine) CheckFreshness() error {
	if e == nil || e.man == nil {
		return errors.New("query: engine has no manifest")
	}
	report, err := freshness.Check(e.srcRoot, e.man.IndexedHead)
	if err != nil {
		return err
	}
	if report.Stale {
		return fmt.Errorf("%w: indexed=%s current=%s (%d changed files)",
			ErrFreshnessStale, report.IndexedHead, report.CurrentHead, len(report.ChangedFiles))
	}
	return nil
}

// EmbedderInfo is the health-endpoint view of the embedder this Engine
// was opened with. Status answers "is this embedder useful for real
// semantic search?" — operators can distinguish "ckv alive but mock
// embedder" from "ckv ready with bgeonnx + model loaded" without
// inspecting the model name string.
//
// Optional fields (Provider, ModelDir) are empty when the embedder
// implementation doesn't expose them — health consumers must tolerate
// the empty case.
type EmbedderInfo struct {
	Name      string `json:"name"`
	Dimension int    `json:"dimension"`
	// Status: "ready" | "stub" | "unavailable"
	//   ready       — embedder loaded, semantic signal expected
	//   stub        — placeholder embedder (mock, hash-based); recall not meaningful
	//   unavailable — embedder is nil or failed to attach
	Status string `json:"status"`
	// Provider is the execution backend tag (bgeonnx-specific):
	// "coreml" | "coreml-fallback-to-cpu" | "cpu" | "". Empty for
	// embedders that don't run a backend (mock).
	Provider string `json:"provider,omitempty"`
	// ModelDir is the on-disk model directory the embedder loaded from.
	// Empty for embedders that don't have one (mock).
	ModelDir string `json:"model_dir,omitempty"`
}

// Warmup forces the embedder to pay its cold-start cost (ONNX
// session load, CoreML compile, tokenizer lazy init) before the
// first user-facing query lands. Runs a single dummy Embed call.
// Returns the embedder's error if it fails to attach — callers
// (health, CLI) should surface it so the operator can investigate
// before traffic starts.
//
// Idempotent and cheap to call repeatedly: after the first call the
// embedder is already warm so subsequent calls just round-trip a
// short batch.
func (e *Engine) Warmup(ctx context.Context) error {
	if e == nil || e.store == nil {
		return errors.New("query: engine is closed")
	}
	if e.emb == nil {
		return errors.New("query: nil embedder")
	}
	_, err := e.emb.Embed(ctx, []string{"warmup"})
	return err
}

// EmbedderInfo extracts metadata via duck typing — bgeonnx exposes
// Provider() and ModelDir(), the mock exposes neither. The returned
// struct is JSON-safe (no unset zero-value confusion for omitted
// fields) so health handlers can serialize it directly.
func (e *Engine) EmbedderInfo() EmbedderInfo {
	if e == nil || e.emb == nil {
		return EmbedderInfo{Status: "unavailable"}
	}
	info := EmbedderInfo{
		Name:      e.emb.Name(),
		Dimension: e.emb.Dimension(),
		Status:    "ready",
	}
	if p, ok := e.emb.(interface{ Provider() string }); ok {
		info.Provider = p.Provider()
	}
	if d, ok := e.emb.(interface{ ModelDir() string }); ok {
		info.ModelDir = d.ModelDir()
	}
	// Stub classification:
	//   - explicit "stub" provider (bgeonnx without -tags bgeonnx), or
	//   - mock-family name (the in-tree mock embedder uses "mock-...").
	switch {
	case info.Provider == "stub":
		info.Status = "unavailable"
	case strings.HasPrefix(info.Name, "mock"):
		info.Status = "stub"
	}
	return info
}

// Embed exposes the embedding service for independent use.
// Converts text into a vector without running the full search pipeline.
func (e *Engine) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.embedSvc.Run(ctx, text)
}

// VectorSearch runs ANN search against the store without reranking,
// threshold, or citation. Returns raw candidate hits.
func (e *Engine) VectorSearch(ctx context.Context, queryVec []float32, k int, filter types.Filter) ([]types.Hit, error) {
	return e.searchSvc.Run(ctx, queryVec, k, filter)
}

// Rerank reorders candidate hits using BM25 + RRF fusion.
func (e *Engine) Rerank(ctx context.Context, candidates []types.Hit, intent string) ([]types.Hit, error) {
	reordered, _ := e.rerankSvc.Run(ctx, candidates, intent)
	return reordered, nil
}

// Search runs the full pipeline (Facade): embed → vector search →
// rerank → threshold → citation → test split → density adjust.
//
// For fine-grained control, use the individual service methods
// (Embed, VectorSearch, Rerank) and compose them manually.
func (e *Engine) Search(ctx context.Context, intent string, opts Options) (*Response, error) {
	if e == nil || e.store == nil {
		return nil, errors.New("query: engine is closed")
	}
	if intent == "" {
		return nil, errors.New("query: empty intent")
	}

	traceID := opts.TraceID
	if traceID == "" {
		traceID = intentHash(intent)
	}

	// Alias expansion
	embedIntent := intent
	if len(opts.Aliases) > 0 {
		if expanded := ExpandQuery(intent, opts.Aliases); expanded != intent {
			embedIntent = expanded
		}
	}

	doneSearch := e.fp.Span("query.search",
		"trace_id", traceID,
		"intent_hash", intentHash(intent),
		"k", opts.K,
		"dry_run", opts.DryRun,
	)

	// Dry-run: return metadata only
	if opts.DryRun {
		doneSearch("dry_run", true)
		return &Response{
			Hits: []Hit{},
			Metadata: ResponseMetadata{
				IndexedHeadCKV: e.man.IndexedHead,
				Fresh:          true,
				TraceID:        traceID,
				DryRun:         true,
			},
		}, nil
	}

	k := opts.K
	if k <= 0 {
		k = DefaultK
	}
	budget := opts.BudgetTokens
	if budget == 0 {
		budget = DefaultBudgetTokens
	}
	if budget > 0 && budget < MinBudgetTokens {
		return nil, fmt.Errorf("%w: budget=%d, minimum=%d",
			ErrBudgetExceeded, budget, MinBudgetTokens)
	}

	// Build and run the pipeline via SearchContext
	sc := &SearchContext{
		Intent:      intent,
		EmbedIntent: embedIntent,
		Options:     opts,
		TraceID:     traceID,
	}

	// 1. Embed
	doneEmbed := e.fp.Span("query.embed", "trace_id", traceID)
	if err := e.embedSvc.RunContext(ctx, sc); err != nil {
		doneEmbed("error", err.Error())
		return nil, err
	}
	doneEmbed("dim", len(sc.QueryVec))

	// 2. Vector search
	doneStore := e.fp.Span("query.store.search", "trace_id", traceID, "k_overfetch", k*overfetchFactor)
	if err := e.searchSvc.RunContext(ctx, sc); err != nil {
		doneStore("error", err.Error())
		return nil, err
	}
	doneStore("candidates_out", len(sc.RawHits))

	// 3. Rerank (conditional)
	doneBM25 := e.fp.Span("query.bm25.rerank", "trace_id", traceID, "enabled", opts.EnableBM25Rerank)
	if err := e.rerankSvc.RunContext(ctx, sc); err != nil {
		doneBM25("error", err.Error())
		return nil, err
	}
	doneBM25("candidates_out", len(sc.RawHits))

	// 3.5 Score boost (conditional)
	doneBoost := e.fp.Span("query.score.boost", "trace_id", traceID, "enabled", opts.EnableScoreBoost)
	e.boostSvc.RunContext(sc, e.man.IndexedHead)
	doneBoost("candidates_out", len(sc.RawHits))

	// 4. Threshold
	doneThreshold := e.fp.Span("query.threshold.drop", "trace_id", traceID)
	if err := e.thresholdSvc.RunContext(ctx, sc); err != nil {
		doneThreshold("error", err.Error())
		return nil, err
	}
	doneThreshold("candidates_out", len(sc.FilteredHits))

	// 5. Citation enforcement
	srcRoot := opts.SrcRoot
	if srcRoot == "" {
		srcRoot = e.srcRoot
	}
	doneCitation := e.fp.Span("query.citation.enforce", "trace_id", traceID)
	currentHead := currentGitHead(srcRoot)
	enforced, dropped, stale := EnforceCitationsAt(sc.FilteredHits, srcRoot, currentHead)
	sc.DroppedCitations = dropped
	sc.StaleCitations = stale
	if dropped > 0 {
		sc.Warnings = append(sc.Warnings, fmt.Sprintf("dropped_%d_unverified_citations", dropped))
	}
	if stale > 0 {
		sc.Warnings = append(sc.Warnings, fmt.Sprintf("stale_%d_citations", stale))
	}
	doneCitation("candidates_out", len(enforced), "dropped", dropped)
	if len(sc.FilteredHits) > 0 && len(enforced) == 0 {
		return nil, fmt.Errorf("%w: dropped all %d hits — source may be out of sync",
			ErrCitationNotFound, dropped)
	}

	// 6. Test split + trim
	sc.PrimaryHits, sc.ExampleHits = splitByTest(enforced, opts.ExamplesK > 0)
	if len(sc.PrimaryHits) > k {
		sc.PrimaryHits = sc.PrimaryHits[:k]
	}
	if opts.ExamplesK > 0 && len(sc.ExampleHits) > opts.ExamplesK {
		sc.ExampleHits = sc.ExampleHits[:opts.ExamplesK]
	}

	// 7. Density adjust
	doneDensity := e.fp.Span("query.density.adjust", "trace_id", traceID, "budget_tokens", budget)
	if err := e.densitySvc.RunContext(ctx, sc); err != nil {
		doneDensity("error", err.Error())
		return nil, err
	}
	doneDensity("tokens_used", sc.TokensUsed)

	// 8. Metadata enrichment (git history per hit's file)
	doneEnrich := e.fp.Span("query.metadata.enrich", "trace_id", traceID, "enabled", opts.EnableMetadataEnrichment)
	e.enrichSvc.RunContext(ctx, sc, srcRoot)
	doneEnrich()

	response := &Response{
		Hits:     sc.FinalHits,
		Examples: sc.FinalExamples,
		Warnings: sc.Warnings,
		Metadata: ResponseMetadata{
			TokensUsed:     sc.TokensUsed,
			IndexedHeadCKV: e.man.IndexedHead,
			Fresh:          true,
			TraceID:        traceID,
		},
	}
	doneSearch(
		"hits", len(sc.FinalHits),
		"examples", len(sc.FinalExamples),
		"tokens_used", sc.TokensUsed,
	)
	return response, nil
}

// splitByTest partitions hits into (primary, examples) by IsTest. When
// separateTests is false, every hit lands in primary and examples is
// nil — preserving the single-list behavior for callers that haven't
// opted in via Options.ExamplesK.
func splitByTest(hits []types.Hit, separateTests bool) (primary, examples []types.Hit) {
	if !separateTests {
		return hits, nil
	}
	for _, h := range hits {
		if h.Chunk.IsTest {
			examples = append(examples, h)
		} else {
			primary = append(primary, h)
		}
	}
	return primary, examples
}

// intentHash is the SHA256 prefix of the intent string. Stable across
// runs so working memory can dedupe repeat questions. We log only the
// hex prefix (12 chars) to keep log volume manageable.
func intentHash(intent string) string {
	sum := sha256.Sum256([]byte(intent))
	return hex.EncodeToString(sum[:6]) // 12 hex chars
}
