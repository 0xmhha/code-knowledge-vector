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
	embedder    string // mock | bgeonnx
	modelDir    string // override default ~/.cache/ckv/models/<name>
	logLevel    string // debug | info | warn | error; empty → $CKV_LOG_LEVEL → info
	profile     string // path to write profile.json on Close (empty = disabled)
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
	cmd.PersistentFlags().StringVar(&globalFlags.embedder, "embedder", "mock",
		"embedder backend: mock (default, no deps) or bgeonnx (requires model + libonnxruntime)")
	cmd.PersistentFlags().StringVar(&globalFlags.modelDir, "model-dir", "",
		"override the model directory (default ~/.cache/ckv/models/<name>)")
	cmd.PersistentFlags().StringVar(&globalFlags.logLevel, "log-level", "",
		"slog minimum level: debug | info | warn | error (default info; falls back to $CKV_LOG_LEVEL)")
	cmd.PersistentFlags().StringVar(&globalFlags.profile, "profile", "",
		"write per-event latency profile (count + p50/p95/sum ms) to this path on exit; empty = disabled")

	cmd.AddCommand(
		newBuildCmd(),
		newReindexCmd(),
		newQueryCmd(),
		newMCPCmd(),
		newFreshnessCmd(),
		newModelCmd(),
		newEvalCmd(),
		newGlossaryCmd(),
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
