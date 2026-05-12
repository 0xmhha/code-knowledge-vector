package build

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

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
	// testdata/sample currently has 3 source files (server.go, cache.go,
	// handler.ts). Add one to the lower bound here every time a fixture
	// file lands. We check ≥ rather than == so adding more fixtures
	// doesn't churn the test.
	if res.FilesIndexed < 3 {
		t.Errorf("expected ≥3 files indexed, got %d", res.FilesIndexed)
	}
	// Symbols per current fixtures: Go (NewServer, Listen, Close, Server,
	// NewCache, Set, Get, Cache) = 8; TS (Request, Response, Handler,
	// Handler.register, Handler.dispatch, notFound) = 6. + one file_header
	// per file. Treat as a lower bound.
	if res.Chunks.Total < 14 {
		t.Errorf("expected ≥14 chunks, got %d (%+v)", res.Chunks.Total, res.Chunks)
	}
	if res.Chunks.FileHeader < 3 {
		t.Errorf("expected ≥3 file_header chunks (one per file), got %d", res.Chunks.FileHeader)
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
