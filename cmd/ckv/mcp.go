package main

import (
	"errors"

	"github.com/spf13/cobra"
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
  cks.context.semantic_search
  cks.ops.get_freshness
  cks.ops.health

The combined cks-mcp binary (CKG+CKV+RRF) is produced by 'make build-cks'
once the CKG dependency is wired in S1-W3.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP(cmd, opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory")
	f.StringVar(&opts.httpAddr, "http", "", "optional HTTP listen addr (dev mode, loopback only)")

	return cmd
}

func runMCP(_ *cobra.Command, _ *mcpOpts) error {
	return errors.New("mcp: not yet implemented (S1-W3: MCP server)")
}
