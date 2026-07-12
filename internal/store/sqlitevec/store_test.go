package sqlitevec

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

const testDim = 4

// mkChunk is a tiny helper for fixture chunks. start/end are line numbers;
// text drives ContentSHA256 (chunk_id determinism is verified in pkg/types).
func mkChunk(id, file, text string, start, end int, lang string, kind types.SymbolKind) types.Chunk {
	return types.Chunk{
		ID:            id,
		File:          file,
		StartLine:     start,
		EndLine:       end,
		Language:      lang,
		SymbolName:    "fn",
		SymbolKind:    kind,
		ChunkKind:     types.ChunkSymbol,
		CommitHash:    "deadbeef",
		ContentSHA256: types.ContentSHA256(text),
		Text:          text,
	}
}

func TestStoreUpsertAndSearch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vector.db")
	s, err := Open(dbPath, testDim)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	chunks := []types.Chunk{
		mkChunk("a", "x.go", "alpha", 1, 5, "go", types.KindFunction),
		mkChunk("b", "y.go", "beta", 1, 8, "go", types.KindMethod),
		mkChunk("c", "z.ts", "gamma", 1, 3, "typescript", types.KindFunction),
	}
	embs := [][]float32{
		{1, 0, 0, 0}, // closest to query {0.9, 0.1, 0, 0}
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	if err := s.Upsert(ctx, chunks, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Search(ctx, []float32{0.9, 0.1, 0, 0}, 3, types.Filter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(got))
	}
	if got[0].Chunk.ID != "a" {
		t.Errorf("expected closest chunk 'a' first, got %s (distance %f)",
			got[0].Chunk.ID, got[0].Score.VectorDistance)
	}
	if got[0].Score.VectorRank != 1 || got[1].Score.VectorRank != 2 {
		t.Errorf("rank not 1-based monotonic: %+v", got)
	}
	if got[0].Score.Normalized <= got[2].Score.Normalized {
		t.Errorf("normalized score must rank-monotone: got %v then %v",
			got[0].Score.Normalized, got[2].Score.Normalized)
	}
}

func TestStoreFilterByLanguage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v.db")
	s, _ := Open(dbPath, testDim)
	defer s.Close()
	ctx := context.Background()

	chunks := []types.Chunk{
		mkChunk("a", "x.go", "alpha", 1, 5, "go", types.KindFunction),
		mkChunk("b", "y.ts", "beta", 1, 8, "typescript", types.KindFunction),
	}
	embs := [][]float32{{1, 0, 0, 0}, {0.9, 0.1, 0, 0}}
	if err := s.Upsert(ctx, chunks, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Search(ctx, []float32{1, 0, 0, 0}, 5, types.Filter{Language: "typescript"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Chunk.ID != "b" {
		t.Fatalf("expected only ts chunk 'b', got %+v", got)
	}
}

func TestDeleteDocsChunks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v.db")
	s, _ := Open(dbPath, testDim)
	defer s.Close()
	ctx := context.Background()

	// Curated docs layer (chunk_kind=doc, category=domain) alongside a
	// symbol chunk and an in-tree doc chunk (category=""). Only the curated
	// docs layer must be deleted.
	doc1 := mkChunk("d1", "corpus/a.md", "domain doc one", 1, 3, "markdown", types.KindFunction)
	doc1.ChunkKind, doc1.Category = types.ChunkDoc, "domain"
	doc2 := mkChunk("d2", "corpus/b.md", "domain doc two", 1, 3, "markdown", types.KindFunction)
	doc2.ChunkKind, doc2.Category = types.ChunkDoc, "domain"
	intreeDoc := mkChunk("d3", "README.md", "in-tree doc", 1, 3, "markdown", types.KindFunction)
	intreeDoc.ChunkKind, intreeDoc.Category = types.ChunkDoc, "" // NOT domain → keep
	sym := mkChunk("s1", "x.go", "code", 1, 5, "go", types.KindFunction)

	embs := [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
	if err := s.Upsert(ctx, []types.Chunk{doc1, doc2, intreeDoc, sym}, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	n, err := s.DeleteDocsChunks(ctx)
	if err != nil {
		t.Fatalf("DeleteDocsChunks: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted %d docs chunks, want 2 (curated domain only)", n)
	}

	remaining, _ := s.DocsChunks(ctx)
	if len(remaining) != 0 {
		t.Fatalf("DocsChunks after delete = %d, want 0", len(remaining))
	}
	// The symbol and in-tree doc chunks must survive.
	st, _ := s.Stats(ctx)
	if st.ChunkCount != 2 {
		t.Fatalf("remaining chunks = %d, want 2 (symbol + in-tree doc)", st.ChunkCount)
	}
}

func TestStoreDeleteByFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v.db")
	s, _ := Open(dbPath, testDim)
	defer s.Close()
	ctx := context.Background()

	chunks := []types.Chunk{
		mkChunk("a1", "x.go", "alpha1", 1, 5, "go", types.KindFunction),
		mkChunk("a2", "x.go", "alpha2", 6, 10, "go", types.KindFunction),
		mkChunk("b", "y.go", "beta", 1, 8, "go", types.KindMethod),
	}
	embs := [][]float32{{1, 0, 0, 0}, {0.7, 0.7, 0, 0}, {0, 1, 0, 0}}
	if err := s.Upsert(ctx, chunks, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := s.DeleteByFile(ctx, "x.go"); err != nil {
		t.Fatalf("DeleteByFile: %v", err)
	}
	st, _ := s.Stats(ctx)
	if st.ChunkCount != 1 {
		t.Errorf("expected 1 remaining chunk, got %d", st.ChunkCount)
	}
	got, _ := s.Search(ctx, []float32{1, 0, 0, 0}, 5, types.Filter{})
	for _, h := range got {
		if h.Chunk.File == "x.go" {
			t.Errorf("chunk from deleted file leaked: %s", h.Chunk.ID)
		}
	}
}

func TestReopenRejectsDimMismatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v.db")
	s, err := Open(dbPath, 4)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s.Close()

	if _, err := Open(dbPath, 8); err == nil {
		t.Fatal("expected dim-mismatch error, got nil")
	}
}

func TestStatsReflectsManifest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v.db")
	s, _ := Open(dbPath, testDim)
	defer s.Close()
	ctx := context.Background()

	_ = s.SetManifest(ctx, map[string]string{
		"embedding_model": "bge-large-en-v1.5",
		"indexed_head":    "abc123",
		"built_at":        "2026-05-08T12:00:00Z",
	})
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.EmbeddingModel != "bge-large-en-v1.5" || st.IndexedHead != "abc123" || st.EmbeddingDim != testDim {
		t.Errorf("manifest not surfaced: %+v", st)
	}
}
