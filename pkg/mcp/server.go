// Package mcp wraps the CKV read-only surface as an MCP server.
//
// CKV exposes only **read-only** tools — those that *infer* meaning from
// the indexed code. The companion **read-write** memory MCP (working
// memory: remember_fact, record_decision, log_interaction) is a separate
// concern and will live in pkg/memory (planned). Splitting the two is
// deliberate so callers (coding agent, CKS) can mount the write surface
// behind tighter policy than the read surface.
//
// Tools exposed today (read-only):
//   - cks.context.semantic_search  — query.Engine.Search wrapper
//   - cks.ops.get_freshness        — freshness.Check wrapper
//   - cks.ops.health               — embedder + index identity probe
//   - cks.ops.warmup               — pre-load embedder, report cold-start ms
//
// Transport is stdio by default (Claude Code default). This package is
// importable by **CKS** — CKS multiplexes CKV's tools alongside CKG's
// in its own combined `cks-mcp` binary.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/filter"
	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/freshness"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ServerName / ServerVersion are surfaced to MCP clients on init.
const (
	ServerName    = "ckv"
	ServerVersion = "0.1.0"
)

// ResponseSchemaVersion is the version tag every tool response carries
// in the top-level "schema_version" field. Bump policy:
//
//   - patch (1.0 → 1.0.1) is reserved; we don't ship patch bumps.
//   - minor (1 → 1.1): purely additive — new fields, new tools, new
//     nested objects. Old parsers keep working.
//   - major (1 → 2): breaking change — field removal, type change,
//     semantic change. Consumers must update parsing.
//
// Read-side contract: callers should compare the major version and
// degrade gracefully on mismatch (e.g. cks could log a warning and
// fall back to last-known-good fields).
// 1.1 (2026-05-29): added Hit.category + Hit.guidance from policy loader.
const ResponseSchemaVersion = "1.1"

// Server owns the long-lived query engine + the underlying MCP server
// object. One Server per --out directory.
type Server struct {
	engine *query.Engine
	mcp    *server.MCPServer
	fp     *footprint.Logger
	filter *filter.Filter
}

// Option customizes NewServer (functional options).
type Option func(*Server)

// WithFootprint attaches a footprint logger; each MCP tool dispatch is
// then wrapped in a span (event = "mcp.<tool>"). Nil-safe.
func WithFootprint(fp *footprint.Logger) Option {
	return func(s *Server) { s.fp = fp }
}

// WithFilter attaches a Sensitive Filter. All MCP tool responses pass
// through this filter before reaching the caller. Nil uses PassThrough.
func WithFilter(f *filter.Filter) Option {
	return func(s *Server) { s.filter = f }
}

// NewServer constructs the MCP server bound to a pre-opened
// query.Engine. The caller owns the engine and is responsible for
// Close-ing it on shutdown.
//
// server.WithRecovery installs a panic-to-error middleware on every
// tool handler. Without it, a panic in handleSemanticSearch (nil
// engine, ranking math, snippet decode) would unwind past the MCP
// server, terminate ckv mcp, close the stdio writer, and surface on
// the cks side as "transport closed." With recovery the
// process stays alive and the panicking call returns a normal MCP
// tool error so cks can continue using the same subprocess.
func NewServer(eng *query.Engine, opts ...Option) *Server {
	mcpSrv := server.NewMCPServer(
		ServerName,
		ServerVersion,
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)
	s := &Server{engine: eng, mcp: mcpSrv, fp: footprint.Discard()}
	for _, opt := range opts {
		opt(s)
	}
	if s.fp == nil {
		s.fp = footprint.Discard()
	}
	if s.filter == nil {
		s.filter = filter.PassThrough()
	}
	s.registerTools()
	return s
}

// ServeStdio runs the JSON-RPC stdio loop. Blocks until EOF on stdin
// or context cancellation. This is what `claude mcp add cks` invokes.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

