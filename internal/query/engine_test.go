package query

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// buildSample is the shared fixture for query E2E tests: it builds an
// index over testdata/sample with the deterministic mock embedder and
// returns the OutDir + absolute src root. Both paths are inside
// t.TempDir() / repo so they stay valid for citation enforcement.
func buildSample(t *testing.T) (out, src string) {
	t.Helper()
	// internal/query/engine_test.go → ../../testdata/sample
	srcAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	_, err = build.Run(context.Background(), build.Options{
		SrcRoot:  srcAbs,
		OutDir:   outDir,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return outDir, srcAbs
}

func TestOpenRejectsDimMismatch(t *testing.T) {
	out, _ := buildSample(t)
	// mock.Default() is 64-dim; instantiating with 128 must fail.
	emb := mock.New(128, "mock-feature-hash-v1")
	_, err := Open(out, emb)
	if !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected ErrIndexUnavailable, got %v", err)
	}
}

func TestOpenRejectsModelMismatch(t *testing.T) {
	out, _ := buildSample(t)
	emb := mock.New(64, "different-model-name")
	_, err := Open(out, emb)
	if !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("expected ErrIndexUnavailable, got %v", err)
	}
}

func TestSearchFindsListenForTCPQuery(t *testing.T) {
	out, _ := buildSample(t)
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer eng.Close()

	res, err := eng.Search(context.Background(), "TCP socket bind on port", Options{K: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Verify *some* hit in top-3 cites server.go and the line range
	// brackets the Listen method (lines 22-29 in our fixture).
	var found bool
	for _, h := range res.Hits {
		if h.Citation.File == "server.go" && h.Citation.StartLine <= 22 && h.Citation.EndLine >= 22 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a hit citing server.go bracketing line 22, got %+v", res.Hits)
	}
	for _, h := range res.Hits {
		if h.Score.Normalized < DefaultThreshold {
			t.Errorf("hit below threshold leaked: %+v", h.Score)
		}
		if h.ChunkID == "" || h.Citation.File == "" {
			t.Errorf("missing identity on hit: %+v", h)
		}
	}
}

func TestSearchHonorsFilter(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	res, err := eng.Search(context.Background(), "lock map mutex",
		Options{K: 5, Filter: types.Filter{SymbolKinds: []types.SymbolKind{types.KindMethod}}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range res.Hits {
		if h.SymbolKind != types.KindMethod {
			t.Errorf("Method filter not honored: %s/%s", h.Language, h.SymbolKind)
		}
	}
}

func TestSearchEmptyIntentRejected(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	if _, err := eng.Search(context.Background(), "", Options{}); err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestThresholdDropEmitsWarning(t *testing.T) {
	out, _ := buildSample(t)
	eng, _ := Open(out, mock.Default())
	defer eng.Close()

	// Threshold 0.999 will reject everything.
	res, err := eng.Search(context.Background(), "anything", Options{Threshold: 0.999})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(res.Hits))
	}
	var sawWarning bool
	for _, w := range res.Warnings {
		if w == "all_results_below_threshold" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Errorf("expected all_results_below_threshold warning, got %v", res.Warnings)
	}
}
