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
func NewServer(eng *query.Engine, opts ...Option) *Server {
	mcpSrv := server.NewMCPServer(
		ServerName,
		ServerVersion,
		server.WithToolCapabilities(true),
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
			mcpgo.Description("Filter by language: go | typescript | solidity | markdown."),
		),
		mcpgo.WithString("path",
			mcpgo.Description("Filter by path glob (filepath.Match, single-star)."),
		),
		mcpgo.WithString("symbol_kind",
			mcpgo.Description("Filter by symbol kind: Function | Method | Type | Struct | Interface | Contract | Event | Modifier | FileHeader | DocSection | ADRSection."),
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
	payload := map[string]any{
		"server":          ServerName,
		"server_version":  ServerVersion,
		"embedding_model": man.EmbeddingModel,
		"embedding_dim":   man.EmbeddingDim,
		"indexed_head":    man.IndexedHead,
		"chunk_count":     man.ChunkCount,
		"built_at":        man.BuiltAt,
		"src_root":        man.SrcRoot,
	}
	return jsonResult(payload)
}

// jsonResult marshals payload into a CallToolResult.Text. MCP tools
// today exchange strings; we use JSON so clients can parse structurally.
func jsonResult(payload any) (*mcpgo.CallToolResult, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpgo.NewToolResultText(string(data)), nil
}
