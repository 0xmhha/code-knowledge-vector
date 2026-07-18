package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
	"github.com/0xmhha/code-knowledge-vector/internal/convention"
	"github.com/0xmhha/code-knowledge-vector/internal/discover"
	"github.com/0xmhha/code-knowledge-vector/internal/invariant"
	"github.com/0xmhha/code-knowledge-vector/internal/llmprefix"
	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/javascript"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/markdown"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-vector/internal/projectcfg"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// newParsers returns the standard set of language parsers.
func newParsers() map[string]cparse.Parser {
	return map[string]cparse.Parser{
		"go":         golang.New(),
		"typescript": typescript.New(),
		"javascript": javascript.New(),
		"solidity":   solidity.New(),
		"markdown":   markdown.New(),
	}
}

// newChunker creates a chunker with the given embedder and project config.
func newChunker(emb types.Embedder, cfg *projectcfg.Config) *chunk.Chunker {
	opts := chunk.Options{
		MaxInputTokens: emb.MaxInputTokens(),
	}
	if cfg != nil && cfg.Chunking.FileHeaderLines > 0 {
		opts.FileHeaderLines = cfg.Chunking.FileHeaderLines
	}
	// Phase B multi-granularity is an experiment gated behind an env flag while
	// its retrieval benefit is measured (docs/phase-b-*). Off by default.
	if os.Getenv("CKV_EXPERIMENTAL_FILE_FULL") == "1" {
		opts.IncludeFileFull = true
	}
	return chunk.New(opts)
}

// resolveEmbedTextFn selects the embed text function. When an LLM prefixer is
// set (Phase D.2), each chunk's embed text is its generated one-sentence
// context prefix followed by the rule-based prefix + raw chunk — the LLM prose
// is layered ON TOP of the rule-based signal, not instead of it. A cache miss /
// generation failure degrades to the rule-based prefix alone (D.1). Otherwise
// disablePrefix chooses raw (no prefix) vs the rule-based prefix.
//
// Combining (LLM + rule-based) beats LLM-alone: the rule-based prefix carries
// exact symbol/file tokens that a prose paraphrase dilutes. Measured on
// testdata/sample (docs/archive/llm-contextual-prefix-poc-2026-07-12.md), even the
// combined form does not beat rule-based alone on that small, self-descriptive
// corpus — so this lever ships opt-in and off by default.
func resolveEmbedTextFn(ctx context.Context, disablePrefix bool, prefixer llmprefix.Prefixer) func(types.Chunk) string {
	if prefixer != nil {
		return func(c types.Chunk) string {
			if pre := prefixer.Prefix(ctx, c); pre != "" {
				return pre + "\n" + chunk.BuildEmbedText(c)
			}
			return chunk.BuildEmbedText(c)
		}
	}
	if disablePrefix {
		return chunk.RawEmbedText
	}
	return chunk.BuildEmbedText
}

// resolveLLMPrefixer builds a disk-cached ollama LLM prefixer for the given
// model (empty → nil, i.e. no LLM prefix). The cache lives under outDir so
// rebuilds reuse generated prefixes.
func resolveLLMPrefixer(model, outDir string) llmprefix.Prefixer {
	if model == "" {
		return nil
	}
	p, err := llmprefix.NewCached(llmprefix.NewOllamaGenerator(model), filepath.Join(outDir, ".ckv-llmprefix-cache"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ckv: warning: LLM prefix disabled — %v\n", err)
		return nil
	}
	return p
}

// processFile reads, parses, and chunks a single source file.
// Returns nil chunks when the file should be skipped (unknown language,
// parser not available, empty parse result).
//
// For Go files, runs the invariant extractor and appends ChunkInvariant
// chunks plus back-references on overlapping source chunks. Failures
// in extraction degrade gracefully — we log to stderr and continue with
// the source chunks unannotated rather than failing the whole file.
func processFile(
	absPath, relPath, language, commitHash string,
	parsers map[string]cparse.Parser,
	cfg *projectcfg.Config,
	chunker *chunk.Chunker,
) ([]types.Chunk, error) {
	p, ok := parsers[language]
	if !ok {
		return nil, nil
	}
	if cfg != nil && !cfg.LanguageAllowed(language) {
		return nil, nil
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	spans, err := p.Parse(relPath, src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relPath, err)
	}
	chunks := chunker.Chunk(chunk.Input{
		File:       relPath,
		Language:   language,
		CommitHash: commitHash,
		Source:     src,
		Spans:      spans,
	})

	if language == "go" && len(chunks) > 0 {
		// Tier-3 heuristics are normally suppressed in *_test.go, but
		// governance test suites (systemcontracts/test/) encode real
		// invariants we want indexed — see includeTestInvariants (02 §4).
		skipT3 := !includeTestInvariants(relPath)
		results, ierr := invariant.Extract(relPath, src, invariant.Options{SkipTier3InTests: skipT3})
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "ckv: invariant skipped %s: %v\n", relPath, ierr)
		} else if len(results) > 0 {
			invChunks, refs := invariant.EmitChunks(relPath, commitHash, results)
			invariant.AttachRefs(chunks, results, refs)
			chunks = append(chunks, invChunks...)
		}
	}

	return chunks, nil
}

