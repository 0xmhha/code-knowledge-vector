// Package chunk turns ([]parse.SymbolSpan, source) into ([]types.Chunk)
// — the records the embedder + vector store actually persist.
//
// Strategy:
//  1. Each symbol span (function/method/type/...) becomes one chunk.
//  2. Spans whose Text exceeds MaxInputTokens are split into
//     sub-chunks (function bodies) or truncated at the head so the
//     signature stays intact. Note that the mock embedder has infinite
//     max input, so this only triggers with the real ONNX embedder.
//  3. A file_header chunk captures the first N lines of the file —
//     package decl + imports + top-level const/var. This lets queries
//     like "what package owns the metrics client" hit the right file
//     even when nothing function-level matches.
package chunk

import (
	"fmt"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultFileHeaderLines is the number of leading lines of each file
// captured as a "file_header" chunk when Options.FileHeaderLines is
// unset. 50 is enough to cover the package decl + a typical import
// block + top-level consts. Per-project ckv.yaml can override this
// via chunking.file_header_lines.
const DefaultFileHeaderLines = 50

// FileHeaderLines is the legacy export retained for callers that
// reference the constant directly. New code should set
// Options.FileHeaderLines and read Chunker.opts.
const FileHeaderLines = DefaultFileHeaderLines

// charsPerToken is the approximate ratio used to convert MaxInputTokens
// into a character cap. Real tokenizers vary (BPE for bge-code ~3.5,
// GPT ~4); 4 is a safe upper bound that keeps us under the model limit
// while keeping the math simple.
const charsPerToken = 4

// Options configure chunking. Zero value uses documented defaults.
type Options struct {
	MaxInputTokens    int  // hard upper bound on chunk Text size; 0 → no cap
	IncludeFileHeader bool // emit a file_header chunk per file (default: true)
	// FileHeaderLines overrides DefaultFileHeaderLines (50). 0 keeps
	// the default. Sourced from project ckv.yaml.chunking.file_header_lines.
	FileHeaderLines int
}

// Input is everything the chunker needs about one file.
type Input struct {
	File       string // repo-relative path
	Language   string // "go" | "typescript" | "solidity" | "markdown"
	CommitHash string // built-time git HEAD
	Source     []byte // full file contents
	Spans      []parse.SymbolSpan
}

// Chunker turns parsed spans into Chunks.
type Chunker struct {
	opts Options
}

// New returns a Chunker with the given options. opts.MaxInputTokens of
// 0 disables truncation (appropriate for the mock embedder).
func New(opts Options) *Chunker {
	if !opts.IncludeFileHeader {
		// Make the default opt-in: explicit zero-value Options{} should
		// produce the typical "include the header" behavior. We flip
		// the bit here so the caller writes Options{} for the default
		// and Options{IncludeFileHeader: false} only when disabling.
		opts.IncludeFileHeader = true
	}
	return &Chunker{opts: opts}
}

// Chunk produces all chunks for one file. The returned slice carries
// IDs and content_sha256 already computed — callers do not need to
// hash again before Upsert.
func (c *Chunker) Chunk(in Input) []types.Chunk {
	out := make([]types.Chunk, 0, len(in.Spans)+1)

	// Skip the file_header chunk for markdown inputs: every heading
	// section is already its own chunk, so the leading-N-lines slice
	// would duplicate the first section. Source-code languages keep
	// the header because their span coverage is sparse (no chunks for
	// imports/consts) — markdown's is dense.
	if c.opts.IncludeFileHeader && in.Language != "markdown" {
		if hdr := c.fileHeaderChunk(in); hdr != nil {
			out = append(out, *hdr)
		}
	}

	for _, sp := range in.Spans {
		// When a function body exceeds the embedder's
		// input cap, split it into multiple sub-chunks instead of
		// head-truncating. Long doc sections / file headers stay on
		// the truncate path — splitting prose loses structure.
		if c.shouldSplit(sp) {
			out = append(out, c.splitLongSpan(in, sp)...)
			continue
		}
		text := c.maybeTruncate(sp.Text)
		out = append(out, c.symbolChunk(in, sp, text))
	}
	return out
}

// shouldSplit reports whether a span gets the function-split treatment.
// Only function-like kinds (Function / Method) are eligible: type
// declarations and Solidity contracts/events are structurally too
// short to benefit, and markdown sections don't have signatures to
// preserve.
func (c *Chunker) shouldSplit(sp parse.SymbolSpan) bool {
	if c.opts.MaxInputTokens <= 0 {
		return false
	}
	maxChars := c.opts.MaxInputTokens * charsPerToken
	if len(sp.Text) <= maxChars {
		return false
	}
	switch sp.Kind {
	case types.KindFunction, types.KindMethod:
		return true
	default:
		return false
	}
}

// splitLongSpan slices the span body into multiple ChunkFunctionSplit
// chunks. Each chunk carries:
//
//   - SymbolName = "<original>:chunk:<n>" (1-indexed) so callers can
//     reassemble the order.
//   - ChunkKind = ChunkFunctionSplit so consumers can filter or
//     visually badge split results.
//   - StartLine/EndLine = the window's actual lines in the source
//     file. Citation lookups land on the right slice.
//
// The signature is *not* duplicated into each split's Text — the
// rule-based contextual prefix (BuildEmbedText) already prepends
// "symbol: X.Y" at embed time so the embedder still sees the symbol
// identity for every split. Snippet display reads Text directly, so
// the user sees the actual window content (signature shows only on
// chunk 1, which contains the function's opening line).
//
// Window math: divide len(sp.Text) by maxChars to get N (with one
// extra chunk for the remainder), then split sp.Text by line count
// evenly. No overlap — adding it is a future refinement when
// measurement shows recall improvement.
func (c *Chunker) splitLongSpan(in Input, sp parse.SymbolSpan) []types.Chunk {
	maxChars := c.opts.MaxInputTokens * charsPerToken
	lines := strings.Split(sp.Text, "\n")
	if len(lines) <= 1 {
		// Degenerate one-line giant — truncate as before.
		return []types.Chunk{c.symbolChunk(in, sp, c.maybeTruncate(sp.Text))}
	}

	// Aim each window for ~maxChars of content. avg chars per line of
	// this span gives us a windowSize that respects the cap.
	avgCharsPerLine := max(1, (len(sp.Text)+len(lines)-1)/len(lines))
	windowLines := max(1, maxChars/avgCharsPerLine)

	chunks := make([]types.Chunk, 0, (len(lines)+windowLines-1)/windowLines)
	for i, idx := 0, 0; i < len(lines); i += windowLines {
		end := min(i+windowLines, len(lines))
		windowText := strings.Join(lines[i:end], "\n")
		// File-relative line range: span starts at sp.StartLine, so the
		// window starts at sp.StartLine + i. Inclusive on both ends.
		startLine := sp.StartLine + i
		endLine := sp.StartLine + end - 1
		idx++

		contentHash := types.ContentSHA256(windowText)
		id := types.ChunkID(in.File, startLine, endLine, contentHash)
		chunks = append(chunks, types.Chunk{
			ID:            id,
			File:          in.File,
			StartLine:     startLine,
			EndLine:       endLine,
			Language:      in.Language,
			IsTest:        types.IsTestPath(in.File, in.Language),
			SymbolName:    fmt.Sprintf("%s:chunk:%d", sp.Name, idx),
			SymbolKind:    sp.Kind,
			ChunkKind:     types.ChunkFunctionSplit,
			CommitHash:    in.CommitHash,
			ContentSHA256: contentHash,
			Text:          windowText,
		})
	}
	return chunks
}

// fileHeaderChunk emits the leading-lines chunk. Returns nil for empty
// files or when the file has fewer than 2 non-blank lines (no signal).
func (c *Chunker) fileHeaderChunk(in Input) *types.Chunk {
	if len(in.Source) == 0 {
		return nil
	}
	limit := c.opts.FileHeaderLines
	if limit <= 0 {
		limit = DefaultFileHeaderLines
	}
	lines := strings.SplitN(string(in.Source), "\n", limit+1)
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > limit {
		lines = lines[:limit]
	}
	text := strings.Join(lines, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	contentHash := types.ContentSHA256(text)
	id := types.ChunkID(in.File, 1, len(lines), contentHash)
	return &types.Chunk{
		ID:            id,
		File:          in.File,
		StartLine:     1,
		EndLine:       len(lines),
		Language:      in.Language,
		IsTest:        types.IsTestPath(in.File, in.Language),
		SymbolKind:    types.KindFileHeader,
		ChunkKind:     types.ChunkFileHeader,
		CommitHash:    in.CommitHash,
		ContentSHA256: contentHash,
		Text:          c.maybeTruncate(text),
	}
}

func (c *Chunker) symbolChunk(in Input, sp parse.SymbolSpan, text string) types.Chunk {
	contentHash := types.ContentSHA256(text)
	id := types.ChunkID(in.File, sp.StartLine, sp.EndLine, contentHash)
	kind := types.ChunkSymbol
	if sp.Kind == types.KindDocSection || sp.Kind == types.KindADRSection {
		kind = types.ChunkDoc
	}
	return types.Chunk{
		ID:            id,
		File:          in.File,
		StartLine:     sp.StartLine,
		EndLine:       sp.EndLine,
		Language:      in.Language,
		IsTest:        types.IsTestPath(in.File, in.Language),
		SymbolName:    sp.Name,
		SymbolKind:    sp.Kind,
		ChunkKind:     kind,
		CommitHash:    in.CommitHash,
		ContentSHA256: contentHash,
		Text:          text,
	}
}

// maybeTruncate enforces the embedder's input cap. Keeps the prefix so
// the signature stays embedded. The trailing "// ..." marker makes the
// truncation explicit in audit/eval logs without changing semantics.
func (c *Chunker) maybeTruncate(text string) string {
	if c.opts.MaxInputTokens <= 0 {
		return text
	}
	max := c.opts.MaxInputTokens * charsPerToken
	if len(text) <= max {
		return text
	}
	const marker = "\n// ... [CKV-TRUNCATED]"
	if max <= len(marker) {
		return text[:max]
	}
	return text[:max-len(marker)] + marker
}

// Stats summarizes the output of one Chunk() call; useful for build
// progress and the bootstrap report.
type Stats struct {
	Total         int
	Symbol        int
	FileHeader    int
	Doc           int
	FunctionSplit int
	PRDoc         int
	Invariant     int
	Truncated     int
}

// Summarize counts chunk kinds. Cheap O(n) pass over the slice.
func Summarize(chunks []types.Chunk) Stats {
	var s Stats
	for _, c := range chunks {
		s.Total++
		switch c.ChunkKind {
		case types.ChunkSymbol:
			s.Symbol++
		case types.ChunkFileHeader:
			s.FileHeader++
		case types.ChunkDoc:
			s.Doc++
		case types.ChunkFunctionSplit:
			s.FunctionSplit++
		case types.ChunkPRBackground, types.ChunkPRSolution, types.ChunkCommitMessage:
			s.PRDoc++
		case types.ChunkInvariant:
			s.Invariant++
		}
		if strings.Contains(c.Text, "[CKV-TRUNCATED]") {
			s.Truncated++
		}
	}
	return s
}

// formatLineRange is a tiny utility kept here (not in pkg/types) because
// it's only used for log + progress strings, not the chunk model.
func formatLineRange(start, end int) string {
	return fmt.Sprintf("%d-%d", start, end)
}

var _ = formatLineRange // reserved for future progress logging
