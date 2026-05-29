package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
	ckvmcp "github.com/0xmhha/code-knowledge-vector/pkg/mcp"
)

type mcpOpts struct {
	out      string
	httpAddr string
}

func newMCPCmd() *cobra.Command {
	opts := &mcpOpts{}

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the CKV MCP server (stdio JSON-RPC)",
		Long: `Speaks MCP JSON-RPC over stdio by default, or HTTP when --http is set.

Tools exposed (15):

  Search:
    cks.context.semantic_search    — full retrieval pipeline (embed → vector → rerank → boost → enrich)
    cks.context.keyword_search     — BM25 keyword search for exact symbol / domain vocabulary
    cks.context.vector_search      — ANN with a pre-computed vector

  Refinement:
    cks.context.narrow_candidates  — filter a hit set by category / language / path
    cks.context.expand_in_file     — return neighbouring chunks of a hit

  Meta:
    cks.context.find_invariants    — list CRITICAL / INVARIANT / panic-heuristic invariants
    cks.context.get_conventions    — per-package AST stats (errors, loggers, naming, concurrency)
    cks.context.explain_match      — explain why a chunk matched (vector + BM25 + matched tokens)

  Helpers:
    cks.context.embed              — text → vector
    cks.context.rerank             — BM25 + RRF rerank on candidate IDs
    cks.context.related_changes    — PR breadcrumbs by file

  Operations:
    cks.ops.health                 — index identity probe
    cks.ops.get_freshness          — git diff vs indexed_head
    cks.ops.warmup                 — pre-load embedder
    cks.ops.index                  — trigger full / incremental rebuild

Full schema reference: docs/mcp-tools.md.

Register with Claude Code (stdio):
  claude mcp add cks --command "$(pwd)/bin/ckv mcp --out=$(pwd)/ckv-data"

Run as HTTP server (multi-client):
  ckv mcp --out=$(pwd)/ckv-data --http=:8080`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP(opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory")
	f.StringVar(&opts.httpAddr, "http", "", "HTTP listen address (e.g. :8080); empty uses stdio")

	return cmd
}

func runMCP(opts *mcpOpts) error {
	fp := newFootprint(opts.out, "")
	defer fp.Close()

	emb, cleanup, err := resolveEmbedder(globalFlags.embedder, globalFlags.modelDir)
	if err != nil {
		return err
	}
	defer cleanup()

	eng, err := query.Open(opts.out, emb, query.WithFootprint(fp))
	if err != nil {
		if errors.Is(err, query.ErrIndexUnavailable) {
			fmt.Fprintln(os.Stderr, "ckv mcp:", err)
		}
		return err
	}
	defer eng.Close()

	srv := ckvmcp.NewServer(eng, ckvmcp.WithFootprint(fp))

	if opts.httpAddr != "" {
		fmt.Fprintf(os.Stderr, "ckv mcp: HTTP listening on %s\n", opts.httpAddr)
		if err := srv.ServeHTTP(opts.httpAddr); err != nil {
			return fmt.Errorf("mcp serve http: %w", err)
		}
		return nil
	}

	// stdio default: reserves stdout for JSON-RPC frames, so logs only to stderr.
	if err := srv.ServeStdio(); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}