// reindexDocsRoots re-walks the curated docs roots and re-embeds their markdown
// as category="domain" doc chunks, returning the total chunks embedded. It
// mirrors the build-time `--docs` loop (builder.go) and is used by reindex
// after DeleteDocsChunks to replace the docs layer wholesale when the docs tree
// content hash changes. Kept as a small parallel loop rather than sharing the
// build path, which is coupled to build-only stat accounting.
func reindexDocsRoots(
	ctx context.Context,
	store *sqlitevec.Store,
	emb types.Embedder,
	batch int,
	parsers map[string]cparse.Parser,
	cfg *projectcfg.Config,
	chunker *chunk.Chunker,
	roots []string,
	embedTextFn func(types.Chunk) string,
) (int, error) {
	total := 0
	for _, root := range roots {
		files, walkErrs, werr := discover.Walk(root, discover.Options{})
		if werr != nil {
			return total, fmt.Errorf("walk docs %q: %w", root, werr)
		}
		for _, e := range walkErrs {
			fmt.Fprintf(os.Stderr, "ckv: docs walk warning: %v\n", e)
		}
		for _, f := range files {
			chunks, perr := processFile(f.AbsPath, f.RelPath, f.Language, "", parsers, cfg, chunker)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "ckv: %v\n", perr)
				continue
			}
			if len(chunks) == 0 {
				continue
			}
			// Curated corpus is always "domain" regardless of any source-tree
			// policy — same rule as the build path.
			for i := range chunks {
				chunks[i].Category = "domain"
			}
			if err := embedAndUpsert(ctx, store, emb, chunks, batch, nil, embedTextFn); err != nil {
				return total, fmt.Errorf("embed/upsert docs %s: %w", f.RelPath, err)
			}
			total += len(chunks)
		}
	}
	return total, nil
}

// accumulateStats adds the stats from a chunk slice to a running total.
func accumulateStats(total *chunk.Stats, chunks []types.Chunk) {
	s := chunk.Summarize(chunks)
	total.Total += s.Total
	total.Symbol += s.Symbol
	total.FileHeader += s.FileHeader
	total.Doc += s.Doc
	total.FunctionSplit += s.FunctionSplit
	total.PRDoc += s.PRDoc
	total.Invariant += s.Invariant
	total.Truncated += s.Truncated
}

// emitConventionChunks materializes one ChunkConvention per package
// from a convention.Aggregator. The returned chunks are ready to feed
// the embedder; each carries Text = Stats.Summary(pkg) so the embed
// vector represents the convention summary. Stats themselves are also
// attached via ConventionStats so the agent can query raw counts.
func emitConventionChunks(agg *convention.Aggregator, commitHash string) []types.Chunk {
	if agg == nil {
		return nil
	}
	stats := agg.Result()
	if len(stats) == 0 {
		return nil
	}
	out := make([]types.Chunk, 0, len(stats))
	for pkg, st := range stats {
		text := st.Summary(pkg)
		sha := types.ContentSHA256(text)
		file := pkg + "/<convention>"
		id := types.ChunkID(file, 0, 0, sha)
		out = append(out, types.Chunk{
			ID:              id,
			File:            file,
			StartLine:       0,
			EndLine:         0,
			Language:        "go",
			SymbolName:      pkg,
			SymbolKind:      "ConventionSummary",
			ChunkKind:       types.ChunkConvention,
			CommitHash:      commitHash,
			ContentSHA256:   sha,
			ConventionStats: st.ToMap(),
			Text:            text,
		})
	}
	return out
}