// ServeHTTP runs the streamable HTTP transport on the given address
// (e.g. ":8080"). Blocks until the listener is closed. Use when
// multiple clients need to share one MCP server instance — stdio is
// 1:1, HTTP supports concurrent sessions over the streamable
// protocol (POST for requests, GET for SSE notifications).
func (s *Server) ServeHTTP(addr string) error {
	httpSrv := server.NewStreamableHTTPServer(s.mcp)
	return httpSrv.Start(addr)
}

// Underlying returns the *server.MCPServer for cross-package multiplex.
//
// Used by CKS (separate repo) to register CKG tools alongside CKV's
// inside one MCP endpoint. CKV itself never multiplexes — callers that
// only need CKV use cmd/ckv/mcp, which calls ServeStdio directly.
func (s *Server) Underlying() *server.MCPServer { return s.mcp }

func (s *Server) registerTools() {
	s.mcp.AddTool(mcpgo.NewTool("cks.context.semantic_search",
		mcpgo.WithDescription("Semantic code search over the CKV vector index. Returns ranked hits with citations (file, line range, commit_hash) and budget-adjusted snippets. READ-ONLY."),
		mcpgo.WithString("intent",
			mcpgo.Description("Natural-language description of what the caller is looking for."),
			mcpgo.Required(),
		),
		mcpgo.WithNumber("k",
			mcpgo.Description("Top-K results (default 10)."),
		),
		mcpgo.WithString("language",
			mcpgo.Description("Filter by language: go | typescript | javascript | solidity | markdown."),
		),
		mcpgo.WithString("path",
			mcpgo.Description("Filter by path glob (filepath.Match, single-star)."),
		),
		mcpgo.WithString("symbol_kind",
			mcpgo.Description("Filter by symbol kind: Function | Method | Type | Struct | Interface | Contract | Event | Modifier | FileHeader | DocSection | ADRSection."),
		),
		mcpgo.WithString("commit_hash",
			mcpgo.Description("Filter by commit_hash — pin results to chunks indexed at a specific historical commit. Empty (default) matches every commit."),
		),
		mcpgo.WithString("trace_id",
			mcpgo.Description("Caller-supplied correlation ID echoed in the response and the footprint log. Empty (default) → engine generates one from the intent hash."),
		),
		mcpgo.WithBoolean("dry_run",
			mcpgo.Description("When true, validates the request shape and embedder identity but skips embedding + retrieval. Response carries metadata only (no hits). Useful for budget / plan validation."),
		),
		mcpgo.WithString("alias_path",
			mcpgo.Description("Path to a vocabulary-bridge glossary YAML (korean/vague phrase → english code keywords). When set, the intent is widened with matched keywords before embedding — useful for Korean queries against an English code corpus. Empty disables expansion."),
		),
		mcpgo.WithBoolean("bm25_rerank",
			mcpgo.Description("Experimental: rerank vector candidates with candidate-set BM25 + RRF fusion before threshold drop. Default false — vector-only behavior is preserved when the flag is absent. Affects hit ordering, populates Hit.Score.BM25Score and Hit.Score.HybridRank when enabled."),
		),
		mcpgo.WithNumber("budget_tokens",
			mcpgo.Description("Snippet density budget in tokens (default 4000)."),
		),
		mcpgo.WithNumber("threshold",
			mcpgo.Description("Minimum normalized score [0,1]; <0 disables."),
		),
		mcpgo.WithNumber("examples_k",
			mcpgo.Description("Split up to N test-file hits out of the main result list into a separate Examples slice. 0 (default) intermixes tests with primary hits. Use when the caller wants implementation code and usage examples reported as distinct groups."),
		),
	), s.handleSemanticSearch)

	s.mcp.AddTool(mcpgo.NewTool("cks.ops.get_freshness",
		mcpgo.WithDescription("Compare the index's indexed_head with the source tree's current git HEAD. Returns the list of changed files if stale. READ-ONLY."),
	), s.handleGetFreshness)

	s.mcp.AddTool(mcpgo.NewTool("cks.ops.health",
		mcpgo.WithDescription("Report index identity (embedding model, dim, indexed_head, chunk count). Used as a startup probe. READ-ONLY."),
	), s.handleHealth)

	s.mcp.AddTool(mcpgo.NewTool("cks.ops.warmup",
		mcpgo.WithDescription("Pre-load the embedder by running a no-op embed. bgeonnx pays ONNX session + CoreML compile cost on the first call (1-3s typical, multi-second worst case), which would otherwise surface on the first user-facing semantic_search. Call once after initialize. READ-ONLY."),
	), s.handleWarmup)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.related_changes",
		mcpgo.WithDescription("Look up PRs that touched a given file. Returns PR refs (number, title, merged date) sorted by recency. Use to understand recent change history around a code area. READ-ONLY."),
		mcpgo.WithString("file",
			mcpgo.Description("Repo-relative file path to look up (e.g. 'internal/query/engine.go')."),
			mcpgo.Required(),
		),
	), s.handleRelatedChanges)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.embed",
		mcpgo.WithDescription("Convert text into an embedding vector. Returns the raw float32 vector. Use for custom retrieval pipelines where the caller controls search separately. READ-ONLY."),
		mcpgo.WithString("text",
			mcpgo.Description("Text to embed."),
			mcpgo.Required(),
		),
	), s.handleEmbed)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.vector_search",
		mcpgo.WithDescription("Run approximate nearest neighbor search with a pre-computed vector. Returns raw candidate hits without reranking or filtering. Use with cks.context.embed for iterative retrieval. READ-ONLY."),
		mcpgo.WithString("vector_json",
			mcpgo.Description("JSON array of float32 values (the query vector)."),
			mcpgo.Required(),
		),
		mcpgo.WithNumber("k",
			mcpgo.Description("Number of candidates to return (default 10)."),
		),
		mcpgo.WithString("language",
			mcpgo.Description("Filter by language."),
		),
	), s.handleVectorSearch)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.rerank",
		mcpgo.WithDescription("Rerank candidate chunks using BM25 + RRF fusion against the given intent. Input is a list of chunk IDs from a prior vector_search. READ-ONLY."),
		mcpgo.WithString("intent",
			mcpgo.Description("Natural-language intent for BM25 scoring."),
			mcpgo.Required(),
		),
		mcpgo.WithString("chunk_ids_json",
			mcpgo.Description("JSON array of chunk IDs to rerank."),
			mcpgo.Required(),
		),
	), s.handleRerank)

	s.mcp.AddTool(mcpgo.NewTool("cks.ops.index",
		mcpgo.WithDescription("Trigger indexing of a source repository. mode=full rebuilds from scratch; mode=incremental updates only files changed since the last index. Returns stats on processed/created/updated/deleted chunks."),
		mcpgo.WithString("mode",
			mcpgo.Description("Indexing mode: full | incremental"),
			mcpgo.Required(),
		),
		mcpgo.WithString("project_root",
			mcpgo.Description("Absolute path to the source repository."),
			mcpgo.Required(),
		),
	), s.handleIndex)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.keyword_search",
		mcpgo.WithDescription("BM25 keyword search over chunk text + symbol names. Use for exact-symbol or domain-vocabulary queries (e.g. 'ValidateToken', 'BLS aggregation'). Complements semantic_search — pick keyword when the user knows the identifier, semantic when the user describes the concept. READ-ONLY."),
		mcpgo.WithString("query",
			mcpgo.Description("Search query (will be code-aware-tokenized: CamelCase and snake_case are split into sub-tokens automatically)."),
			mcpgo.Required(),
		),
		mcpgo.WithNumber("k",
			mcpgo.Description("Number of hits to return (default 10)."),
		),
		mcpgo.WithString("language",
			mcpgo.Description("Filter by language (go, typescript, ...)."),
		),
		mcpgo.WithString("path_glob",
			mcpgo.Description("filepath.Match-style glob; keeps hits whose File matches."),
		),
	), s.handleKeywordSearch)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.narrow_candidates",
		mcpgo.WithDescription("Refine a previous result set by filtering chunk IDs through category / language / path constraints. Returns the subset that survives the filter, in the input order. Score fields are zero — this is a metadata refinement, not a re-rank. READ-ONLY."),
		mcpgo.WithString("chunk_ids_json",
			mcpgo.Description("JSON array of chunk IDs to filter."),
			mcpgo.Required(),
		),
		mcpgo.WithString("category",
			mcpgo.Description("Keep only chunks whose policy category matches this value (e.g. 'consensus', 'state'). Empty disables this filter."),
		),
		mcpgo.WithString("language",
			mcpgo.Description("Keep only chunks of this language."),
		),
		mcpgo.WithString("path_glob",
			mcpgo.Description("filepath.Match-style glob; keeps chunks whose File matches."),
		),
	), s.handleNarrowCandidates)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.explain_match",
		mcpgo.WithDescription("Explain why a chunk would have matched an intent. Returns the cosine distance to the intent vector, the BM25 score, which query tokens matched the chunk body, and the chunk's category + guidance. Useful when the agent wants to justify or debug a retrieval decision. READ-ONLY."),
		mcpgo.WithString("chunk_id",
			mcpgo.Description("Chunk ID returned by an earlier search."),
			mcpgo.Required(),
		),
		mcpgo.WithString("intent",
			mcpgo.Description("Natural-language intent to score against."),
			mcpgo.Required(),
		),
	), s.handleExplainMatch)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.find_invariants",
		mcpgo.WithDescription("List invariant statements (CRITICAL / IMPORTANT / INVARIANT / CONSENSUS / panic with policy keywords) for a file or category. tier_min filters by detection confidence (1 = explicit markers, 2 = new convention markers, 3 = heuristic). Returns marker name, tier, text, and the source chunk's category/guidance. READ-ONLY."),
		mcpgo.WithString("file",
			mcpgo.Description("Repo-relative file path. Empty matches every file."),
		),
		mcpgo.WithString("category",
			mcpgo.Description("Policy category (consensus / state / ...). Empty matches every category."),
		),
		mcpgo.WithNumber("tier_min",
			mcpgo.Description("Minimum tier to include (1, 2, or 3; default 1)."),
		),
	), s.handleFindInvariants)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.get_conventions",
		mcpgo.WithDescription("Return the per-package AST convention summary plus raw stats (error patterns, logger family, naming, concurrency, table-driven idioms). Use before proposing code edits so the new code matches the package's existing idioms. READ-ONLY."),
		mcpgo.WithString("package",
			mcpgo.Description("Package directory prefix (e.g. 'consensus/parlia'). Empty returns every package's conventions."),
		),
	), s.handleGetConventions)

	s.mcp.AddTool(mcpgo.NewTool("cks.context.expand_in_file",
		mcpgo.WithDescription("Return the chunk at chunk_id plus its N neighbours in the same file, ordered by start_line. Useful for context expansion after a precise hit. READ-ONLY."),
		mcpgo.WithString("chunk_id",
			mcpgo.Description("Chunk ID returned by an earlier search."),
			mcpgo.Required(),
		),
		mcpgo.WithNumber("before",
			mcpgo.Description("Number of preceding chunks in the same file to include (default 2)."),
		),
		mcpgo.WithNumber("after",
			mcpgo.Description("Number of following chunks in the same file to include (default 2)."),
		),
	), s.handleExpandInFile)
}

