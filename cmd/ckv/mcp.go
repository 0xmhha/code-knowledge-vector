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
		Long: `Speaks MCP JSON-RPC over stdio by default. Exposes:
  cks.context.semantic_search   — query.Search wrapper
  cks.ops.get_freshness         — git diff vs indexed_head
  cks.ops.health                — index identity probe

Register with Claude Code:
  claude mcp add cks --command "$(pwd)/bin/ckv mcp --out=$(pwd)/ckv-data"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP(opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory")
	f.StringVar(&opts.httpAddr, "http", "", "(reserved) HTTP listen addr — not yet wired")

	return cmd
}

func runMCP(opts *mcpOpts) error {
	if opts.httpAddr != "" {
		return errors.New("mcp: --http transport not yet implemented")
	}

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
	// ServeStdio blocks until stdin EOF or fatal transport error.
	// We deliberately log nothing on stdout — MCP stdio transport
	// reserves stdout for JSON-RPC frames.
	if err := srv.ServeStdio(); err != nil {
		return fmt.Errorf("mcp serve: %w", err)
	}
	return nil
}
