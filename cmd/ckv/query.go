package main

import (
	"errors"

	"github.com/spf13/cobra"
)

type queryOpts struct {
	out          string
	k            int
	lang         string
	pathGlob     string
	symbolKind   string
	budgetTokens int
	jsonOut      bool
}

func newQueryCmd() *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query <intent>",
		Short: "Run a semantic search over the vector index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd, opts, args[0])
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory (vector.db, manifest.json)")
	f.IntVarP(&opts.k, "top", "k", 10, "top-K results")
	f.StringVar(&opts.lang, "lang", "", "filter by language (go|typescript|solidity)")
	f.StringVar(&opts.pathGlob, "path", "", "filter by path glob")
	f.StringVar(&opts.symbolKind, "kind", "", "filter by symbol kind (Function|Method|Type|...)")
	f.IntVar(&opts.budgetTokens, "budget-tokens", 4000, "token budget for snippet density")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable output")

	return cmd
}

func runQuery(_ *cobra.Command, _ *queryOpts, _ string) error {
	return errors.New("query: not yet implemented (S1-W3: query engine)")
}
