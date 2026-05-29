package build

import (
	"fmt"
	"os"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
	"github.com/0xmhha/code-knowledge-vector/internal/invariant"
	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/javascript"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/markdown"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-vector/internal/projectcfg"
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
	return chunk.New(opts)
}

// resolveEmbedTextFn selects the embed text function based on the
// contextual prefix flag.
func resolveEmbedTextFn(disablePrefix bool) func(types.Chunk) string {
	if disablePrefix {
		return chunk.RawEmbedText
	}
	return chunk.BuildEmbedText
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
		results, ierr := invariant.Extract(relPath, src, invariant.Options{SkipTier3InTests: true})
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
