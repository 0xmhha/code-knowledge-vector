package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	prRuns        int    // PRR-2 — repeat each entry N times, report mean ± std
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
	f.IntVar(&opts.prRuns, "pr-runs", 1, "repeat each PR fixture entry N times and report mean ± sample std (N>=1; PR-regression mode only)")
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
// score). When --pr-runs > 1, the whole flow is repeated N times per
// entry and the CLI reports the mean ± sample std of judge_score /
// file_f1 across runs, plus a pass_rate (fraction of runs whose
// judge_score crossed the entry's threshold). LLM-judge noise on this
// fixture has been measured at σ ≈ 0.25, so averaging is the right
// gate at small fixture sizes.
//
// Output mirrors single-run mode when N == 1 so existing callers and
// JSON consumers keep working unchanged.
func runPREval(ctx context.Context, opts *evalOpts) error {
	if opts.prRuns < 1 {
		return fmt.Errorf("ckv pr-eval: --pr-runs must be >= 1, got %d", opts.prRuns)
	}
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

	// Repeat each entry prRuns times. For N == 1 the loop runs once
	// per entry and the aggregation reduces to a passthrough so
	// existing behavior is bit-for-bit identical.
	summaries := make([]entrySummary, 0, len(fx.PRs))
	for _, entry := range fx.PRs {
		runs := make([]prregress.Result, 0, opts.prRuns)
		for i := 0; i < opts.prRuns; i++ {
			runs = append(runs, prregress.RunEntry(ctx, entry, runOpts))
		}
		summaries = append(summaries, aggregateRuns(entry, runs))
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
		// For backwards compat, single-run mode keeps the old flat
		// shape (results: []prregress.Result). Multi-run emits the new
		// nested shape so consumers can branch on runs_per_entry.
		if opts.prRuns == 1 {
			flat := make([]prregress.Result, 0, len(summaries))
			for _, s := range summaries {
				flat = append(flat, s.Runs[0])
			}
			return enc.Encode(struct {
				Summary string             `json:"summary"`
				Results []prregress.Result `json:"results"`
			}{
				Summary: prregress.SummarizeResults(flat, threshold),
				Results: flat,
			})
		}
		return enc.Encode(struct {
			Summary       string          `json:"summary"`
			RunsPerEntry  int             `json:"runs_per_entry"`
			EntrySummary  []entrySummary  `json:"entry_summary"`
		}{
			Summary:      summarizeMultiRun(summaries, threshold),
			RunsPerEntry: opts.prRuns,
			EntrySummary: summaries,
		})
	}
	renderPREvalHumanMulti(summaries, threshold, opts.prRuns)

	// CI gate: an entry fails when its *mean* judge_score is below the
	// entry's threshold (or any run errored). This matches the spirit
	// of single-run gating while smoothing out per-run noise.
	for _, s := range summaries {
		if s.AnyError {
			return fmt.Errorf("ckv pr-eval: entry %s had %d/%d errored runs",
				s.Entry.ID, s.ErrorCount, opts.prRuns)
		}
		gate := s.Entry.Threshold
		if gate <= 0 {
			gate = prregress.DefaultThreshold
		}
		if s.JudgeMean < gate {
			return fmt.Errorf(
				"ckv pr-eval: entry %s mean judge_score=%.2f < threshold %.2f over %d run(s)",
				s.Entry.ID, s.JudgeMean, gate, opts.prRuns,
			)
		}
	}
	return nil
}

// entrySummary aggregates the N runs for one fixture entry. Means and
// stddevs use the sample formulas (denominator N-1 for std); for N=1
// stddev fields are 0.
type entrySummary struct {
	Entry              prregress.Entry    `json:"entry"`
	Runs               []prregress.Result `json:"runs"`
	JudgeMean          float64            `json:"judge_mean"`
	JudgeStd           float64            `json:"judge_std"`
	FileF1Mean         float64            `json:"file_f1_mean"`
	FileF1Std          float64            `json:"file_f1_std"`
	FilePrecisionMean  float64            `json:"file_precision_mean"`
	FileRecallMean     float64            `json:"file_recall_mean"`
	PassRate           float64            `json:"pass_rate"`
	ErrorCount         int                `json:"error_count"`
	AnyError           bool               `json:"any_error"`
}

