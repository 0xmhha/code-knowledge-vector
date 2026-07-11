package query

import (
	"context"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// recordingQE wraps an Embedder and records whether EmbedQuery was taken.
type recordingQE struct {
	types.Embedder
	calledQuery bool
}

func (r *recordingQE) EmbedQuery(ctx context.Context, queries []string) ([][]float32, error) {
	r.calledQuery = true
	return r.Embed(ctx, queries)
}

// TestEmbedQueryBatch_RoutesToQueryEmbedder verifies the query path uses
// EmbedQuery when the embedder implements QueryEmbedder, and falls back to
// Embed otherwise.
func TestEmbedQueryBatch_RoutesToQueryEmbedder(t *testing.T) {
	base := mock.Default()

	qe := &recordingQE{Embedder: base}
	if _, err := embedQueryBatch(context.Background(), qe, []string{"q"}); err != nil {
		t.Fatalf("embedQueryBatch(QueryEmbedder): %v", err)
	}
	if !qe.calledQuery {
		t.Fatalf("expected EmbedQuery to be used for a QueryEmbedder")
	}

	// A plain Embedder must fall back to Embed and still return a vector.
	vecs, err := embedQueryBatch(context.Background(), base, []string{"q"})
	if err != nil {
		t.Fatalf("embedQueryBatch(plain Embedder): %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		t.Fatalf("fallback returned no vector: %v", vecs)
	}
}
