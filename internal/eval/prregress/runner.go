package prregress

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// RunOptions configure how RunFixture / RunEntry operate.
// Defaults try to do the most useful thing with the least caller
// input: Claude CLI for both agent and scorer, top-10 hints to the
// agent, index built into a temp dir beside the worktree.
type RunOptions struct {
	// Embedder is used to build the per-PR index and answer hint
	// queries. Required.
	Embedder types.Embedder
	// EmbedderName is the value Embedder.Name() returns, captured so
	// post-run reports can label which model produced the score.
	EmbedderName string

	// Agent generates the implementation plan from PR Background +
	// search hints. Defaults to NewClaudePlanAgent().
	Agent PlanAgent
	// Scorer grades the plan against the actual diff. Defaults to
	// NewClaudeJudgeScorer().
	Scorer JudgeScorer

	// TopK is how many ckv hits to hand the agent as "candidates worth
	// looking at." Defaults to 10.
	TopK int

	// IndexBase is the parent directory under which per-PR ckv index
	// dirs are created. Defaults to $TMPDIR. Each entry creates
	// <IndexBase>/ckv-prregress-<id>-<sha[:12]>/index/.
	IndexBase string
}

func (o *RunOptions) fill() error {
	if o.Embedder == nil {
		return fmt.Errorf("RunOptions.Embedder is required")
	}
	if o.EmbedderName == "" {
		o.EmbedderName = o.Embedder.Name()
	}
	if o.Agent == nil {
		o.Agent = NewClaudePlanAgent()
	}
	if o.Scorer == nil {
		o.Scorer = NewClaudeJudgeScorer()
	}
	if o.TopK <= 0 {
		o.TopK = 10
	}
	if o.IndexBase == "" {
		o.IndexBase = os.TempDir()
	}
	return nil
}

// RunFixture executes RunEntry for every PR in fx. Results come back
// in the same order as fx.PRs. Individual failures don't abort the
// batch — each Result captures its own Error string.
func RunFixture(ctx context.Context, fx *Fixture, opts *RunOptions) ([]Result, error) {
	if fx == nil {
		return nil, fmt.Errorf("RunFixture: nil fixture")
	}
	if opts == nil {
		opts = &RunOptions{}
	}
	if err := opts.fill(); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(fx.PRs))
	for _, e := range fx.PRs {
		out = append(out, RunEntry(ctx, e, opts))
	}
	return out, nil
}

// RunEntry executes the six-stage flow for one PR. Errors from any
// stage are folded into Result.Error; later stages skip but the
// pre-computed parts of Score / Plan / Meta still appear in the report
// so the user can see how far the pipeline got.
//
//	1. fetch PR metadata (gh CLI)
//	2. create detached git worktree at base_sha
//	3. ckv build over the worktree
//	4. ckv query (Background as the intent) → top-K hints
//	5. agent generates plan (Background + hints)
//	6. fetch PR diff + score (plan vs diff)
func RunEntry(ctx context.Context, e Entry, opts *RunOptions) Result {
	if opts == nil {
		opts = &RunOptions{}
	}
	if err := opts.fill(); err != nil {
		return Result{Entry: e, Error: err.Error()}
	}
	r := Result{Entry: e}

	// Stage 1 — fetch meta.
	meta, err := FetchMeta(ctx, e)
	if err != nil {
		r.Error = fmt.Sprintf("fetch meta: %v", err)
		return r
	}
	r.Meta = meta

	// Stage 2 — worktree.
	wt, err := CreateWorktree(ctx, e)
	if err != nil {
		r.Error = fmt.Sprintf("worktree: %v", err)
		return r
	}
	defer func() { _ = wt.Close() }()

	// Stage 3 — ckv build.
	indexDir := filepath.Join(opts.IndexBase, fmt.Sprintf("ckv-prregress-%s-%s", e.ID, wt.BaseSHA[:12]), "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		r.Error = fmt.Sprintf("mkdir index: %v", err)
		return r
	}
	if _, err := build.Run(ctx, build.Options{
		SrcRoot:  wt.Path,
		OutDir:   indexDir,
		Embedder: opts.Embedder,
	}); err != nil {
		r.Error = fmt.Sprintf("ckv build: %v", err)
		return r
	}

	// Stage 4 — ckv query for hints.
	eng, err := query.Open(indexDir, opts.Embedder)
	if err != nil {
		r.Error = fmt.Sprintf("query open: %v", err)
		return r
	}
	defer eng.Close()

	intent := meta.Background
	if intent == "" {
		intent = meta.Title // fall back to title when description is empty
	}
	resp, err := eng.Search(ctx, intent, query.Options{K: opts.TopK})
	if err != nil {
		r.Error = fmt.Sprintf("query search: %v", err)
		return r
	}

	// Stage 5 — agent plan.
	plan, err := opts.Agent.Generate(ctx, e, meta, resp.Hits)
	if err != nil {
		r.Error = fmt.Sprintf("agent generate: %v", err)
		return r
	}
	r.Plan = plan

	// Stage 6 — diff fetch + score.
	diff, err := FetchDiff(ctx, e)
	if err != nil {
		r.Error = fmt.Sprintf("fetch diff: %v", err)
		// We can still compute file-set F1 without the diff (judge
		// will simply have nothing to grade). Surface the failure but
		// keep going so the report has the F1 signal.
		diff = ""
	}
	score, err := opts.Scorer.Score(ctx, e, meta, plan, diff)
	if err != nil {
		r.Error = fmt.Sprintf("score: %v", err)
		r.Score = score
		return r
	}
	r.Score = score

	threshold := e.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	r.Pass = score.JudgeScore >= threshold && score.JudgeError == ""

	return r
}

// SummarizeResults formats a batch outcome for the CLI report. Plain
// text by default; JSON is the caller's responsibility (just encode
// the []Result slice).
func SummarizeResults(results []Result, threshold float64) string {
	var pass, fail, errored int
	for _, r := range results {
		switch {
		case r.Error != "":
			errored++
		case r.Pass:
			pass++
		default:
			fail++
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf("ckv pr-eval @ %s — total=%d pass=%d fail=%d errored=%d (threshold=%.2f)",
		now, len(results), pass, fail, errored, threshold)
}
