package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

type queryOpts struct {
	out          string
	k            int
	lang         string
	pathGlob     string
	symbolKind   string
	budgetTokens int
	threshold    float64
	srcRoot      string
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
	f.IntVarP(&opts.k, "top", "k", query.DefaultK, "top-K results")
	f.StringVar(&opts.lang, "lang", "", "filter by language (go|typescript|solidity)")
	f.StringVar(&opts.pathGlob, "path", "", "filter by path glob (filepath.Match, single-star)")
	f.StringVar(&opts.symbolKind, "kind", "", "filter by symbol kind (Function|Method|Type|Struct|Interface)")
	f.IntVar(&opts.budgetTokens, "budget-tokens", query.DefaultBudgetTokens, "token budget for snippet density")
	f.Float64Var(&opts.threshold, "threshold", query.DefaultThreshold, "min normalized score (<0 disables)")
	f.StringVar(&opts.srcRoot, "src", "", "source root used for citation verification (default: manifest.src_root)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable output")

	return cmd
}

func runQuery(ctx context.Context, opts *queryOpts, intent string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Mock embedder for W3; swap to bgeonnx in W3-late or W4.
	emb := mock.Default()

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
		Language: opts.lang,
		PathGlob: opts.pathGlob,
	}
	if opts.symbolKind != "" {
		filter.SymbolKinds = []types.SymbolKind{types.SymbolKind(opts.symbolKind)}
	}

	res, err := eng.Search(ctx, intent, query.Options{
		K:            opts.k,
		Filter:       filter,
		BudgetTokens: opts.budgetTokens,
		Threshold:    opts.threshold,
		SrcRoot:      opts.srcRoot,
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
func renderHuman(res *query.Response) error {
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "ckv: warning:", w)
	}
	if len(res.Hits) == 0 {
		fmt.Println("(no hits)")
		return nil
	}
	for i, h := range res.Hits {
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
		if i < len(res.Hits)-1 {
			fmt.Println()
		}
	}
	fmt.Fprintf(os.Stderr, "ckv: tokens_used=%d indexed_head=%s\n",
		res.Metadata.TokensUsed, res.Metadata.IndexedHeadCKV)
	return nil
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