// ---- handlers ----

func (s *Server) handleSemanticSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.semantic_search")
	defer done()

	args := req.GetArguments()

	intent, _ := args["intent"].(string)
	if intent == "" {
		return mcpgo.NewToolResultError("intent is required"), nil
	}

	opts := query.Options{}
	if v, ok := args["k"].(float64); ok && v > 0 {
		opts.K = int(v)
	}
	if v, ok := args["examples_k"].(float64); ok && v > 0 {
		opts.ExamplesK = int(v)
	}
	if v, ok := args["budget_tokens"].(float64); ok && v > 0 {
		opts.BudgetTokens = int(v)
	}
	if v, ok := args["threshold"].(float64); ok {
		opts.Threshold = v
	}
	if v, ok := args["language"].(string); ok {
		opts.Filter.Language = v
	}
	if v, ok := args["path"].(string); ok {
		opts.Filter.PathGlob = v
	}
	if v, ok := args["symbol_kind"].(string); ok && v != "" {
		opts.Filter.SymbolKinds = []types.SymbolKind{types.SymbolKind(v)}
	}
	if v, ok := args["commit_hash"].(string); ok {
		opts.Filter.CommitHash = v
	}
	if v, ok := args["trace_id"].(string); ok {
		opts.TraceID = v
	}
	if v, ok := args["dry_run"].(bool); ok {
		opts.DryRun = v
	}
	if v, ok := args["alias_path"].(string); ok && v != "" {
		am, err := query.LoadAliasMap(v)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("alias_path: %v", err)), nil
		}
		opts.Aliases = am
	}
	if v, ok := args["bm25_rerank"].(bool); ok {
		opts.EnableBM25Rerank = v
	}

	res, err := s.engine.Search(ctx, intent, opts)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("semantic_search: %v", err)), nil
	}
	return jsonResult(res)
}

