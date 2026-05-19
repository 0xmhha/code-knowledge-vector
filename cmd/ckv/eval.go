package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/eval"
	"github.com/0xmhha/code-knowledge-vector/internal/eval/prregress"
	"github.com/0xmhha/code-knowledge-vector/internal/judge"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

type evalOpts struct {
	out         string
	fixturePath string
	k           int
	threshold   float64
	minRecall5  float64 // exit non-zero if recall@5 < this
	judgeCmd    string  // empty → no judge; "claude" → invoke Claude Code CLI
	judgeModel  string  // optional --model passthrough
	jsonOut     bool

	// PR-regression mode (mutually exclusive with --fixture queries).
	prFixturePath string // path to testdata/prs.yaml — switches eval into PR-regression mode
	prTopK        int    // hints passed to the planning agent
}

func newEvalCmd() *cobra.Command {
	opts := &evalOpts{}
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Score the vector index against a known-query fixture",
		Long: `Loads a YAML fixture of (intent → expected file:line) pairs, runs each
intent through ckv query, and reports recall@k, MRR, and citation
accuracy. Exit code is non-zero when recall@5 falls below --min-recall5
so CI can gate on retrieval regressions.

Default fixture path: ./testdata/queries.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEval(cmd.Context(), opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory (vector.db, manifest.json)")
	f.StringVar(&opts.fixturePath, "fixture", "./testdata/queries.yaml", "path to fixture YAML")
	f.IntVarP(&opts.k, "top", "k", eval.DefaultK, "top-K used for recall counting")
	f.Float64Var(&opts.threshold, "threshold", -1, "query threshold (default -1: disabled for eval)")
	f.Float64Var(&opts.minRecall5, "min-recall5", 0.0, "fail with exit 1 if recall@5 < this")
	f.StringVar(&opts.judgeCmd, "judge", "", "LLM-as-judge command (empty=disabled; e.g. 'claude' for Claude Code CLI)")
	f.StringVar(&opts.judgeModel, "judge-model", "", "model passed to the judge CLI (--model)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable output")
	f.StringVar(&opts.prFixturePath, "pr-fixture", "", "path to PR fixture YAML (switches into PR-regression mode; mutually exclusive with --fixture)")
	f.IntVar(&opts.prTopK, "pr-top", 10, "top-K hints passed to the planning agent in PR-regression mode")
	return cmd
}

func runEval(ctx context.Context, opts *evalOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.prFixturePath != "" {
		return runPREval(ctx, opts)
	}
	fx, err := eval.LoadFixture(opts.fixturePath)
	if err != nil {
		return err
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
			fmt.Fprintln(os.Stderr, "ckv eval:", err)
		}
		return err
	}
	defer eng.Close()

	evalOpts := eval.Options{
		K:         opts.k,
		Threshold: opts.threshold,
	}
	if opts.judgeCmd != "" {
		evalOpts.Judge = &judge.ClaudeCLI{
			Binary: opts.judgeCmd,
			Model:  opts.judgeModel,
		}
	}
	res, err := eval.Run(ctx, eng, fx, evalOpts)
	if err != nil {
		return err
	}
	res.Fixture = opts.fixturePath

	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else {
		renderEvalHuman(res)
	}

	// CI gate: fail the binary if recall@5 < threshold.
	if res.Aggregate.RecallAt5 < opts.minRecall5 {
		return fmt.Errorf("ckv eval: recall@5=%.3f < --min-recall5=%.3f",
			res.Aggregate.RecallAt5, opts.minRecall5)
	}
	return nil
}

func renderEvalHuman(res *eval.Result) {
	a := res.Aggregate
	fmt.Printf("ckv eval — fixture=%s k=%d\n", res.Fixture, res.K)
	fmt.Println()
	fmt.Println("Per-query:")
	for _, p := range res.PerQuery {
		rank := "—"
		if p.FoundRank > 0 {
			rank = fmt.Sprintf("%d", p.FoundRank)
		}
		fmt.Printf("  %-6s rank=%-3s top=%s (score=%.3f)  intent=%q\n",
			p.QueryID, rank, p.TopHitFile, p.TopHitScore, p.Intent)
	}
	fmt.Println()
	fmt.Println("Aggregate:")
	fmt.Printf("  total       %d\n", a.Total)
	fmt.Printf("  found       %d / %d\n", a.Found, a.Total)
	fmt.Printf("  recall@1    %.3f\n", a.RecallAt1)
	fmt.Printf("  recall@3    %.3f\n", a.RecallAt3)
	fmt.Printf("  recall@5    %.3f\n", a.RecallAt5)
	fmt.Printf("  MRR         %.3f\n", a.MRR)
	fmt.Printf("  citation    %.3f  (over found)\n", a.CitationAccuracy)
	if len(res.Verdicts) > 0 {
		fmt.Println()
		fmt.Println("LLM judge verdicts:")
		for _, v := range res.Verdicts {
			if v.Error != "" {
				fmt.Printf("  %-6s ERROR  %s\n", v.QueryID, truncOneLine(v.Error, 80))
				continue
			}
			fmt.Printf("  %-6s score=%d  %s\n", v.QueryID, v.Score, truncOneLine(v.Rationale, 100))
		}
		fmt.Printf("  mean        %.3f  (judge)\n", res.MeanJudge)
	}
}

func truncOneLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// runPREval handles the --pr-fixture mode. Each fixture entry runs the
// six-stage prregress flow (fetch → worktree → build → query → agent →
// score). Output mirrors the regular eval format: human-readable per-PR
// table by default, --json emits the raw []Result slice.
func runPREval(ctx context.Context, opts *evalOpts) error {
	fx, err := prregress.LoadFixture(opts.prFixturePath)
	if err != nil {
		return err
	}

	emb, cleanup, err := resolveEmbedder(globalFlags.embedder, globalFlags.modelDir)
	if err != nil {
		return err
	}
	defer cleanup()

	runOpts := &prregress.RunOptions{
		Embedder: emb,
		TopK:     opts.prTopK,
	}
	if opts.judgeCmd != "" {
		// Reuse the same Claude binary for both the planner and judge.
		// They are separate prompts but the auth / model selection
		// pipeline should be identical.
		runOpts.Agent = &prregress.ClaudePlanAgent{Binary: opts.judgeCmd, Model: opts.judgeModel}
		runOpts.Scorer = &prregress.ClaudeJudgeScorer{Binary: opts.judgeCmd, Model: opts.judgeModel}
	}

	results, err := prregress.RunFixture(ctx, fx, runOpts)
	if err != nil {
		return err
	}

	// Determine the effective threshold for the CLI summary line. If
	// every entry uses DefaultThreshold (0.80) we report that; otherwise
	// we report -1 to indicate per-entry thresholds.
	threshold := prregress.DefaultThreshold
	for _, e := range fx.PRs {
		if e.Threshold != threshold {
			threshold = -1
			break
		}
	}

	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Summary string             `json:"summary"`
			Results []prregress.Result `json:"results"`
		}{
			Summary: prregress.SummarizeResults(results, threshold),
			Results: results,
		})
	}
	renderPREvalHuman(results, threshold)

	// CI gate: any errored or failing PR exits non-zero so build
	// pipelines can fail on retrieval regression.
	for _, r := range results {
		if r.Error != "" || !r.Pass {
			return fmt.Errorf("ckv pr-eval: %d/%d PR(s) failed threshold", failuresOnly(results), len(results))
		}
	}
	return nil
}

func failuresOnly(rs []prregress.Result) int {
	n := 0
	for _, r := range rs {
		if r.Error != "" || !r.Pass {
			n++
		}
	}
	return n
}

func renderPREvalHuman(results []prregress.Result, threshold float64) {
	fmt.Printf("ckv pr-eval — entries=%d threshold=%.2f\n\n", len(results), threshold)
	fmt.Println("Per-PR:")
	for _, r := range results {
		status := "PASS"
		switch {
		case r.Error != "":
			status = "ERROR"
		case !r.Pass:
			status = "FAIL"
		}
		fmt.Printf("  %-10s %s  judge=%.2f  file_f1=%.2f  (P=%.2f R=%.2f)\n",
			r.Entry.ID, status, r.Score.JudgeScore, r.Score.FileF1,
			r.Score.FilePrecision, r.Score.FileRecall)
		if r.Error != "" {
			fmt.Printf("             error: %s\n", truncOneLine(r.Error, 100))
		}
		if r.Score.JudgeRaw != "" && r.Score.JudgeError == "" {
			fmt.Printf("             %s\n", truncOneLine(r.Score.JudgeRaw, 110))
		}
		if r.Score.JudgeError != "" {
			fmt.Printf("             judge error: %s\n", truncOneLine(r.Score.JudgeError, 100))
		}
	}
	fmt.Println()
	fmt.Println(prregress.SummarizeResults(results, threshold))
}
