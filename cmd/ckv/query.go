package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

type queryOpts struct {
	out          string
	k            int
	examplesK    int
	lang         string
	pathGlob     string
	symbolKind   string
	commitHash   string
	budgetTokens int
	threshold    float64
	srcRoot      string
	traceID      string
	dryRun       bool
	aliasPath    string
	bm25Rerank   bool
	jsonOut      bool
}

func newQueryCmd() *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query <intent>",
		Short: "Run a semantic search over the vector index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(cmd.Context(), opts, args[0])
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory (vector.db, manifest.json)")
	f.IntVarP(&opts.k, "top", "k", query.DefaultK, "top-K primary (non-test) results")
	f.IntVar(&opts.examplesK, "examples", 0, "split out up to N test-file hits as Examples (0 = no separation; tests intermix with primary)")
	f.StringVar(&opts.lang, "lang", "", "filter by language (go|typescript|javascript|solidity|markdown)")
	f.StringVar(&opts.pathGlob, "path", "", "filter by path glob (filepath.Match, single-star)")
	f.StringVar(&opts.symbolKind, "kind", "", "filter by symbol kind (Function|Method|Type|Struct|Interface|DocSection|ADRSection)")
	f.StringVar(&opts.commitHash, "commit", "", "filter by commit_hash (incremental snapshot view; pin to a historical commit)")
	f.IntVar(&opts.budgetTokens, "budget-tokens", query.DefaultBudgetTokens, "token budget for snippet density")
	f.Float64Var(&opts.threshold, "threshold", query.DefaultThreshold, "min normalized score (<0 disables)")
	f.StringVar(&opts.srcRoot, "src", "", "source root used for citation verification (default: manifest.src_root)")
	f.StringVar(&opts.traceID, "trace-id", "", "correlation id echoed in response.metadata and footprint log (default: engine-generated from intent hash)")
	f.BoolVar(&opts.dryRun, "dry-run", false, "validate request shape only; skip embed + retrieval (response has metadata only)")
	f.StringVar(&opts.aliasPath, "alias", "", "vocabulary-bridge glossary YAML (korean/vague phrase → english code keywords); intent gets widened with matched keywords before embedding")
	f.BoolVar(&opts.bm25Rerank, "bm25-rerank", false, "experimental: rerank vector candidates with candidate-set BM25 + RRF fusion before threshold")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable output")

	return cmd
}

func runQuery(ctx context.Context, opts *queryOpts, intent string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	emb, cleanup, err := resolveEmbedder(globalFlags.embedder, globalFlags.modelDir)
	if err != nil {
		return err
	}
	defer cleanup()

	fp := newFootprint(opts.out, "")
	defer fp.Close()

	eng, err := query.Open(opts.out, emb, query.WithFootprint(fp))
	if err != nil {
		if errors.Is(err, query.ErrIndexUnavailable) {
			fmt.Fprintln(os.Stderr, "ckv:", err)
		}
		return err
	}
	defer eng.Close()

	filter := types.Filter{
		Language:   opts.lang,
		PathGlob:   opts.pathGlob,
		CommitHash: opts.commitHash,
	}
	if opts.symbolKind != "" {
		filter.SymbolKinds = []types.SymbolKind{types.SymbolKind(opts.symbolKind)}
	}

	aliases, err := query.LoadAliasMap(opts.aliasPath)
	if err != nil {
		return err
	}

	res, err := eng.Search(ctx, intent, query.Options{
		K:                opts.k,
		ExamplesK:        opts.examplesK,
		Filter:           filter,
		BudgetTokens:     opts.budgetTokens,
		Threshold:        opts.threshold,
		SrcRoot:          opts.srcRoot,
		TraceID:          opts.traceID,
		DryRun:           opts.dryRun,
		Aliases:          aliases,
		EnableBM25Rerank: opts.bm25Rerank,
	})
	if err != nil {
		return err
	}

	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	return renderHuman(res)
}

// renderHuman prints a compact tabular view that's still scannable
// in a terminal — one block per hit with citation, score, and snippet.
// When Examples are present (--examples > 0), they render as a second
// section so the reader can see "primary code" vs "usage examples" at
// a glance.
func renderHuman(res *query.Response) error {
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "ckv: warning:", w)
	}
	if len(res.Hits) == 0 && len(res.Examples) == 0 {
		fmt.Println("(no hits)")
		return nil
	}
	if len(res.Examples) > 0 {
		fmt.Println("Primary:")
	}
	renderHits(res.Hits)
	if len(res.Examples) > 0 {
		fmt.Println()
		fmt.Println("Examples (test files showing usage):")
		renderHits(res.Examples)
	}
	fmt.Fprintf(os.Stderr, "ckv: tokens_used=%d indexed_head=%s\n",
		res.Metadata.TokensUsed, res.Metadata.IndexedHeadCKV)
	return nil
}

func renderHits(hits []query.Hit) {
	for i, h := range hits {
		symbol := h.Symbol
		if symbol == "" {
			symbol = "(no symbol)"
		}
		fmt.Printf("[%d] %s:%d-%d  score=%.3f  rank=%d  %s/%s  %s\n",
			i+1, h.Citation.File, h.Citation.StartLine, h.Citation.EndLine,
			h.Score.Normalized, h.Score.VectorRank,
			h.Language, h.SymbolKind, symbol)
		// Indent the snippet so it's visually grouped under its header.
		for _, line := range splitLines(h.Snippet) {
			fmt.Println("    " + line)
		}
		if i < len(hits)-1 {
			fmt.Println()
		}
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
