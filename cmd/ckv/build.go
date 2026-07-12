package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/bgeonnx"
)

type buildOpts struct {
	src        string
	out        string
	ckgPath    string
	languages  []string
	exclude    []string
	filesFrom  string
	configPth  string
	policy     string
	docs       []string
	flowCorpus string
	batchSize  int
	jsonOut    bool

	llmPrefixModel string

	includePR bool
	prSince   string
	prRepo    string
}

func newBuildCmd() *cobra.Command {
	opts := &buildOpts{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the vector index from a code repository",
		Long: `Walks --src, parses supported languages (Go, TypeScript, Solidity, JavaScript, Markdown),
chunks function/method/type spans, embeds each chunk, and persists to
<out>/vector.db + <out>/manifest.json.

Re-running on a populated --out updates chunks in place (Upsert).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.src, "src", ".", "source repository path")
	f.StringVar(&opts.out, "out", "./ckv-data", "output data directory (vector.db, manifest.json)")
	f.StringVar(&opts.ckgPath, "ckg", "", "CKG data directory for symbol alignment (optional)")
	f.StringSliceVar(&opts.languages, "lang", nil, "languages to index (default: auto-detect; supported: go, typescript, javascript, solidity, markdown)")
	f.StringSliceVar(&opts.exclude, "exclude", nil, "extra ignore patterns (repeatable; e.g. --exclude='vendor/**' --exclude='**/*_gen.go')")
	f.StringVar(&opts.filesFrom, "files-from", "", "path to JSON file with {include, exclude} glob patterns; only files matching the include set (minus exclude) are embedded — applies to ALL languages")
	f.StringVar(&opts.configPth, "config", "", "path to ckv.yaml (optional)")
	f.StringVar(&opts.policy, "policy", "", "path to policy yaml (categorizes chunks by path; e.g. policy/stablenet.yaml)")
	f.StringSliceVar(&opts.docs, "docs", nil, "additional markdown corpus dirs to embed in the same index (repeatable; chunks tagged Category=domain; e.g. --docs=generated/domain-corpus/go-stablenet)")
	f.StringVar(&opts.flowCorpus, "flow-corpus", "", "path to a curated flow corpus JSONL (corpus.jsonl) to embed as flow_step/flow_spine/curated-invariant chunks (schema: <go-stablenet>/.claude/docs/corpus/SCHEMA.md)")
	f.BoolVar(&opts.jsonOut, "json", false, "machine-readable summary output")
	f.IntVar(&opts.batchSize, "batch", 0, "embedding batch size (0 = default 32); lower it (e.g. 4) when an embedder rejects large batches of big chunks")
	f.StringVar(&opts.llmPrefixModel, "llm-prefix-model", "", "enable Phase-D.2 LLM contextual prefix: name of a local Ollama chat model (e.g. 'llama3') that writes a one-sentence description of each chunk, prepended to its embed text. Empty keeps the cheap rule-based prefix. Disk-cached under <out>/.ckv-llmprefix-cache; a generation failure degrades to the rule-based prefix")
	f.BoolVar(&opts.includePR, "include-pr-history", false, "fetch merged PRs via gh CLI and index descriptions + commit messages")
	f.StringVar(&opts.prSince, "pr-since", "", "only PRs merged after this date (YYYY-MM-DD); requires --include-pr-history")
	f.StringVar(&opts.prRepo, "pr-repo", "", "GitHub repo (owner/repo) for PR fetch; auto-detected from git remote if empty")

	return cmd
}

func runBuild(ctx context.Context, opts *buildOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Memory pre-check before resolveEmbedder() — model load + CoreML
	// compile artifacts can be several GB and take seconds. The
	// in-Run() guard fires too late: by then the host has already paid
	// the resource cost the guard is meant to protect.
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

	buildOpts := build.Options{
		SrcRoot:                 opts.src,
		OutDir:                  opts.out,
		Embedder:                emb,
		Version:                 Version,
		CKVIgnore:               opts.exclude,
		FilesFromPath:           opts.filesFrom,
		Footprint:               fp,
		ProgressOut:             os.Stderr,
		DisableContextualPrefix: os.Getenv("CKV_DISABLE_CONTEXTUAL_PREFIX") == "1",
		LLMPrefixModel:          opts.llmPrefixModel,
		PolicyPath:              opts.policy,
		DocsRoots:               opts.docs,
		FlowCorpus:              opts.flowCorpus,
		CKGPath:                 opts.ckgPath,
		BatchSize:               opts.batchSize,
	}
	if opts.includePR {
		prFetch := &build.PRFetchOptions{Repo: opts.prRepo}
		if opts.prSince != "" {
			t, err := time.Parse("2006-01-02", opts.prSince)
			if err != nil {
				return fmt.Errorf("--pr-since: invalid date %q (expected YYYY-MM-DD)", opts.prSince)
			}
			prFetch.Since = t
		}
		buildOpts.PRFetch = prFetch
	}
	res, err := build.Run(ctx, buildOpts)
	if err != nil {
		return err
	}

	if opts.jsonOut {
		return json.NewEncoder(cmdOutput()).Encode(res)
	}
	fmt.Printf("ckv: indexed %d files → %d chunks (%d symbol, %d doc, %d header, %d truncated)\n",
		res.FilesIndexed, res.Chunks.Total, res.Chunks.Symbol, res.Chunks.Doc, res.Chunks.FileHeader, res.Chunks.Truncated)
	fmt.Printf("ckv: indexed_head=%s built_at=%s db=%s\n", res.IndexedHead, res.BuiltAt, res.DBPath)
	return nil
}

// cmdOutput is a hook so tests can capture writes. Today returns os.Stdout.
func cmdOutput() interface{ Write(p []byte) (int, error) } {
	return stdoutWriter{}
}

type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) {
	return fmt.Print(string(p))
}
