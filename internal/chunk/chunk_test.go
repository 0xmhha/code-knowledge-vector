package chunk

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestChunkSymbolAndFileHeader(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A() { fmt.Println("a") }
func B() { fmt.Println("b") }
`)
	in := Input{
		File:       "x.go",
		Language:   "go",
		CommitHash: "abc",
		Source:     src,
		Spans: []parse.SymbolSpan{
			{Name: "A", Kind: types.KindFunction, StartLine: 5, EndLine: 5, Text: `func A() { fmt.Println("a") }`},
			{Name: "B", Kind: types.KindFunction, StartLine: 6, EndLine: 6, Text: `func B() { fmt.Println("b") }`},
		},
	}
	chunks := New(Options{}).Chunk(in)
	stats := Summarize(chunks)
	if stats.Total != 3 || stats.Symbol != 2 || stats.FileHeader != 1 {
		t.Fatalf("expected 1 file_header + 2 symbol chunks, got %+v", stats)
	}
}

func TestChunkIDsDeterministic(t *testing.T) {
	in := Input{
		File:       "x.go",
		Language:   "go",
		CommitHash: "abc",
		Source:     []byte("package x\n\nfunc A() {}\n"),
		Spans: []parse.SymbolSpan{
			{Name: "A", Kind: types.KindFunction, StartLine: 3, EndLine: 3, Text: "func A() {}"},
		},
	}
	a := New(Options{}).Chunk(in)
	b := New(Options{}).Chunk(in)
	if len(a) != len(b) {
		t.Fatalf("chunk count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("chunk %d id differs: %s vs %s", i, a[i].ID, b[i].ID)
		}
	}
}

func TestTruncationKeepsHeadAndMarker(t *testing.T) {
	long := strings.Repeat("a", 1000)
	in := Input{
		File:       "x.go",
		Language:   "go",
		CommitHash: "abc",
		Source:     []byte("package x\n"),
		Spans: []parse.SymbolSpan{
			{Name: "Big", Kind: types.KindFunction, StartLine: 1, EndLine: 1, Text: long},
		},
	}
	// MaxInputTokens=50 → ~200 chars
	chunks := New(Options{MaxInputTokens: 50}).Chunk(in)
	// Find the symbol chunk (not file_header).
	var bigChunk types.Chunk
	for _, c := range chunks {
		if c.ChunkKind == types.ChunkSymbol {
			bigChunk = c
			break
		}
	}
	if len(bigChunk.Text) > 50*charsPerToken {
		t.Errorf("text not truncated: len=%d", len(bigChunk.Text))
	}
	if !strings.Contains(bigChunk.Text, "[CKV-TRUNCATED]") {
		t.Errorf("truncation marker missing: %q", bigChunk.Text)
	}
	if !strings.HasPrefix(bigChunk.Text, "aaa") {
		t.Errorf("head not preserved: %q", bigChunk.Text)
	}
}

func TestEmptyFileNoHeader(t *testing.T) {
	chunks := New(Options{}).Chunk(Input{File: "empty.go", Language: "go", CommitHash: "x", Source: nil})
	if len(chunks) != 0 {
		t.Errorf("empty file should produce no chunks, got %d", len(chunks))
	}
}

// Markdown inputs skip the file_header chunk because every heading
// section is already its own chunk — emitting a leading-N-lines chunk
// on top would duplicate the first section verbatim and inflate
// retrieval noise.
func TestMarkdownSkipsFileHeader(t *testing.T) {
	src := []byte("# Title\n\nbody\n\n## Sub\n\nmore\n")
	in := Input{
		File:       "x.md",
		Language:   "markdown",
		CommitHash: "abc",
		Source:     src,
		Spans: []parse.SymbolSpan{
			{Name: "title", Kind: types.KindDocSection, StartLine: 1, EndLine: 4, Text: "# Title\n\nbody\n\n"},
			{Name: "sub", Kind: types.KindDocSection, StartLine: 5, EndLine: 7, Text: "## Sub\n\nmore\n"},
		},
	}
	chunks := New(Options{}).Chunk(in)
	stats := Summarize(chunks)
	if stats.FileHeader != 0 {
		t.Errorf("markdown should not produce file_header chunks, got %d", stats.FileHeader)
	}
	if stats.Symbol != 0 {
		t.Errorf("DocSection spans should not produce symbol chunks (got %d) — chunk_kind=doc is the new classification", stats.Symbol)
	}
	if stats.Doc != 2 {
		t.Errorf("expected 2 doc chunks, got %d", stats.Doc)
	}
	for _, c := range chunks {
		if c.ChunkKind != types.ChunkDoc {
			t.Errorf("expected ChunkDoc for %s, got %s", c.SymbolName, c.ChunkKind)
		}
	}
}
