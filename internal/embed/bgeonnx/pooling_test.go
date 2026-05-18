package bgeonnx

import (
	"math"
	"testing"
)

// Hand-computed expected values for two batch rows × 3 tokens × 2 hidden
// dims, with the second token of row 1 masked off.
//
//   row 0: tokens [(1,0), (0,1), (1,1)], all attended
//     pooled = mean([(1,0),(0,1),(1,1)]) = (2/3, 2/3)
//     ||·||  = sqrt(8/9) ≈ 0.9428
//     normalized = (2/3 / 0.9428, 2/3 / 0.9428) = (0.7071, 0.7071)
//
//   row 1: tokens [(2,0), (?, ?) masked, (0,2)], mask=[1,0,1]
//     pooled = mean of attended = ((2+0)/2, (0+2)/2) = (1, 1)
//     ||·||  = sqrt(2) ≈ 1.4142
//     normalized = (0.7071, 0.7071)
//
// Both rows L2-normalize to the same unit vector — convenient
// fingerprint for an arithmetic regression.
func TestMeanPoolNormalize_HandComputed(t *testing.T) {
	raw := []float32{
		// row 0
		1, 0,
		0, 1,
		1, 1,
		// row 1
		2, 0,
		99, 99, // masked — must be ignored
		0, 2,
	}
	mask := [][]int64{
		{1, 1, 1},
		{1, 0, 1},
	}
	vecs, err := meanPoolNormalize(raw, mask, 2, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d rows, want 2", len(vecs))
	}
	want := [2]float32{0.7071068, 0.7071068}
	for i, vec := range vecs {
		for j := range vec {
			if diff := math.Abs(float64(vec[j] - want[j])); diff > 1e-5 {
				t.Errorf("row %d dim %d: got %f, want %f (diff %g)", i, j, vec[j], want[j], diff)
			}
		}
	}
}

func TestMeanPoolNormalize_UnitNorm(t *testing.T) {
	// Random-ish input — only invariant we assert is L2 norm == 1
	// for every output row.
	raw := []float32{
		0.1, 0.5, -0.3, 0.8,
		0.7, -0.2, 0.4, 0.1,
	}
	mask := [][]int64{{1, 1}}
	vecs, err := meanPoolNormalize(raw, mask, 1, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, v := range vecs[0] {
		sum += float64(v) * float64(v)
	}
	if diff := math.Abs(sum - 1.0); diff > 1e-5 {
		t.Errorf("L2 norm² = %f, want 1.0 (diff %g)", sum, diff)
	}
}

func TestMeanPoolNormalize_AllZeroMaskRejected(t *testing.T) {
	raw := []float32{1, 2, 3, 4}
	mask := [][]int64{{0, 0}}
	_, err := meanPoolNormalize(raw, mask, 1, 2, 2)
	if err == nil {
		t.Fatal("expected error for all-zero attention row")
	}
}

func TestMeanPoolNormalize_ShapeMismatchRejected(t *testing.T) {
	raw := []float32{1, 2, 3} // 3 floats, but [1, 2, 2] requires 4
	mask := [][]int64{{1, 1}}
	_, err := meanPoolNormalize(raw, mask, 1, 2, 2)
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestCLSPoolNormalize_TakesFirstTokenAndNormalizes(t *testing.T) {
	// 2 rows × 3 tokens × 2 hidden dims. Only token 0 ([CLS]) should
	// matter; the 99s at later positions exist purely to fail loud if
	// the implementation accidentally averages or sums anything past
	// position 0.
	raw := []float32{
		// row 0: CLS=(3,4) → normalized = (0.6, 0.8)
		3, 4,
		99, 99,
		99, 99,
		// row 1: CLS=(1,0) → already unit length
		1, 0,
		99, 99,
		99, 99,
	}
	vecs, err := clsPoolNormalize(raw, 2, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d rows, want 2", len(vecs))
	}
	wantRow0 := [2]float32{0.6, 0.8}
	for i, v := range vecs[0] {
		if math.Abs(float64(v-wantRow0[i])) > 1e-5 {
			t.Errorf("row 0 dim %d: got %f, want %f", i, v, wantRow0[i])
		}
	}
	wantRow1 := [2]float32{1.0, 0.0}
	for i, v := range vecs[1] {
		if math.Abs(float64(v-wantRow1[i])) > 1e-5 {
			t.Errorf("row 1 dim %d: got %f, want %f", i, v, wantRow1[i])
		}
	}
}

func TestCLSPoolNormalize_UnitNorm(t *testing.T) {
	// Any non-zero CLS hidden state must produce a unit-norm output.
	raw := []float32{
		0.3, -0.4, 0.7, 0.1, // CLS of row 0
		77, 77, 77, 77, // ignored
	}
	vecs, err := clsPoolNormalize(raw, 1, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, v := range vecs[0] {
		sum += float64(v) * float64(v)
	}
	if diff := math.Abs(sum - 1.0); diff > 1e-5 {
		t.Errorf("L2 norm² = %f, want 1.0 (diff %g)", sum, diff)
	}
}

func TestCLSPoolNormalize_ShapeMismatchRejected(t *testing.T) {
	raw := []float32{1, 2, 3} // 3 floats, but [1, 2, 2] requires 4
	_, err := clsPoolNormalize(raw, 1, 2, 2)
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
}
