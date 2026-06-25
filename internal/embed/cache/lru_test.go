package cache

import (
	"context"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

type fakeEmbedder struct {
	name   string
	dim    int
	calls  int
	answer func(s string) []float32
}

func (f *fakeEmbedder) Name() string        { return f.name }
func (f *fakeEmbedder) Dimension() int      { return f.dim }
func (f *fakeEmbedder) MaxInputTokens() int { return 0 }
func (f *fakeEmbedder) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{Provider: "fake", Model: f.name, Dim: f.dim}
}
func (f *fakeEmbedder) Embed(_ context.Context, batch []string) ([][]float32, error) {
	f.calls += len(batch)
	out := make([][]float32, len(batch))
	for i, s := range batch {
		out[i] = f.answer(s)
	}
	return out, nil
}

func mkEmb() *fakeEmbedder {
	return &fakeEmbedder{
		name: "fake", dim: 4,
		answer: func(s string) []float32 {
			return []float32{float32(len(s)), 0, 0, 0}
		},
	}
}

func TestCache_CacheHitOnSecondCall(t *testing.T) {
	inner := mkEmb()
	c := New(inner, 10)

	if _, err := c.Embed(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.Embed(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("inner called %d times, want 1 (cache should serve the second)", inner.calls)
	}
	h, m, _ := c.Stats()
	if h != 1 || m != 1 {
		t.Errorf("Stats: hits=%d miss=%d, want 1/1", h, m)
	}
}

func TestCache_BatchMixedHitsAndMisses(t *testing.T) {
	inner := mkEmb()
	c := New(inner, 10)
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inner.calls = 0

	// Mix: "a" hit, "c" miss, "b" hit. Inner gets only "c".
	out, err := c.Embed(context.Background(), []string{"a", "c", "b"})
	if err != nil {
		t.Fatalf("mix: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 vectors, got %d", len(out))
	}
	if inner.calls != 1 {
		t.Errorf("inner should be called only for miss 'c', got %d", inner.calls)
	}
	// Vector for "a" is correct (length 1).
	if out[0][0] != 1 {
		t.Errorf("cache returned wrong vector for 'a': %v", out[0])
	}
}

func TestCache_EvictsOldestOnOverflow(t *testing.T) {
	inner := mkEmb()
	c := New(inner, 2)
	_, _ = c.Embed(context.Background(), []string{"x", "y"})
	_, _ = c.Embed(context.Background(), []string{"z"}) // evicts 'x'

	inner.calls = 0
	// Asking for 'x' again should miss → inner call.
	_, _ = c.Embed(context.Background(), []string{"x"})
	if inner.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (x was evicted)", inner.calls)
	}
}

func TestCache_ForwardsName(t *testing.T) {
	c := New(&fakeEmbedder{name: "x", dim: 4, answer: func(string) []float32 { return nil }}, 1)
	if c.Name() != "x" {
		t.Errorf("Name forwarding broken")
	}
	if c.Dimension() != 4 {
		t.Errorf("Dimension forwarding broken")
	}
}

func TestCache_DefaultSize(t *testing.T) {
	c := New(mkEmb(), 0)
	if c.size != DefaultSize {
		t.Errorf("size = %d, want default %d", c.size, DefaultSize)
	}
}

var _ types.Embedder = (*Cached)(nil) // compile-time interface check
