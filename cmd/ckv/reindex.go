package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/bgeonnx"
)

type reindexOpts struct {
	src     string
	out     string
	since   string
	files   []string
	exclude []string
	policy  string
	jsonOut bool

	llmPrefixModel string

	includePR bool
	prSince   string
	prRepo    string
}

func newReindexCmd() *cobra.Command {
	opts := &reindexOpts{}

	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Incrementally update the index for files changed since the last build",
		Long: `Re-embeds only the files that changed between the manifest's
indexed_head and the source tree's current git HEAD. Use --since to
override the diff base; use --files to bypass git entirely and
reindex an explicit path list (e.g., from a CI hook or watcher).

Requires a prior 'ckv build' to seed the manifest. The embedder
identity must match the index — re-embedding with a different model
or dimension is refused.

Examples:
  ckv reindex --out ./ckv-data
  ckv reindex --out ./ckv-data --since main~5
  ckv reindex --out ./ckv-data --files internal/x.go,internal/y.go`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReindex(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.src, "src", ".", "source repository path")
	f.StringVar(&opts.out, "out", "./ckv-data", "data directory (vector.db, manifest.json)")
	f.StringVar(&opts.since, "since", "", "git commit to diff from (default: manifest.indexed_head)")
	f.StringSliceVar(&opts.files, "files", nil, "force-reindex these src-relative paths (bypasses git diff)")
	f.StringSliceVar(&opts.exclude, "exclude", nil, "extra ignore patterns (repeatable; e.g. --exclude='vendor/**')")
	f.StringVar(&opts.policy, "policy", "", "path to policy yaml (must match the build's policy; categorizes chunks by path)")
	f.StringVar(&opts.llmPrefixModel, "llm-prefix-model", "", "Phase-D.2 LLM contextual prefix model (must match the build's value; empty keeps the rule-based prefix)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable summary output")
	f.BoolVar(&opts.includePR, "include-pr-history", false, "incrementally fetch merged PRs via gh CLI and index only those newer than the recorded cutoff")
	f.StringVar(&opts.prSince, "pr-since", "", "only PRs merged after this date (YYYY-MM-DD); requires --include-pr-history (default: manifest cutoff)")
	f.StringVar(&opts.prRepo, "pr-repo", "", "GitHub repo (owner/repo) for PR fetch; auto-detected from git remote if empty")

	return cmd
}

func runReindex(ctx context.Context, opts *reindexOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if globalFlags.embedder == "bgeonnx" {
		needMB := bgeonnx.EstimatedRAMMB(bgeonnx.Options{ModelDir: globalFlags.modelDir})
		if err := build.PreCheckByEstimate(needMB, os.Stderr); err != nil {
			return err
		}
	}
	emb, cleanup, err := resolveEmbedder(globalFlags.embedder, globalFlags.modelDir)
	if err != nil {
		return err
	}
	defer cleanup()

	fp := newFootprint(opts.out, "")
	defer fp.Close()

	reindexOptions := build.ReindexOptions{
		SrcRoot:                 opts.src,
		OutDir:                  opts.out,
		Embedder:                emb,
		CKVIgnore:               opts.exclude,
		Since:                   opts.since,
		Files:                   opts.files,
		Footprint:               fp,
		ProgressOut:             os.Stderr,
		DisableContextualPrefix: os.Getenv("CKV_DISABLE_CONTEXTUAL_PREFIX") == "1",
		LLMPrefixModel:          opts.llmPrefixModel,
		PolicyPath:              opts.policy,
	}
	if opts.includePR {
		prFetch := &build.PRFetchOptions{Repo: opts.prRepo}
		if opts.prSince != "" {
			t, perr := time.Parse("2006-01-02", opts.prSince)
			if perr != nil {
				return fmt.Errorf("--pr-since: invalid date %q (expected YYYY-MM-DD)", opts.prSince)
			}
			prFetch.Since = t
		}
		reindexOptions.PRFetch = prFetch
	}
	res, err := build.Reindex(ctx, reindexOptions)
	if err != nil {
		// Surface the two domain errors with operator-friendly hints.
		if errors.Is(err, build.ErrNoManifest) {
			return fmt.Errorf("%w\n  hint: run `ckv build --src %s --out %s` to seed the index",
				err, opts.src, opts.out)
		}
		if errors.Is(err, build.ErrEmbedderMismatch) {
			return fmt.Errorf("%w\n  hint: use the same --embedder that built the index, or run `ckv build` to replace it",
				err)
		}
		if errors.Is(err, build.ErrSchemaCascade) {
			return fmt.Errorf("%w\n  hint: the CKG graph's schema changed; run `ckv build` to rebuild the index against the new graph",
				err)
		}
		return err
	}

	if opts.jsonOut {
		return json.NewEncoder(cmdOutput()).Encode(res)
	}
	fmt.Printf("ckv: %d files processed (%d added, %d modified, %d deleted, %d skipped)\n",
		res.FilesProcessed, res.FilesAdded, res.FilesModified, res.FilesDeleted, res.FilesSkipped)
	fmt.Printf("ckv: chunks %d (%d symbol, %d doc, %d header, %d truncated)\n",
		res.Chunks.Total, res.Chunks.Symbol, res.Chunks.Doc, res.Chunks.FileHeader, res.Chunks.Truncated)
	if res.PRsIndexed > 0 {
		fmt.Printf("ckv: incremental PR ingest → %d new PR chunks\n", res.PRsIndexed)
	}
	if res.FlowReindexed > 0 {
		fmt.Printf("ckv: flow corpus changed → re-indexed %d flow chunks\n", res.FlowReindexed)
	}
	if res.FilesResumed > 0 {
		fmt.Printf("ckv: resumed — %d files skipped (already done by an interrupted run)\n", res.FilesResumed)
	}
	fmt.Printf("ckv: %s → %s at %s\n", res.PrevHead, res.NewHead, res.BuiltAt)
	return nil
}