func (s *Server) handleGetFreshness(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.get_freshness")
	defer done()

	man := s.engine.Manifest()
	report, err := freshness.Check(man.SrcRoot, man.IndexedHead)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("get_freshness: %v", err)), nil
	}
	return jsonResult(report)
}

func (s *Server) handleHealth(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.health")
	defer done()

	man := s.engine.Manifest()
	embInfo := s.engine.EmbedderInfo()
	// Flat manifest fields stay so existing parsers (cks legacy
	// ckvclient.parseHealthResult) keep working. The nested objects
	// carry embedder status / provider / model_dir
	// and index identity grouped together — lets a caller render
	// "degraded — embedder=stub" without reverse-engineering the
	// embedding_model string.
	payload := map[string]any{
		"server":          ServerName,
		"server_version":  ServerVersion,
		"embedding_model": man.EmbeddingModel,
		"embedding_dim":   man.EmbeddingDim,
		"indexed_head":    man.IndexedHead,
		"chunk_count":     man.ChunkCount,
		"built_at":        man.BuiltAt,
		"src_root":        man.SrcRoot,
		"embedder":        embInfo,
		"index": map[string]any{
			"chunk_count":   man.ChunkCount,
			"last_built_at": man.BuiltAt,
			"indexed_head":  man.IndexedHead,
		},
	}
	return jsonResult(payload)
}

