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

func TestIncludeFileFullEmitsCoarseChunk(t *testing.T) {
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

	// Off by default: no file_full chunk.
	for _, c := range New(Options{}).Chunk(in) {
		if c.ChunkKind == types.ChunkFileFull {
			t.Fatal("file_full emitted without IncludeFileFull")
		}
	}

	// On: exactly one additive file_full chunk spanning the whole file, with a
	// distinct ID from the file_header chunk (so both coexist in the store).
	chunks := New(Options{IncludeFileFull: true}).Chunk(in)
	var full, header *types.Chunk
	for i := range chunks {
		switch chunks[i].ChunkKind {
		case types.ChunkFileFull:
			if full != nil {
				t.Fatal("more than one file_full chunk")
			}
			full = &chunks[i]
		case types.ChunkFileHeader:
			header = &chunks[i]
		}
	}
	if full == nil || header == nil {
		t.Fatalf("want both file_full and file_header, got full=%v header=%v", full != nil, header != nil)
	}
	if full.Text != string(src) {
		t.Errorf("file_full text should be the whole file")
	}
	if full.ID == header.ID {
		t.Errorf("file_full and file_header must have distinct IDs")
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

// TestLongFunctionSplitsIntoMultipleChunks verifies that a multi-line
// function body exceeding MaxInputTokens gets split into N
// ChunkFunctionSplit chunks instead of being head-truncated.
func TestLongFunctionSplitsIntoMultipleChunks(t *testing.T) {
	// 60 distinct lines, each ~20 chars → ~1200 chars total.
	bodyLines := []string{}
	for range 60 {
		bodyLines = append(bodyLines, "  doWork(stepNum)  ")
	}
	long := "func Big() {\n" + strings.Join(bodyLines, "\n") + "\n}"

	// MaxInputTokens=50 → ~200 char cap. Body is ~1200 chars → must split.
	chunks := New(Options{MaxInputTokens: 50}).Chunk(Input{
		File:       "x.go",
		Language:   "go",
		CommitHash: "abc",
		Source:     []byte("package x\n"),
		Spans: []parse.SymbolSpan{
			{Name: "Big", Kind: types.KindFunction, StartLine: 10, EndLine: 71, Text: long},
		},
	})

	// Find every function_split chunk.
	splits := []types.Chunk{}
	for _, c := range chunks {
		if c.ChunkKind == types.ChunkFunctionSplit {
			splits = append(splits, c)
		}
	}
	if len(splits) < 2 {
		t.Fatalf("expected ≥2 function_split chunks; got %d (chunks=%d)", len(splits), len(chunks))
	}

	// Every split must carry the :chunk:N suffix and a distinct line range.
	seenLines := map[int]bool{}
	for i, c := range splits {
		want := "Big:chunk:" + itoa(i+1)
		if c.SymbolName != want {
			t.Errorf("splits[%d].SymbolName = %q, want %q", i, c.SymbolName, want)
		}
		if c.SymbolKind != types.KindFunction {
			t.Errorf("splits[%d].SymbolKind = %q, want Function", i, c.SymbolKind)
		}
		if c.StartLine < 10 || c.EndLine > 71 {
			t.Errorf("splits[%d] line range out of span: %d-%d (span 10-71)", i, c.StartLine, c.EndLine)
		}
		if seenLines[c.StartLine] {
			t.Errorf("splits[%d] StartLine=%d duplicated; windows must be disjoint", i, c.StartLine)
		}
		seenLines[c.StartLine] = true
		if c.ID == "" {
			t.Errorf("splits[%d] missing chunk_id", i)
		}
	}
	// Splits must be ordered by start_line so reassembly is trivial.
	for i := 1; i < len(splits); i++ {
		if splits[i].StartLine <= splits[i-1].StartLine {
			t.Errorf("splits not in line order: [%d].StartLine=%d ≤ [%d].StartLine=%d",
				i, splits[i].StartLine, i-1, splits[i-1].StartLine)
		}
	}
}

// TestShortFunctionNotSplit verifies the threshold side: a function
// comfortably under MaxInputTokens still goes through the regular
// single-chunk path (ChunkSymbol, no :chunk:N suffix).
func TestShortFunctionNotSplit(t *testing.T) {
	short := "func Small() {\n  return 1\n}"
	chunks := New(Options{MaxInputTokens: 50}).Chunk(Input{
		File: "x.go", Language: "go", CommitHash: "abc",
		Source: []byte("package x\n"),
		Spans: []parse.SymbolSpan{
			{Name: "Small", Kind: types.KindFunction, StartLine: 1, EndLine: 3, Text: short},
		},
	})
	for _, c := range chunks {
		if c.ChunkKind == types.ChunkFunctionSplit {
			t.Errorf("short function should not split; got %+v", c)
		}
		if strings.Contains(c.SymbolName, ":chunk:") {
			t.Errorf("short function should not carry chunk suffix; got %q", c.SymbolName)
		}
	}
}

// TestSplitOnlyForFunctionLikeKinds verifies splitting is restricted
// to Function/Method. Types, structs, interfaces, contracts, and
// markdown sections fall back to truncation — splitting prose loses
// structure and Solidity/TS types are typically short anyway.
func TestSplitOnlyForFunctionLikeKinds(t *testing.T) {
	long := "struct Big {\n" + strings.Repeat("  field T\n", 60) + "}"
	chunks := New(Options{MaxInputTokens: 50}).Chunk(Input{
		File: "x.go", Language: "go", CommitHash: "abc",
		Source: []byte("package x\n"),
		Spans: []parse.SymbolSpan{
			{Name: "BigStruct", Kind: types.KindStruct, StartLine: 1, EndLine: 62, Text: long},
		},
	})
	for _, c := range chunks {
		if c.ChunkKind == types.ChunkFunctionSplit {
			t.Errorf("non-function kind should not split; got %+v", c)
		}
	}
}

// TestSplitChunkIDsAreDistinct ensures every split gets a unique
// chunk_id. Critical for the store's PRIMARY KEY constraint and for
// incremental reindex (DeleteByFile + Upsert sequence).
func TestSplitChunkIDsAreDistinct(t *testing.T) {
	bodyLines := []string{}
	for range 80 {
		bodyLines = append(bodyLines, "  step()")
	}
	long := "func Big() {\n" + strings.Join(bodyLines, "\n") + "\n}"
	chunks := New(Options{MaxInputTokens: 30}).Chunk(Input{
		File: "x.go", Language: "go", CommitHash: "abc",
		Source: []byte("package x\n"),
		Spans: []parse.SymbolSpan{
			{Name: "Big", Kind: types.KindFunction, StartLine: 1, EndLine: 82, Text: long},
		},
	})
	ids := map[string]bool{}
	for _, c := range chunks {
		if ids[c.ID] {
			t.Errorf("duplicate chunk_id: %s", c.ID)
		}
		ids[c.ID] = true
	}
}

// itoa is a tiny helper kept local so the test file doesn't pull in
// strconv just for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := []byte{}
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
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
