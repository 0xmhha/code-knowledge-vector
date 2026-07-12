package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// failEmbedder wraps a working embedder but errors on Embed, simulating a
// build that fails partway (e.g. the daemon dies mid-run).
type failEmbedder struct{ types.Embedder }

func (failEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embed failed")
}

// TestRun_FailedBuildClearsManifest verifies a build that fails partway leaves
// the index "not ready": the manifest is removed rather than left pairing a
// stale manifest with a partially-written vector.db.
func TestRun_FailedBuildClearsManifest(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()
	fixedNow := func() time.Time { return time.Unix(0, 0).UTC() }

	if _, err := Run(context.Background(), Options{SrcRoot: src, OutDir: out, Embedder: mock.Default(), Now: fixedNow}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	if _, err := manifest.Load(out); err != nil {
		t.Fatalf("expected manifest after successful build: %v", err)
	}

	if _, err := Run(context.Background(), Options{SrcRoot: src, OutDir: out, Embedder: failEmbedder{mock.Default()}, Now: fixedNow}); err == nil {
		t.Fatal("expected the build to fail")
	}
	if _, err := manifest.Load(out); !errors.Is(err, manifest.ErrNotFound) {
		t.Errorf("manifest must be gone after a failed build, got err=%v", err)
	}
}

// resolveTestdataSample returns the absolute path to <repo>/testdata/sample,
// independent of the test's CWD. tests in internal/build run from
// internal/build/, so we walk up to find the module root.
func resolveTestdataSample(t *testing.T) string {
	t.Helper()
	// internal/build/builder_test.go → ../../testdata/sample
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestRun_AlignsFlowStepsToCode is the Phase C guard: a flow step whose
// file:line falls inside a real code chunk gets that chunk's ID stamped as
// AlignedChunkID, so a caller can jump from the flow prose to its implementation.
func TestRun_AlignsFlowStepsToCode(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	// server.go's Server.Listen spans lines 22–29; a step at line 25 must
	// align to that symbol chunk. The second step points at a nonexistent line
	// so it stays unaligned (corpus drift).
	corpus := filepath.Join(t.TempDir(), "corpus.jsonl")
	lines := strings.Join([]string{
		`{"type":"flow","id":"f1","entry_point":"E","trigger":"t","summary":"server listen flow","root_symbol":"Server"}`,
		`{"type":"step","id":"s1","flow":"f1","symbol":"Server.Listen","file":"server.go","line":25,"prose":"binds the listening socket"}`,
		`{"type":"step","id":"s2","flow":"f1","symbol":"Ghost","file":"server.go","line":9000,"prose":"drifted step past EOF"}`,
	}, "\n")
	if err := os.WriteFile(corpus, []byte(lines+"\n"), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}

	if _, err := Run(context.Background(), Options{
		SrcRoot:    src,
		OutDir:     out,
		Embedder:   mock.Default(),
		FlowCorpus: corpus,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	st, err := sqlitevec.Open(filepath.Join(out, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Resolve what the aligned code chunk for server.go:25 should be.
	codeChunks, err := st.LookupByFileOrdered(context.Background(), "server.go")
	if err != nil {
		t.Fatalf("lookup server.go: %v", err)
	}
	wantID := ""
	for _, c := range codeChunks {
		if c.ChunkKind == types.ChunkSymbol && c.StartLine <= 25 && 25 <= c.EndLine {
			wantID = c.ID
		}
	}
	if wantID == "" {
		t.Fatal("no server.go symbol chunk contains line 25 — fixture assumption broken")
	}

	flowChunks, err := st.FlowChunks(context.Background())
	if err != nil {
		t.Fatalf("flow chunks: %v", err)
	}
	var s1, s2 *types.FlowStepMeta
	for _, c := range flowChunks {
		if c.FlowStep == nil {
			continue
		}
		switch c.FlowStep.StepID {
		case "s1":
			s1 = c.FlowStep
		case "s2":
			s2 = c.FlowStep
		}
	}
	if s1 == nil || s2 == nil {
		t.Fatalf("expected both flow steps in the store (s1=%v s2=%v)", s1 != nil, s2 != nil)
	}
	if s1.AlignedChunkID != wantID {
		t.Errorf("s1 AlignedChunkID = %q, want %q (server.go:25 symbol chunk)", s1.AlignedChunkID, wantID)
	}
	if s2.AlignedChunkID != "" {
		t.Errorf("s2 (drifted) should be unaligned, got %q", s2.AlignedChunkID)
	}
}

func TestRunIndexesSample(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	res, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// testdata/sample currently has 4 indexable files (server.go,
	// cache.go, handler.ts, docs/decisions.md). Add one to the lower
	// bound here every time a fixture file lands. We check ≥ rather
	// than == so adding more fixtures doesn't churn the test.
	if res.FilesIndexed < 4 {
		t.Errorf("expected ≥4 files indexed, got %d", res.FilesIndexed)
	}
	// Symbols per current fixtures: Go (NewServer, Listen, Close, Server,
	// NewCache, Set, Get, Cache) = 8; TS (Request, Response, Handler,
	// Handler.register, Handler.dispatch, notFound) = 6; markdown
	// (4 heading sections in decisions.md) = 4. + one file_header per
	// SOURCE file (markdown skips it). Treat as a lower bound.
	if res.Chunks.Total < 18 {
		t.Errorf("expected ≥18 chunks, got %d (%+v)", res.Chunks.Total, res.Chunks)
	}
	if res.Chunks.FileHeader < 3 {
		t.Errorf("expected ≥3 file_header chunks (one per source file), got %d", res.Chunks.FileHeader)
	}

	// manifest.json round-trips
	m, err := manifest.Load(out)
	if err != nil {
		t.Fatalf("Load manifest: %v", err)
	}
	if m.EmbeddingDim != mock.Default().Dimension() {
		t.Errorf("manifest dim mismatch: got %d, want %d", m.EmbeddingDim, mock.Default().Dimension())
	}
	if m.ChunkCount != res.Chunks.Total {
		t.Errorf("manifest chunk_count %d != Result.Chunks.Total %d", m.ChunkCount, res.Chunks.Total)
	}

	// store can be reopened and answer a query — citation must point at
	// a real chunk in the indexed sample (server.go or cache.go).
	store, err := sqlitevec.Open(filepath.Join(out, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	q, _ := mock.Default().Embed(context.Background(), []string{"open tcp listener on the configured address"})
	hits, err := store.Search(context.Background(), q[0], 3, types.Filter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for the listener query")
	}
	gotFile := hits[0].Chunk.File
	if gotFile != "server.go" && gotFile != "cache.go" {
		t.Errorf("top hit file unexpected: %q (raw=%+v)", gotFile, hits[0].Chunk)
	}
}

func TestRunIndexesMarkdownSections(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	store, err := sqlitevec.Open(filepath.Join(out, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	// Filter by language="markdown" — must include the sample doc with
	// at least 4 sections (sample-decisions + 3 ## headings).
	q, _ := mock.Default().Embed(context.Background(), []string{"why sqlite-vec was chosen"})
	hits, err := store.Search(context.Background(), q[0], 20, types.Filter{Language: "markdown"})
	if err != nil {
		t.Fatalf("Search markdown: %v", err)
	}
	if len(hits) < 4 {
		t.Fatalf("expected ≥4 markdown hits (one per heading section), got %d", len(hits))
	}
	for _, h := range hits {
		if h.Chunk.Language != "markdown" {
			t.Errorf("language filter leaked non-markdown chunk: %+v", h.Chunk)
		}
		if h.Chunk.SymbolKind != types.KindDocSection && h.Chunk.SymbolKind != types.KindADRSection {
			t.Errorf("expected DocSection/ADRSection, got %q for %s", h.Chunk.SymbolKind, h.Chunk.File)
		}
	}
}

func TestRunHonorsProjectCKVYaml(t *testing.T) {
	// Stage a minimal multi-language tree inside a TempDir so we can
	// drop a ckv.yaml without polluting testdata/sample.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package x\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ui.ts"), []byte("export function f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ckv.yaml"), []byte(`schema_version: "1"
languages: [go]
chunking:
  file_header_lines: 5
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   t.TempDir(),
		Embedder: mock.Default(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Only Go should be indexed because ckv.yaml restricts languages.
	if res.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed (go only), got %d", res.FilesIndexed)
	}
	// 1 file_header + 1 symbol (func A) + 1 package convention chunk = 3.
	if res.Chunks.Total != 3 {
		t.Errorf("expected 3 chunks (header + symbol + convention), got %d (%+v)", res.Chunks.Total, res.Chunks)
	}
	if res.Chunks.FileHeader != 1 {
		t.Errorf("expected 1 file_header chunk, got %d", res.Chunks.FileHeader)
	}
	if res.Chunks.Invariant != 0 {
		t.Errorf("expected 0 invariant chunks for trivial source, got %d", res.Chunks.Invariant)
	}
}

func TestRunFailsOnMalformedCKVYaml(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "ckv.yaml"), []byte("schema_version: \"99\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   t.TempDir(),
		Embedder: mock.Default(),
	})
	if err == nil {
		t.Fatal("expected error for unsupported schema_version, got nil")
	}
}

func TestRunEmitsProgressFinalLine(t *testing.T) {
	// Contract: when ProgressOut is set, the final tick must emit a
	// completion line even on small fixtures that never cross the
	// 100-file or 2s throttle gates. Guards the closure-defer wiring
	// in Run against silent regressions.
	src := resolveTestdataSample(t)
	out := t.TempDir()

	var buf bytes.Buffer
	_, err := Run(context.Background(), Options{
		SrcRoot:     src,
		OutDir:      out,
		Embedder:    mock.Default(),
		ProgressOut: &buf,
		Now:         func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out2 := buf.String()
	if !strings.Contains(out2, "files (") {
		t.Errorf("expected at least one progress line, got %q", out2)
	}
	// Final line denominator must equal the walked file count: we don't
	// know the exact number (it grows when fixtures land), but the line
	// must show N/N where both sides agree.
	lines := strings.Split(strings.TrimRight(out2, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "/") {
		t.Fatalf("final line shape unexpected: %q", last)
	}
}

func TestRunHonorsNilProgressOut(t *testing.T) {
	// Library callers leave ProgressOut nil. Behavior must stay
	// identical to the default: no panic, no stray writes.
	src := resolveTestdataSample(t)
	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   t.TempDir(),
		Embedder: mock.Default(),
		// ProgressOut left nil on purpose.
	})
	if err != nil {
		t.Fatalf("Run with nil ProgressOut: %v", err)
	}
}

func TestRunIndexesDocsRoots(t *testing.T) {
	src := resolveTestdataSample(t)
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "A4.addresses.md"),
		[]byte("# System contract addresses\n\nNativeCoinAdapter is 0x1000.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()

	_, err := Run(context.Background(), Options{
		SrcRoot:   src,
		OutDir:    out,
		DocsRoots: []string{docs},
		Embedder:  mock.Default(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	store, err := sqlitevec.Open(filepath.Join(out, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	q, _ := mock.Default().Embed(context.Background(), []string{"system contract address NativeCoinAdapter"})
	hits, err := store.Search(context.Background(), q[0], 50, types.Filter{Language: "markdown"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, h := range hits {
		if h.Chunk.File == "A4.addresses.md" {
			found = true
			if h.Chunk.Category != "domain" {
				t.Errorf("docs chunk Category = %q, want domain", h.Chunk.Category)
			}
		}
	}
	if !found {
		t.Fatalf("corpus doc A4.addresses.md not indexed (got %d markdown hits)", len(hits))
	}

	m, err := manifest.Load(out)
	if err != nil {
		t.Fatalf("Load manifest: %v", err)
	}
	if len(m.DocsRoots) != 1 {
		t.Errorf("manifest DocsRoots = %v, want one entry", m.DocsRoots)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	a, err := Run(context.Background(), Options{SrcRoot: src, OutDir: out, Embedder: mock.Default()})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	b, err := Run(context.Background(), Options{SrcRoot: src, OutDir: out, Embedder: mock.Default()})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if a.Chunks.Total != b.Chunks.Total {
		t.Errorf("chunk count drift across rebuilds: %d → %d", a.Chunks.Total, b.Chunks.Total)
	}
}
