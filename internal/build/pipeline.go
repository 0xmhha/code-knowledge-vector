package build

import (
	"fmt"
	"os"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
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
	total.Truncated += s.Truncated
}