// aggregateRuns reduces N Result records into one entrySummary.
func aggregateRuns(entry prregress.Entry, runs []prregress.Result) entrySummary {
	out := entrySummary{Entry: entry, Runs: runs}
	if len(runs) == 0 {
		return out
	}
	var judges, f1s, ps, rs []float64
	var passes int
	for _, r := range runs {
		if r.Error != "" {
			out.ErrorCount++
			continue
		}
		judges = append(judges, r.Score.JudgeScore)
		f1s = append(f1s, r.Score.FileF1)
		ps = append(ps, r.Score.FilePrecision)
		rs = append(rs, r.Score.FileRecall)
		if r.Pass {
			passes++
		}
	}
	out.AnyError = out.ErrorCount > 0
	if len(judges) > 0 {
		out.JudgeMean, out.JudgeStd = meanStd(judges)
		out.FileF1Mean, out.FileF1Std = meanStd(f1s)
		out.FilePrecisionMean, _ = meanStd(ps)
		out.FileRecallMean, _ = meanStd(rs)
		out.PassRate = float64(passes) / float64(len(runs))
	}
	return out
}

// meanStd returns sample mean and sample standard deviation (N-1 in
// the denominator). With len(xs) < 2 stddev is 0.
func meanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	std = math.Sqrt(sq / float64(len(xs)-1))
	return mean, std
}

// summarizeMultiRun produces the top-line summary string for
// multi-run mode. Reports overall pass-rate plus mean ± std of
// judge_score across all entries × runs.
func summarizeMultiRun(summaries []entrySummary, threshold float64) string {
	if len(summaries) == 0 {
		return "ckv pr-eval: empty fixture"
	}
	var totalJudges, totalF1s []float64
	var anyError int
	for _, s := range summaries {
		if s.AnyError {
			anyError++
		}
		for _, r := range s.Runs {
			if r.Error != "" {
				continue
			}
			totalJudges = append(totalJudges, r.Score.JudgeScore)
			totalF1s = append(totalF1s, r.Score.FileF1)
		}
	}
	jMean, jStd := meanStd(totalJudges)
	fMean, fStd := meanStd(totalF1s)
	return fmt.Sprintf(
		"ckv pr-eval: entries=%d runs_per_entry=%d judge=%.2f±%.2f file_f1=%.2f±%.2f errored_entries=%d threshold=%.2f",
		len(summaries), len(summaries[0].Runs), jMean, jStd, fMean, fStd, anyError, threshold,
	)
}

// renderPREvalHumanMulti renders the per-PR table for either single-run
// (N=1) or multi-run mode. For N=1 the output is byte-equivalent to the
// previous renderPREvalHuman; multi-run adds mean ± std and pass_rate
// columns and an aggregate summary line.
func renderPREvalHumanMulti(summaries []entrySummary, threshold float64, runs int) {
	fmt.Printf("ckv pr-eval — entries=%d runs_per_entry=%d threshold=%.2f\n\n",
		len(summaries), runs, threshold)
	fmt.Println("Per-PR:")
	for _, s := range summaries {
		if runs == 1 && len(s.Runs) == 1 {
			r := s.Runs[0]
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
			continue
		}
		// Multi-run row: mean ± std with pass rate.
		gate := s.Entry.Threshold
		if gate <= 0 {
			gate = prregress.DefaultThreshold
		}
		status := "PASS"
		switch {
		case s.AnyError:
			status = "ERROR"
		case s.JudgeMean < gate:
			status = "FAIL"
		}
		fmt.Printf("  %-10s %s  judge=%.2f±%.2f  file_f1=%.2f±%.2f  pass_rate=%.0f%%  (P=%.2f R=%.2f)\n",
			s.Entry.ID, status,
			s.JudgeMean, s.JudgeStd,
			s.FileF1Mean, s.FileF1Std,
			s.PassRate*100,
			s.FilePrecisionMean, s.FileRecallMean,
		)
		if s.AnyError {
			fmt.Printf("             %d/%d runs errored\n", s.ErrorCount, len(s.Runs))
		}
	}
	fmt.Println()
	if runs == 1 && len(summaries) > 0 {
		flat := make([]prregress.Result, 0, len(summaries))
		for _, s := range summaries {
			flat = append(flat, s.Runs[0])
		}
		fmt.Println(prregress.SummarizeResults(flat, threshold))
		return
	}
	fmt.Println(summarizeMultiRun(summaries, threshold))
}
