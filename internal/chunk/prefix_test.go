package chunk

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// TestBuildEmbedText_SymbolChunk verifies a typical symbol chunk gets
// the documented contextual prefix prepended. The raw chunk Text must
// appear unchanged after the prefix so snippet display and chunk IDs
// remain stable.
func TestBuildEmbedText_SymbolChunk(t *testing.T) {
	c := types.Chunk{
		File:       "server.go",
		Language:   "go",
		SymbolName: "Server.Listen",
		SymbolKind: types.KindMethod,
		ChunkKind:  types.ChunkSymbol,
		Text:       "func (s *Server) Listen() error {\n  return nil\n}",
	}
	got := BuildEmbedText(c)
	wantPrefix := "language: go. file: server.go. symbol: Server.Listen (Method).\n\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("missing prefix: got first 80 chars %q", got[:min(80, len(got))])
	}
	if !strings.HasSuffix(got, c.Text) {
		t.Errorf("raw text must be the suffix; got %q", got)
	}
}

// TestBuildEmbedText_FileHeader uses the file-header phrasing without
// a symbol — header chunks don't have one.
func TestBuildEmbedText_FileHeader(t *testing.T) {
	c := types.Chunk{
		File:      "cache.go",
		Language:  "go",
		ChunkKind: types.ChunkFileHeader,
		Text:      "package sample\n\nimport \"sync\"\n",
	}
	got := BuildEmbedText(c)
	want := "language: go. file: cache.go. file header.\n\npackage sample\n\nimport \"sync\"\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestBuildEmbedText_DocSection uses the "section:" phrasing and keeps
// the heading slug so markdown queries see "why-sqlite-vec" alongside
// the body.
func TestBuildEmbedText_DocSection(t *testing.T) {
	c := types.Chunk{
		File:       "docs/decisions.md",
		Language:   "markdown",
		SymbolName: "why-sqlite-vec",
		SymbolKind: types.KindDocSection,
		ChunkKind:  types.ChunkDoc,
		Text:       "## Why sqlite-vec\n\nWe picked sqlite-vec because...",
	}
	got := BuildEmbedText(c)
	if !strings.HasPrefix(got, "language: markdown. file: docs/decisions.md. section: why-sqlite-vec (DocSection).\n\n") {
		t.Errorf("missing doc prefix: got %q", got[:min(120, len(got))])
	}
}

// TestBuildEmbedText_FallbackOnMissingMetadata exercises the partial-
// info branches: a chunk with no SymbolName still gets a useful prefix.
func TestBuildEmbedText_FallbackOnMissingMetadata(t *testing.T) {
	noName := types.Chunk{File: "x.go", Language: "go", ChunkKind: types.ChunkSymbol, Text: "body"}
	got := BuildEmbedText(noName)
	if !strings.HasPrefix(got, "language: go. file: x.go.\n\n") {
		t.Errorf("no-symbol prefix wrong: %q", got)
	}

	nameOnly := types.Chunk{File: "x.go", Language: "go", SymbolName: "Foo", ChunkKind: types.ChunkSymbol, Text: "body"}
	got = BuildEmbedText(nameOnly)
	if !strings.HasPrefix(got, "language: go. file: x.go. symbol: Foo.\n\n") {
		t.Errorf("name-only prefix wrong: %q", got)
	}

	noLang := types.Chunk{File: "x", SymbolName: "Foo", SymbolKind: types.KindFunction, ChunkKind: types.ChunkSymbol, Text: "body"}
	got = BuildEmbedText(noLang)
	if !strings.HasPrefix(got, "language: unknown. file: x. symbol: Foo (Function).\n\n") {
		t.Errorf("empty-language fallback wrong: %q", got)
	}
}

// TestRawEmbedText is the trivial baseline — returns Text unchanged.
// Used by callers who want to disable the prefix for A/B measurement.
func TestRawEmbedText(t *testing.T) {
	c := types.Chunk{Text: "hello"}
	if got := RawEmbedText(c); got != "hello" {
		t.Errorf("RawEmbedText = %q, want %q", got, "hello")
	}
}

// TestBuildEmbedText_IsDeterministic guarantees the same chunk yields
// the same prefix across calls — important so chunk IDs (which hash
// the raw Text, not the embed text) stay stable across rebuilds.
func TestBuildEmbedText_IsDeterministic(t *testing.T) {
	c := types.Chunk{
		File: "a.go", Language: "go",
		SymbolName: "Foo", SymbolKind: types.KindFunction,
		ChunkKind: types.ChunkSymbol, Text: "body",
	}
	a := BuildEmbedText(c)
	b := BuildEmbedText(c)
	if a != b {
		t.Errorf("BuildEmbedText is non-deterministic: %q vs %q", a, b)
	}
}
