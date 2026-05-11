package mock

import (
	"context"
	"math"
	"testing"
)

func TestDeterministic(t *testing.T) {
	e := Default()
	a, _ := e.Embed(context.Background(), []string{"hello world"})
	b, _ := e.Embed(context.Background(), []string{"hello world"})
	if !equalSlices(a[0], b[0]) {
		t.Fatal("same input must produce same vector")
	}
}

func TestDimensionMatches(t *testing.T) {
	e := New(128, "")
	out, _ := e.Embed(context.Background(), []string{"x"})
	if len(out[0]) != 128 {
		t.Fatalf("expected dim 128, got %d", len(out[0]))
	}
}

func TestSimilarTextsAreSimilar(t *testing.T) {
	e := New(256, "")
	v, _ := e.Embed(context.Background(), []string{
		"connection pool initialization",
		"connect pool init",
		"deserialize protobuf message",
	})
	simAB := cosine(v[0], v[1])
	simAC := cosine(v[0], v[2])
	if !(simAB > simAC) {
		t.Errorf("similar texts must be more similar than unrelated: sim(A,B)=%f, sim(A,C)=%f", simAB, simAC)
	}
}

func TestVectorsAreL2Normalized(t *testing.T) {
	e := Default()
	v, _ := e.Embed(context.Background(), []string{"any text here"})
	var sum float64
	for _, x := range v[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-6 {
		t.Errorf("L2 norm should be 1, got %f", math.Sqrt(sum))
	}
}

func equalSlices(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cosine(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot // already L2-normalized → dot = cosine similarity
}
