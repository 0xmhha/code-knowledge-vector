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

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/freshness"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/query/bm25"
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
	// at the very start of Search (R9). When non-nil, ExpandQuery
	// widens the intent with code-keyword aliases before embedding —
	// useful for Korean / domain-vague queries against an English code
	// corpus. Nil leaves the intent untouched (off by default).
	//
	// The expanded intent is what gets embedded and what appears in
	// the query.embed sub-span fingerprint; the *original* intent is
	// still echoed in the query.search top-level span for log triage.
	Aliases AliasMap

	// EnableBM25Rerank turns on the optional NEW-9 / ADR-006 BM25
	// rerank pass between store.search and threshold.drop. When true,
	// the engine builds a candidate-set BM25 over the vector hits
	// (corpus = chunk.SymbolName + first text line per D3-B), scores
	// each candidate against the *original* intent (alias expansion is
	// embed-side only), and reorders by RRF fusion of vector + BM25
	// ranks. Default false — ADR-003 vector-only behavior preserved
	// until the supersede measurement (tracked in ADR-006).
	//
	// The flag intentionally lives at the query-call level rather than
	// the Engine: comparison runs (`--bm25-rerank` vs no-flag) share
	// the same Engine + index, only the rerank step toggles.
	EnableBM25Rerank bool
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

// ResponseMetadata mirrors plan §7.3 (EvidencePack) so the MCP layer
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