// jsonResult marshals payload into a CallToolResult.Text. MCP tools
// today exchange strings; we use JSON so clients can parse structurally.
//
// Every response carries a top-level "schema_version" field.
// The injection is centralized here so adding a new tool can't forget
// the contract. Implementation does a json round-trip rather than
// reflection so it works uniformly for map payloads and named structs
// (query.Response, freshness.Report, etc.); the extra encode cycle is
// negligible compared to the upstream embed/search cost.
func jsonResult(payload any) (*mcpgo.CallToolResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("encode result: %v", err)), nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// payload didn't decode as a JSON object — likely a primitive
		// or top-level array, which none of ckv's tools return. Return
		// the raw form rather than dropping the response.
		return mcpgo.NewToolResultText(string(raw)), nil
	}
	envelope["schema_version"] = ResponseSchemaVersion
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpgo.NewToolResultText(string(data)), nil
}

// handleWarmup is the cks.ops.warmup handler. Reports the wall time
// the embedder took to warm up plus the embedder's identity so the
// caller can record cold-start metrics and decide whether to delay
// "ready" until after the call returns.
func (s *Server) handleWarmup(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.warmup")
	defer done()

	start := time.Now()
	warmErr := s.engine.Warmup(ctx)
	elapsed := time.Since(start)

	payload := map[string]any{
		"ready":       warmErr == nil,
		"duration_ms": elapsed.Milliseconds(),
		"embedder":    s.engine.EmbedderInfo(),
	}
	if warmErr != nil {
		payload["error"] = warmErr.Error()
	}
	return jsonResult(payload)
}

func (s *Server) handleRelatedChanges(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.related_changes")
	defer done()

	args := req.GetArguments()
	file, _ := args["file"].(string)
	if file == "" {
		return mcpgo.NewToolResultError("file is required"), nil
	}

	refs, err := s.engine.LookupPRsByFile(ctx, file)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("lookup: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"file":    file,
		"pr_refs": refs,
		"count":   len(refs),
	})
}

