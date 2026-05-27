package bgeonnx

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/registry"
)

func TestRegistry_DefaultModelExists(t *testing.T) {
	cfg, err := registry.Lookup(registry.DefaultModelName)
	if err != nil {
		t.Fatalf("DefaultModelName %q must be in registry: %v", registry.DefaultModelName, err)
	}
	if cfg.Name != registry.DefaultModelName {
		t.Errorf("registry mismatch: lookup %q got Name=%q", registry.DefaultModelName, cfg.Name)
	}
}

func TestRegistry_EntryShape(t *testing.T) {
	for _, cfg := range registry.List() {
		t.Run(cfg.Name, func(t *testing.T) {
			if cfg.Dim <= 0 {
				t.Errorf("Dim must be > 0, got %d", cfg.Dim)
			}
			if cfg.MaxInput <= 0 {
				t.Errorf("MaxInput must be > 0, got %d", cfg.MaxInput)
			}
			if cfg.OnnxFile == "" {
				t.Error("OnnxFile must not be empty")
			}
			if cfg.TokenizerFile == "" {
				t.Error("TokenizerFile must not be empty")
			}
			if len(cfg.InputOrder) == 0 {
				t.Error("InputOrder must not be empty")
			}
			if len(cfg.Outputs) == 0 {
				t.Error("Outputs must not be empty")
			}
			for _, in := range cfg.InputOrder {
				if in == "input_ids" || in == "attention_mask" {
					continue
				}
				if _, ok := cfg.ExtraInputs[in]; !ok {
					t.Errorf("InputOrder mentions %q but ExtraInputs has no entry", in)
				}
			}
		})
	}
}

func TestLookupModel_UnknownReturnsError(t *testing.T) {
	_, err := registry.Lookup("definitely-not-a-real-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestZeroExtraInput_ShapeAndValues(t *testing.T) {
	out := registry.ZeroExtraInput(2, 3)
	if len(out) != 6 {
		t.Fatalf("expected 6 elements (2×3), got %d", len(out))
	}
	for i, v := range out {
		if v != 0 {
			t.Errorf("element %d: got %d, want 0", i, v)
		}
	}
}

func TestPositionIDsExtraInput_BroadcastsPosOverBatch(t *testing.T) {
	out := registry.PositionIDsExtraInput(2, 3)
	if len(out) != 6 {
		t.Fatalf("expected 6 elements, got %d", len(out))
	}
	want := []int64{0, 1, 2, 0, 1, 2}
	for i := range out {
		if out[i] != want[i] {
			t.Errorf("element %d: got %d, want %d", i, out[i], want[i])
		}
	}
}

func TestPoolingMode_StringSurvivesUnknown(t *testing.T) {
	got := registry.PoolingMode(99).String()
	if got == "" {
		t.Error("String() returned empty for unknown mode")
	}
}

func TestPoolByMode_DispatchesToRightPool(t *testing.T) {
	raw := []float32{3, 4}
	mask := [][]int64{{1}}

	clsOut, err := poolByMode(registry.PoolingCLS, raw, mask, 1, 1, 2)
	if err != nil {
		t.Fatalf("CLS pool: %v", err)
	}
	if diff := abs32(clsOut[0][0] - 0.6); diff > 1e-5 {
		t.Errorf("CLS pool dim 0: got %f, want 0.6", clsOut[0][0])
	}

	meanOut, err := poolByMode(registry.PoolingMean, raw, mask, 1, 1, 2)
	if err != nil {
		t.Fatalf("Mean pool: %v", err)
	}
	if diff := abs32(meanOut[0][0] - 0.6); diff > 1e-5 {
		t.Errorf("Mean pool dim 0: got %f, want 0.6", meanOut[0][0])
	}

	_, err = poolByMode(registry.PoolingLastToken, raw, mask, 1, 1, 2)
	if err == nil {
		t.Error("LastToken pool should error until implemented")
	}

	_, err = poolByMode(registry.PoolingMode(99), raw, mask, 1, 1, 2)
	if err == nil {
		t.Error("unknown pool mode must error")
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
