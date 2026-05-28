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

Tools exposed:
  cks.context.semantic_search   — full search pipeline
  cks.context.embed             — text to vector
  cks.context.vector_search     — ANN search with pre-computed vector
  cks.context.rerank            — BM25 rerank candidate hits
  cks.context.related_changes   — PR breadcrumb lookup
  cks.ops.get_freshness         — git diff vs indexed_head
  cks.ops.health                — index identity probe
  cks.ops.warmup                — pre-load embedder

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
