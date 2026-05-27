package ckv_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

// buildSampleIndex creates a small mock-embedder index in t.TempDir()
// from the project's testdata/sample fixture. Reused by every test that
// needs a real on-disk index.
func buildSampleIndex(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	src := filepath.Join(repoRoot, "testdata", "sample")
	out := t.TempDir()
	_, err = build.Run(context.Background(), build.Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: ckv.MockEmbedder(),
	})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return out
}

func TestMockEmbedder_Factory(t *testing.T) {
	emb := ckv.MockEmbedder()
	if emb == nil {
		t.Fatal("MockEmbedder() returned nil")
	}
	if emb.Dimension() <= 0 {
		t.Errorf("mock embedder Dimension()=%d, want >0", emb.Dimension())
	}
	if emb.Name() == "" {
		t.Error("mock embedder Name() is empty")
	}
}

func TestOpen_NilEmbedder(t *testing.T) {
	dir := t.TempDir()
	_, err := ckv.Open(dir, ckv.OpenOptions{Embedder: nil})
	if err == nil {
		t.Fatal("expected error for nil Embedder, got nil")
	}
}

func TestOpen_MissingIndex(t *testing.T) {
	_, err := ckv.Open(filepath.Join(t.TempDir(), "nonexistent"),
		ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !errors.Is(err, ckv.ErrIndexUnavailable) {
		t.Errorf("expected ErrIndexUnavailable, got %v", err)
	}
}

func TestOpen_ModelMismatch(t *testing.T) {
	out := buildSampleIndex(t)
	// Different name → mismatch.
	mismatched := ckv.NewMockEmbedder(384, "different-mock")
	_, err := ckv.Open(out, ckv.OpenOptions{Embedder: mismatched})
	if err == nil {
		t.Fatal("expected error for model mismatch")
	}
	if !errors.Is(err, ckv.ErrIndexUnavailable) {
		t.Errorf("expected ErrIndexUnavailable wrapped, got %v", err)
	}
}

func TestSearch_FindsHits(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer engine.Close()

	resp, err := engine.SemanticSearch(context.Background(),
		"server listen function",
		ckv.SearchOptions{K: 5, Threshold: -1},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(resp.Hits) == 0 {
		t.Error("expected at least one hit (mock embedder + sample corpus)")
	}
	for i, h := range resp.Hits {
		if h.Citation.File == "" {
			t.Errorf("hit %d: empty citation file", i)
		}
		if h.Snippet == "" {
			t.Errorf("hit %d: empty snippet", i)
		}
	}
}

func TestSearch_EmptyIntent(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	defer engine.Close()

	_, err := engine.SemanticSearch(context.Background(), "", ckv.SearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestSearch_AfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := engine.SemanticSearch(context.Background(), "x", ckv.SearchOptions{})
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestClose_Idempotent(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err := engine.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Errorf("second Close should be safe, got %v", err)
	}
}

func TestEngine_Warmup(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer engine.Close()
	if err := engine.Warmup(context.Background()); err != nil {
		t.Errorf("Warmup: %v", err)
	}
}

func TestEngine_WarmupAfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	engine.Close()
	if err := engine.Warmup(context.Background()); err == nil {
		t.Fatal("expected Warmup after Close to error")
	}
}

// TestErrorModel_AllSentinelsExported verifies the 6 error variants
// are reachable via pkg/ckv with errors.Is semantics. Forward-compat:
// consumers writing switch errors.Is(...) today must continue to work
// as we wire raise points for SanitizeFailed and PolicyError.
func TestErrorModel_AllSentinelsExported(t *testing.T) {
	sentinels := map[string]error{
		"IndexUnavailable": ckv.ErrIndexUnavailable,
		"FreshnessStale":   ckv.ErrFreshnessStale,
		"BudgetExceeded":   ckv.ErrBudgetExceeded,
		"CitationNotFound": ckv.ErrCitationNotFound,
		"SanitizeFailed":   ckv.ErrSanitizeFailed,
		"PolicyError":      ckv.ErrPolicyError,
	}
	for name, err := range sentinels {
		if err == nil {
			t.Errorf("%s sentinel is nil — must be a non-nil error value", name)
		}
		// Wrap and re-test: errors.Is must roundtrip the wrapped form
		// so callers can format-wrap freely without breaking detection.
		wrapped := errors.Join(err, errors.New("context detail"))
		if !errors.Is(wrapped, err) {
			t.Errorf("%s does not satisfy errors.Is after wrap", name)
		}
	}
}

// TestSearch_BudgetExceeded_PropagatesThroughCKVFacade ensures the
// pkg/ckv surface (not just internal/query) returns ErrBudgetExceeded
// when consumers pass a too-small budget. Catches accidental error
// translation in the wrapper.
func TestSearch_BudgetExceeded_PropagatesThroughCKVFacade(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	defer engine.Close()

	_, err := engine.SemanticSearch(context.Background(), "x",
		ckv.SearchOptions{BudgetTokens: 5})
	if !errors.Is(err, ckv.ErrBudgetExceeded) {
		t.Errorf("expected ErrBudgetExceeded propagated via ckv facade, got %v", err)
	}
}

// TestCheckFreshness_AfterCloseFails ensures CheckFreshness fails
// gracefully on a closed engine (no nil deref).
func TestCheckFreshness_AfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	engine.Close()
	if err := engine.CheckFreshness(); err == nil {
		t.Fatal("expected CheckFreshness after Close to error")
	}
}

func TestEngine_Manifest(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	defer engine.Close()

	man := engine.Manifest()
	if man.EmbeddingModel == "" {
		t.Error("Manifest.EmbeddingModel should not be empty")
	}
	if man.ChunkCount <= 0 {
		t.Errorf("Manifest.ChunkCount=%d, want >0", man.ChunkCount)
	}
}