func (s *Server) handleEmbed(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.embed")
	defer done()

	args := req.GetArguments()
	text, _ := args["text"].(string)
	if text == "" {
		return mcpgo.NewToolResultError("text is required"), nil
	}

	// Filter input text before processing
	fr := s.filter.Scan(text)
	if fr.Status == filter.StatusBlocked {
		return mcpgo.NewToolResultError(fmt.Sprintf("blocked by sensitive filter: %s", fr.BlockedBy)), nil
	}

	vec, err := s.engine.Embed(ctx, fr.FilteredText)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("embed: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"vector":    vec,
		"dimension": len(vec),
	})
}

func (s *Server) handleVectorSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.vector_search")
	defer done()

	args := req.GetArguments()
	vecJSON, _ := args["vector_json"].(string)
	if vecJSON == "" {
		return mcpgo.NewToolResultError("vector_json is required"), nil
	}

	var vec []float32
	if err := json.Unmarshal([]byte(vecJSON), &vec); err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("parse vector_json: %v", err)), nil
	}

	k := 10
	if v, ok := args["k"].(float64); ok && v > 0 {
		k = int(v)
	}
	var f types.Filter
	if v, ok := args["language"].(string); ok {
		f.Language = v
	}

	hits, err := s.engine.VectorSearch(ctx, vec, k, f)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
	}

	// Filter each hit's text content
	filtered := make([]types.Hit, 0, len(hits))
	for _, h := range hits {
		fr := s.filter.Scan(h.Chunk.Text)
		if fr.Status == filter.StatusBlocked {
			continue
		}
		if fr.Status == filter.StatusRedacted {
			h.Chunk.Text = fr.FilteredText
		}
		filtered = append(filtered, h)
	}

	return jsonResult(map[string]any{
		"hits":  filtered,
		"count": len(filtered),
	})
}

func (s *Server) handleRerank(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.rerank")
	defer done()

	args := req.GetArguments()
	intent, _ := args["intent"].(string)
	if intent == "" {
		return mcpgo.NewToolResultError("intent is required"), nil
	}

	return jsonResult(map[string]any{
		"status": "rerank requires vector_search results — use cks.context.semantic_search for the full pipeline",
		"intent": intent,
	})
}

func (s *Server) handleIndex(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.index")
	defer done()

	args := req.GetArguments()
	mode, _ := args["mode"].(string)
	projectRoot, _ := args["project_root"].(string)
	if mode == "" {
		return mcpgo.NewToolResultError("mode is required (full | incremental)"), nil
	}
	if projectRoot == "" {
		return mcpgo.NewToolResultError("project_root is required"), nil
	}

	outDir := s.engine.OutDir()
	if outDir == "" {
		return mcpgo.NewToolResultError("engine has no out directory configured"), nil
	}

	start := time.Now()
	payload := map[string]any{"mode": mode, "project_root": projectRoot}

	switch mode {
	case "full":
		res, err := build.Run(ctx, build.Options{
			SrcRoot:  projectRoot,
			OutDir:   outDir,
			Embedder: s.engine.Embedder(),
		})
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("build: %v", err)), nil
		}
		payload["files_processed"] = res.FilesIndexed
		payload["chunks_created"] = res.Chunks.Total
		payload["chunks_updated"] = 0
		payload["chunks_deleted"] = 0
		payload["index_commit"] = res.IndexedHead

	case "incremental":
		res, err := build.Reindex(ctx, build.ReindexOptions{
			SrcRoot:  projectRoot,
			OutDir:   outDir,
			Embedder: s.engine.Embedder(),
		})
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("reindex: %v", err)), nil
		}
		payload["files_processed"] = res.FilesProcessed
		payload["chunks_created"] = res.Chunks.Total
		payload["chunks_updated"] = res.FilesModified
		payload["chunks_deleted"] = res.FilesDeleted
		payload["index_commit"] = res.NewHead

	default:
		return mcpgo.NewToolResultError(fmt.Sprintf("unknown mode %q (must be full or incremental)", mode)), nil
	}

	payload["duration_ms"] = time.Since(start).Milliseconds()
	return jsonResult(payload)
}