// LookupPRsByFile delegates to the store's PR breadcrumb lookup.
func (e *Engine) LookupPRsByFile(ctx context.Context, file string) ([]types.PRRef, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	return e.store.LookupPRsByFile(ctx, file)
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

	// Resolve trace_id: caller-supplied wins; engine-generated fallback
	// is the same intent_hash used in spans so existing log queries
	// keep working.
	traceID := opts.TraceID
	if traceID == "" {
		traceID = intentHash(intent)
	}

	// Vocabulary bridge (NEW-1 / R9): widen the intent with curated
	// keyword aliases before embedding. The expanded intent flows
	// through every downstream step including the embed sub-span
	// fingerprint, so trace consumers see exactly what reached the
	// embedder. The top-level query.search span echoes the *original*
	// intent so log readers can correlate the human input.
	embedIntent := intent
	aliasApplied := 0
	if len(opts.Aliases) > 0 {
		expanded := ExpandQuery(intent, opts.Aliases)
		if expanded != intent {
			embedIntent = expanded
			aliasApplied = 1
		}
	}

	doneSearch := e.fp.Span("query.search",
		"trace_id", traceID,
		"intent_hash", intentHash(intent),
		"intent_preview", preview(intent, 80),
		"k", opts.K,
		"threshold", opts.Threshold,
		"language", opts.Filter.Language,
		"dry_run", opts.DryRun,
		"alias_applied", aliasApplied,
	)

	// Dry-run short-circuit: skip embed, store, citation, density.
	// Return metadata-only response so the caller can validate envelope
	// shape, intent, and embedder identity without paying the query
	// cost. Useful for plan/budget validation in CKS multiplex flows.
	if opts.DryRun {
		response := &Response{
			Hits: []Hit{}, // never nil
			Metadata: ResponseMetadata{
				IndexedHeadCKV: e.man.IndexedHead,
				Fresh:          true,
				TraceID:        traceID,
				DryRun:         true,
			},
		}
		doneSearch("dry_run", true, "trace_id", traceID)
		return response, nil
	}

	k := opts.K
	if k <= 0 {
		k = DefaultK
	}
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	budget := opts.BudgetTokens
	if budget == 0 {
		budget = DefaultBudgetTokens
	}
	// budget < 0 means "disable budgeting"; engine returns hits at full
	// density. budget > 0 but tiny is a contract violation — the engine
	// can't represent even one signature-only hit below MinBudgetTokens.
	if budget > 0 && budget < MinBudgetTokens {
		return nil, fmt.Errorf("%w: budget=%d, minimum=%d (pass <0 to disable budgeting)",
			ErrBudgetExceeded, budget, MinBudgetTokens)
	}

	// Step 1: embed.
	// Sub-span query.embed — tunable knob: --embedder, --model-dir,
	// CKV_DISABLE_CONTEXTUAL_PREFIX, --alias. p95 outliers point to
	// cold-start or CoreML compile churn. embed_intent_hash differs
	// from the top-level intent_hash when alias expansion fired.
	doneEmbed := e.fp.Span("query.embed",
		"trace_id", traceID,
		"alias_applied", aliasApplied,
		"embed_intent_hash", intentHash(embedIntent),
	)
	vecs, err := e.emb.Embed(ctx, []string{embedIntent})
	if err != nil {
		doneEmbed("error", err.Error())
		return nil, fmt.Errorf("embed intent: %w", err)
	}
	if len(vecs) == 0 {
		doneEmbed("error", "no_vector")
		return nil, errors.New("query: embedder returned no vector")
	}
	doneEmbed("dim", len(vecs[0]))

	// Step 2: store-side ANN with over-fetch.
	// Sub-span query.store.search — tunable knob: overfetchFactor (k×3
	// today) and Filter. candidates_out / fingerprint give cheap drift
	// detection: same intent should produce the same top hit between
	// rebuilds when the corpus is stable.
	overfetch := k * overfetchFactor
	doneStore := e.fp.Span("query.store.search",
		"trace_id", traceID,
		"k_overfetch", overfetch,
		"filter_language", opts.Filter.Language,
		"filter_commit_hash", opts.Filter.CommitHash,
	)
	rawHits, err := e.store.Search(ctx, vecs[0], overfetch, opts.Filter)
	if err != nil {
		doneStore("error", err.Error())
		return nil, fmt.Errorf("store search: %w", err)
	}
	doneStore(
		"candidates_out", len(rawHits),
		"top_chunk_id", topChunkID(rawHits),
		"top_score", topScore(rawHits),
	)

	warnings := []string{}

	// Step 2.5 (optional, NEW-9 / ADR-006): BM25 rerank.
	// Default off — ADR-003 vector-only behavior is the baseline. When
	// EnableBM25Rerank is set, we build a candidate-set BM25 corpus
	// (chunk.SymbolName + first text line per D3-B), score against the
	// *original* intent (alias expansion happens for the embedder only),
	// and reorder rawHits by RRF fusion of vector + BM25 ranks.
	//
	// The footprint span fires unconditionally so operators comparing
	// runs with/without the flag see a symmetric trace; `disabled=true`
	// when the flag is off so log readers can filter quickly.
	doneBM25 := e.fp.Span("query.bm25.rerank",
		"trace_id", traceID,
		"candidates_in", len(rawHits),
		"enabled", opts.EnableBM25Rerank,
	)
	if opts.EnableBM25Rerank && len(rawHits) > 0 {
		cands := make([]bm25.Candidate, len(rawHits))
		for i, h := range rawHits {
			cands[i] = bm25.Candidate{Hit: h, Corpus: bm25.BuildCorpusText(h)}
		}
		results, bstats := bm25.Rerank(cands, intent)
		// Re-materialize rawHits in the new order; the per-hit Score now
		// carries BM25Score + HybridRank so downstream layers (citation,
		// density) keep the rerank fingerprint without re-running BM25.
		reordered := make([]types.Hit, len(results))
		for i, r := range results {
			reordered[i] = r.Hit
		}
		rawHits = reordered
		doneBM25(
			"candidates_out", bstats.CandidatesOut,
			"rank_changes", bstats.RankChanges,
			"top1_score_delta", bstats.Top1ScoreDelta,
			"top1_chunk_id", bstats.Top1ChunkID,
			"disabled", bstats.BM25Disabled,
		)
	} else {
		doneBM25(
			"candidates_out", len(rawHits),
			"rank_changes", 0,
			"top1_score_delta", 0.0,
			"top1_chunk_id", topChunkID(rawHits),
			"disabled", true,
		)
	}

	// Step 3: threshold drop. Sub-span query.threshold.drop — tunable
	// knob: --threshold. dropped count growing across runs means the
	// distribution shifted (rebuild needed or model drift).
	doneThreshold := e.fp.Span("query.threshold.drop",
		"trace_id", traceID,
		"threshold", threshold,
		"candidates_in", len(rawHits),
	)
	var passed []types.Hit
	for _, h := range rawHits {
		if threshold > 0 && h.Score.Normalized < threshold {
			continue
		}
		passed = append(passed, h)
	}
	doneThreshold(
		"candidates_out", len(passed),
		"dropped", len(rawHits)-len(passed),
	)
	if len(rawHits) > 0 && len(passed) == 0 {
		warnings = append(warnings, "all_results_below_threshold")
	}

	// Step 4: citation enforcement — drop any hit whose file we can't
	// resolve against the recorded src_root. Plan §5 + §7.4. Sub-span
	// query.citation.enforce — tunable knob: --src override. dropped /
	// stale counts feed the operator's reindex decision.
	srcRoot := opts.SrcRoot
	if srcRoot == "" {
		srcRoot = e.srcRoot
	}
	// Cheap stale detection (B4): pull the source tree's current HEAD
	// once and compare against each chunk's CommitHash. Mismatch is a
	// warning, not a drop — file content at the new commit is usually
	// still relevant. The git call here is the same one freshness uses;
	// we accept its 5-10ms cost on the query hot path because the
	// signal is high-value for the caller deciding whether to reindex.
	doneCitation := e.fp.Span("query.citation.enforce",
		"trace_id", traceID,
		"candidates_in", len(passed),
		"src_root", srcRoot,
	)
	currentHead := currentGitHead(srcRoot)
	enforced, droppedCitations, staleCitations := EnforceCitationsAt(passed, srcRoot, currentHead)
	doneCitation(
		"candidates_out", len(enforced),
		"dropped", droppedCitations,
		"stale", staleCitations,
	)
	if droppedCitations > 0 {
		warnings = append(warnings, fmt.Sprintf("dropped_%d_unverified_citations", droppedCitations))
	}
	if staleCitations > 0 {
		warnings = append(warnings, fmt.Sprintf("stale_%d_citations", staleCitations))
	}
	// Catastrophic citation failure: we had threshold-passing candidates
	// but lost every one to citation enforcement. Almost always means the
	// source tree was moved or deleted without rebuilding the index — the
	// remaining response would be empty and the cause non-obvious. Raise.
	if len(passed) > 0 && len(enforced) == 0 {
		return nil, fmt.Errorf("%w: dropped all %d candidate hits — source root may be missing or out of sync (rebuild with `ckv build --src <path>`)",
			ErrCitationNotFound, droppedCitations)
	}

	// Step 5: split tests out of primary hits when ExamplesK > 0.
	// We keep the same score-sorted order in both groups so the top of
	// each is the highest-similarity result of its kind.
	primary, examples := splitByTest(enforced, opts.ExamplesK > 0)

	// Step 6: trim each group to its respective limit *before* density
	// adjustment, so the budget only applies to the visible hits.
	if len(primary) > k {
		primary = primary[:k]
	}
	if opts.ExamplesK > 0 && len(examples) > opts.ExamplesK {
		examples = examples[:opts.ExamplesK]
	}

	// Step 7: snippet density. Combine primary + examples into a single
	// DensityAdjust call so the token budget is shared across both
	// groups (primary downgrades last because it's earlier in the slice).
	// Sub-span query.density.adjust — tunable knob: --budget-tokens.
	// tier_full / tier_sig5 / tier_sig_only distribution shows whether
	// the budget commonly bites; if mostly sig_only, raise budget or
	// trim K.
	combined := append(append([]types.Hit{}, primary...), examples...)
	doneDensity := e.fp.Span("query.density.adjust",
		"trace_id", traceID,
		"candidates_in", len(combined),
		"budget_tokens", budget,
		"max_density", string(opts.MaxDensity),
	)
	combinedHits, tokensUsed := DensityAdjustWith(combined, budget, opts.MaxDensity, opts.SignatureContextLines)
	tierFull, tierSig5, tierSigOnly := countTiers(combinedHits)
	doneDensity(
		"tokens_used", tokensUsed,
		"tier_full", tierFull,
		"tier_sig5", tierSig5,
		"tier_sig_only", tierSigOnly,
	)

	hits := combinedHits[:len(primary)]
	var exampleHits []Hit
	if opts.ExamplesK > 0 {
		exampleHits = combinedHits[len(primary):]
	}

	response := &Response{
		Hits:     hits,
		Examples: exampleHits,
		Warnings: warnings,
		Metadata: ResponseMetadata{
			TokensUsed:     tokensUsed,
			IndexedHeadCKV: e.man.IndexedHead,
			Fresh:          true, // freshness check arrives with `ckv freshness`
			TraceID:        traceID,
		},
	}
	doneSearch(
		"hits", len(hits),
		"examples", len(exampleHits),
		"citation_drops", droppedCitations,
		"warnings", warnings,
		"tokens_used", tokensUsed,
		"top_file", topFile(hits),
	)
	return response, nil
}

// splitByTest partitions hits into (primary, examples) by IsTest. When
// separateTests is false, every hit lands in primary and examples is
// nil — preserving the pre-FU-10 single-list behavior for callers that
// haven't opted in via Options.ExamplesK.
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

// topChunkID returns the first 12 hex chars of the top-ranked hit's
// chunk_id, or empty when none. Used as a sub-span fingerprint so
// log readers can spot drift between runs without dumping the full id.
func topChunkID(hits []types.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	id := hits[0].Chunk.ID
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// topScore returns the normalized score of the top-ranked store hit,
// or 0 when none. Logged as a sub-span field so threshold tuning has
// a concrete number to anchor on.
func topScore(hits []types.Hit) float64 {
	if len(hits) == 0 {
		return 0
	}
	return hits[0].Score.Normalized
}

// countTiers tallies the 3-tier density distribution across the
// response hits. Surfaced in the query.density.adjust footprint so
// operators can decide whether to raise --budget-tokens.
func countTiers(hits []Hit) (full, sig5, sigOnly int) {
	for _, h := range hits {
		switch h.Density {
		case DensityFull:
			full++
		case DensitySignature5:
			sig5++
		case DensitySignatureOnly:
			sigOnly++
		}
	}
	return full, sig5, sigOnly
}
