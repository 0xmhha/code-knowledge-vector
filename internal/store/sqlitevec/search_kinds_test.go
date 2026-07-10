package sqlitevec

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// mkKindChunk is mkChunk with an explicit ChunkKind.
func mkKindChunk(id, file, text string, kind types.ChunkKind) types.Chunk {
	c := mkChunk(id, file, text, 1, 5, "go", types.KindFunction)
	c.ChunkKind = kind
	return c
}

func TestSearchWithinKinds_FindsRareKindsKNNWouldMiss(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vector.db")
	s, err := Open(dbPath, testDim)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// Many code chunks near the query, one invariant chunk far from it.
	// A KNN + post-filter path returns zero for ChunkKinds{invariant};
	// the kind-scoped exact scan must return the invariant regardless of
	// how many code chunks outrank it.
	var chunks []types.Chunk
	var embs [][]float32
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		chunks = append(chunks, mkKindChunk("code-"+id, "x.go", "code "+id, types.ChunkSymbol))
		embs = append(embs, []float32{1, 0, 0, 0})
	}
	chunks = append(chunks,
		mkKindChunk("inv-1", "docs/invariants.md", "empty block same state root", types.ChunkInvariant),
		mkKindChunk("conv-1", "docs/conventions.md", "error wrapping convention", types.ChunkConvention),
	)
	embs = append(embs, []float32{0, 1, 0, 0}, []float32{0, 0, 1, 0})
	if err := s.Upsert(ctx, chunks, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	query := []float32{0.9, 0.1, 0, 0} // nearest = every code chunk
	got, err := s.Search(ctx, query, 2, types.Filter{
		ChunkKinds: []types.ChunkKind{types.ChunkInvariant, types.ChunkConvention},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("hits = %d, want 2 (both knowledge chunks)", len(got))
	}
	// inv-1's embedding is nearer to the query than conv-1's.
	if got[0].Chunk.ID != "inv-1" || got[1].Chunk.ID != "conv-1" {
		t.Errorf("order = %s,%s want inv-1,conv-1", got[0].Chunk.ID, got[1].Chunk.ID)
	}
	if got[0].Score.VectorRank != 1 || got[1].Score.VectorRank != 2 {
		t.Errorf("ranks not 1-based monotonic: %+v, %+v", got[0].Score, got[1].Score)
	}
	// Distance must match vec0's metric: Euclidean. |(0.9,0.1)-(0,1)| = sqrt(0.81+0.81).
	wantDist := math.Sqrt(0.81 + 0.81)
	if d := got[0].Score.VectorDistance; math.Abs(d-wantDist) > 1e-6 {
		t.Errorf("VectorDistance = %v, want %v (euclidean)", d, wantDist)
	}
	if got[0].Score.Normalized <= got[1].Score.Normalized {
		t.Errorf("normalized not rank-monotone: %v then %v", got[0].Score.Normalized, got[1].Score.Normalized)
	}
}

func TestSearchWithinKinds_RespectsOtherFilterFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vector.db")
	s, err := Open(dbPath, testDim)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	a := mkKindChunk("inv-go", "docs/a.md", "go rule", types.ChunkInvariant)
	b := mkKindChunk("inv-ts", "docs/b.md", "ts rule", types.ChunkInvariant)
	b.Language = "typescript"
	if err := s.Upsert(ctx, []types.Chunk{a, b}, [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Search(ctx, []float32{1, 0, 0, 0}, 5, types.Filter{
		ChunkKinds: []types.ChunkKind{types.ChunkInvariant},
		Language:   "typescript",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Chunk.ID != "inv-ts" {
		t.Fatalf("got %+v, want only inv-ts (Language filter must still apply)", got)
	}
}
