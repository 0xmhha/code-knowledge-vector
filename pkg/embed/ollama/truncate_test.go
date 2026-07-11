package ollama

import (
	"math"
	"testing"
)

func TestTruncateNormalize(t *testing.T) {
	// First two components [3,4] have L2 norm 5 → renormalized to [0.6,0.8].
	v := []float32{3, 4, 12, 0}
	out := truncateNormalize(v, 2)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if math.Abs(float64(out[0])-0.6) > 1e-6 || math.Abs(float64(out[1])-0.8) > 1e-6 {
		t.Fatalf("out = %v, want [0.6 0.8]", out)
	}
	var norm float64
	for _, x := range out {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-6 {
		t.Fatalf("truncated vector not unit-normalized: norm=%v", math.Sqrt(norm))
	}

	// dim >= len → returned unchanged (no truncation, no renorm).
	same := truncateNormalize(v, len(v))
	if len(same) != len(v) || same[2] != 12 {
		t.Fatalf("dim>=len should return the input unchanged, got %v", same)
	}
	if got := truncateNormalize(v, 0); len(got) != len(v) {
		t.Fatalf("dim<=0 should return the input unchanged, got len %d", len(got))
	}
}
