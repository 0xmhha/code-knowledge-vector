// Package mcp wraps the CKV read-only surface as an MCP server.
//
// CKV exposes only **read-only** tools — those that *infer* meaning from
// the indexed code. The companion **read-write** memory MCP (working
// memory: remember_fact, record_decision, log_interaction) is a separate
// concern and will live in pkg/memory (planned). Splitting the two is
// deliberate so callers (coding agent, CKS) can mount the write surface
// behind tighter policy than the read surface.
//
// Tools exposed today (read-only, plan §8.2):
//   - cks.context.semantic_search  — query.Engine.Search wrapper
//   - cks.ops.get_freshness        — freshness.Check wrapper
//   - cks.ops.health               — embedder + index identity probe
//   - cks.ops.warmup               — pre-load embedder, report cold-start ms
//
// Transport is stdio by default (Claude Code default). This package is
// importable by **CKS** — CKS multiplexes CKV's tools alongside CKG's
// in its own combined `cks-mcp` binary. See plan-S1-ckv.md §7 for the
// (CKS-side) integration shape.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-vector/internal/footprint"
	"github.com/0xmhha/code-knowledge-vector/internal/freshness"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// ServerName / ServerVersion are surfaced to MCP clients on init.
const (
	ServerName    = "ckv"
	ServerVersion = "0.1.0-S1W3"
)

// ResponseSchemaVersion is the version tag every tool response carries
// in the top-level "schema_version" field. Bumps follow CKV-7:
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
const ResponseSchemaVersion = "1"

// Server owns the long-lived query engine + the underlying MCP server
// object. One Server per --out directory.
type Server struct {
	engine *query.Engine
	mcp    *server.MCPServer
	fp     *footprint.Logger
}

// Option customizes NewServer (functional options).
type Option func(*Server)

// WithFootprint attaches a footprint logger; each MCP tool dispatch is
// then wrapped in a span (event = "mcp.<tool>"). Nil-safe.
func WithFootprint(fp *footprint.Logger) Option {
	return func(s *Server) { s.fp = fp }
}

// NewServer constructs the MCP server bound to a pre-opened
// query.Engine. The caller owns the engine and is responsible for
// Close-ing it on shutdown.
//
// server.WithRecovery installs a panic-to-error middleware on every
// tool handler. Without it, a panic in handleSemanticSearch (nil
// engine, ranking math, snippet decode) would unwind past the MCP
// server, terminate ckv mcp, close the stdio writer, and surface on
// the cks side as "transport closed" — see CKV-4 in
// docs/followups-from-cks-dogfood-2026-05-19.md. With recovery the
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
	s.registerTools()
	return s
}

// ServeStdio runs the JSON-RPC stdio loop. Blocks until EOF on stdin
// or context cancellation. This is what `claude mcp add cks` invokes.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
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
	// carry the CKV-6 expansion: embedder status / provider / model_dir
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
// Every response carries a top-level "schema_version" field (CKV-7).
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
