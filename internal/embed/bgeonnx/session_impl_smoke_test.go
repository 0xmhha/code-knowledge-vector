//go:build bgeonnx && bgeonnx_smoke

// Smoke test for the production ONNX session. Skipped unless the
// model.onnx is on disk — see docs/d1-installation-guide.md.
// Run: go test -tags 'bgeonnx bgeonnx_smoke' ./internal/embed/bgeonnx/

package bgeonnx

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestONNXSessionSmoke_EmbedShape(t *testing.T) {
	dir, cfg := defaultModelDir(t) // defined in tokenizer_impl_smoke_test.go
	if _, err := os.Stat(filepath.Join(dir, cfg.OnnxFile)); err != nil {
		t.Skipf("%s not installed at %s — see docs/d1-installation-guide.md", cfg.OnnxFile, dir)
	}

	a, err := Open(Options{ModelDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	vecs, err := a.Embed(context.Background(), []string{
		"def fetch_user(id): return db.users.get(id)",
		"function fetchUser(id) { return db.users.get(id); }",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != cfg.Dim {
			t.Errorf("row %d: dim %d, want %d", i, len(v), cfg.Dim)
		}
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if diff := math.Abs(math.Sqrt(norm) - 1.0); diff > 1e-4 {
			t.Errorf("row %d: L2 norm = %f, want 1.0", i, math.Sqrt(norm))
		}
	}

	// Semantic sanity: Python and JS implementations of "fetch user
	// by id" should be much closer than either is to a random string.
	// We don't assert a hard threshold (depends on the model), just
	// that cosine(v0, v1) > 0.5 — well above what mock-hash would
	// produce for these unrelated lexical surfaces.
	cos := cosine(vecs[0], vecs[1])
	if cos < 0.5 {
		t.Errorf("semantic similarity Python↔JS = %f, expected > 0.5 for paraphrase", cos)
	}
}

func cosine(a, b []float32) float32 {
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot // both already L2-normalized → cosine == dot
}