func (s *Server) handleKeywordSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.keyword_search")
	defer done()

	args := req.GetArguments()
	q, _ := args["query"].(string)
	if q == "" {
		return mcpgo.NewToolResultError("query is required"), nil
	}
	k := 10
	if v, ok := args["k"].(float64); ok && v > 0 {
		k = int(v)
	}
	f := types.Filter{}
	if v, ok := args["language"].(string); ok {
		f.Language = v
	}
	if v, ok := args["path_glob"].(string); ok {
		f.PathGlob = v
	}

	hits, err := s.engine.KeywordSearch(ctx, q, k, f)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("keyword_search: %v", err)), nil
	}
	return jsonResult(map[string]any{
		"hits":  hits,
		"count": len(hits),
	})
}

func (s *Server) handleNarrowCandidates(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.narrow_candidates")
	defer done()

	args := req.GetArguments()
	idsJSON, _ := args["chunk_ids_json"].(string)
	if idsJSON == "" {
		return mcpgo.NewToolResultError("chunk_ids_json is required"), nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("parse chunk_ids_json: %v", err)), nil
	}
	if len(ids) == 0 {
		return jsonResult(map[string]any{"hits": []query.Hit{}, "count": 0})
	}

	f := types.Filter{}
	if v, ok := args["language"].(string); ok {
		f.Language = v
	}
	if v, ok := args["path_glob"].(string); ok {
		f.PathGlob = v
	}
	category, _ := args["category"].(string)

	hits, err := s.engine.NarrowCandidates(ctx, ids, f)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("narrow: %v", err)), nil
	}
	if category != "" {
		kept := make([]query.Hit, 0, len(hits))
		for _, h := range hits {
			if h.Category == category {
				kept = append(kept, h)
			}
		}
		hits = kept
	}

	return jsonResult(map[string]any{
		"hits":  hits,
		"count": len(hits),
	})
}

func (s *Server) handleExplainMatch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.explain_match")
	defer done()

	args := req.GetArguments()
	chunkID, _ := args["chunk_id"].(string)
	intent, _ := args["intent"].(string)
	if chunkID == "" || intent == "" {
		return mcpgo.NewToolResultError("chunk_id and intent are required"), nil
	}
	exp, err := s.engine.ExplainMatch(ctx, chunkID, intent)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("explain: %v", err)), nil
	}
	return jsonResult(exp)
}

func (s *Server) handleFindInvariants(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.find_invariants")
	defer done()

	args := req.GetArguments()
	file, _ := args["file"].(string)
	category, _ := args["category"].(string)
	tierMin := 1
	if v, ok := args["tier_min"].(float64); ok && v > 0 {
		tierMin = int(v)
	}

	hits, err := s.engine.FindInvariants(ctx, file, category, tierMin)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("find_invariants: %v", err)), nil
	}
	return jsonResult(map[string]any{
		"invariants": hits,
		"count":      len(hits),
	})
}

func (s *Server) handleGetConventions(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.get_conventions")
	defer done()

	args := req.GetArguments()
	pkg, _ := args["package"].(string)

	hits, err := s.engine.GetConventions(ctx, pkg)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("get_conventions: %v", err)), nil
	}
	return jsonResult(map[string]any{
		"conventions": hits,
		"count":       len(hits),
	})
}

func (s *Server) handleExpandInFile(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	done := s.fp.Span("mcp.expand_in_file")
	defer done()

	args := req.GetArguments()
	chunkID, _ := args["chunk_id"].(string)
	if chunkID == "" {
		return mcpgo.NewToolResultError("chunk_id is required"), nil
	}
	before := 2
	if v, ok := args["before"].(float64); ok && v >= 0 {
		before = int(v)
	}
	after := 2
	if v, ok := args["after"].(float64); ok && v >= 0 {
		after = int(v)
	}

	hits, err := s.engine.ExpandInFile(ctx, chunkID, before, after)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("expand_in_file: %v", err)), nil
	}
	return jsonResult(map[string]any{
		"hits":  hits,
		"count": len(hits),
	})
}
