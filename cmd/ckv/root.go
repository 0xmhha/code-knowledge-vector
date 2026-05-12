package main

import (
	"github.com/spf13/cobra"
)

// Version is set via -ldflags at build time. Default "dev" works for `go run`.
var Version = "dev"

// rootFlags holds CLI flags that apply to every subcommand. They are
// set via PersistentFlags so any leaf command can read them.
type rootFlags struct {
	noFootprint bool
}

var globalFlags rootFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ckv",
		Short:         "Code Knowledge Vector — semantic code retrieval over your repo",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	cmd.PersistentFlags().BoolVar(&globalFlags.noFootprint, "no-footprint", false,
		"disable the JSONL footprint log written to <out>/footprint.jsonl")

	cmd.AddCommand(
		newBuildCmd(),
		newQueryCmd(),
		newMCPCmd(),
		newFreshnessCmd(),
		newModelCmd(),
	)

	return cmd
}

const rootLong = `ckv indexes a code repository as embedding vectors and serves semantic
search over CLI and MCP. Designed to be paired with code-knowledge-graph (CKG)
for hybrid retrieval.

Quickstart:
  ckv build --src=. --out=./ckv-data
  ckv query "connection pool initialization"
  ckv mcp
`
