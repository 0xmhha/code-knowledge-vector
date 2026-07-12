package registry

import "testing"

func TestLookup_DefaultExists(t *testing.T) {
	cfg, err := Lookup(DefaultModelName)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", DefaultModelName, err)
	}
	if cfg.Name != DefaultModelName {
		t.Errorf("Name = %q, want %q", cfg.Name, DefaultModelName)
	}
	if cfg.Dim <= 0 {
		t.Errorf("Dim = %d, want > 0", cfg.Dim)
	}
}

func TestLookup_UnknownReturnsError(t *testing.T) {
	_, err := Lookup("nonexistent-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestList_ReturnsAllModels(t *testing.T) {
	all := List()
	if len(all) < 2 {
		t.Fatalf("expected at least 2 models, got %d", len(all))
	}
	// Verify sorted order
	for i := 1; i < len(all); i++ {
		if all[i].Name <= all[i-1].Name {
			t.Errorf("not sorted: %q after %q", all[i].Name, all[i-1].Name)
		}
	}
}

func TestNames_MatchesList(t *testing.T) {
	names := Names()
	all := List()
	if len(names) != len(all) {
		t.Fatalf("Names() len=%d, List() len=%d", len(names), len(all))
	}
	for i, n := range names {
		if n != all[i].Name {
			t.Errorf("Names()[%d]=%q, List()[%d].Name=%q", i, n, i, all[i].Name)
		}
	}
}

func TestModelConfig_FetchFiles(t *testing.T) {
	cfg, _ := Lookup(DefaultModelName)
	files := cfg.FetchFiles()
	if len(files) < 2 {
		t.Errorf("expected at least 2 fetch files (onnx + tokenizer), got %d", len(files))
	}
}

func TestModelConfig_DefaultModelDir(t *testing.T) {
	cfg, _ := Lookup(DefaultModelName)
	dir, err := cfg.DefaultModelDir()
	if err != nil {
		t.Fatalf("DefaultModelDir: %v", err)
	}
	if dir == "" {
		t.Error("DefaultModelDir returned empty string")
	}
}

func TestModelConfig_EntryShape(t *testing.T) {
	for _, cfg := range List() {
		t.Run(cfg.Name, func(t *testing.T) {
			if cfg.Dim <= 0 {
				t.Errorf("Dim must be > 0")
			}
			if cfg.MaxInput <= 0 {
				t.Errorf("MaxInput must be > 0")
			}
			if cfg.OnnxFile == "" {
				// Ollama-only entry: identity metadata only. It must not
				// half-configure the ONNX/fetch path.
				if cfg.TokenizerFile != "" {
					t.Error("ollama-only entry must not set TokenizerFile")
				}
				if cfg.HFRepo != "" {
					t.Error("ollama-only entry must not set HFRepo")
				}
				if len(cfg.InputOrder) != 0 {
					t.Error("ollama-only entry must not set InputOrder")
				}
				return
			}
			if cfg.TokenizerFile == "" {
				t.Error("TokenizerFile must not be empty")
			}
			if cfg.HFRepo == "" {
				t.Error("HFRepo must not be empty")
			}
			if len(cfg.InputOrder) == 0 {
				t.Error("InputOrder must not be empty")
			}
			for _, in := range cfg.InputOrder {
				if in == "input_ids" || in == "attention_mask" {
					continue
				}
				if _, ok := cfg.ExtraInputs[in]; !ok {
					t.Errorf("InputOrder has %q but ExtraInputs missing entry", in)
				}
			}
		})
	}
}

func TestPoolingMode_String(t *testing.T) {
	tests := []struct {
		mode PoolingMode
		want string
	}{
		{PoolingCLS, "cls"},
		{PoolingMean, "mean"},
		{PoolingLastToken, "last_token"},
		{PoolingMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("PoolingMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestZeroExtraInput(t *testing.T) {
	out := ZeroExtraInput(2, 3)
	if len(out) != 6 {
		t.Fatalf("len = %d, want 6", len(out))
	}
	for i, v := range out {
		if v != 0 {
			t.Errorf("[%d] = %d, want 0", i, v)
		}
	}
}

func TestPositionIDsExtraInput(t *testing.T) {
	out := PositionIDsExtraInput(2, 3)
	want := []int64{0, 1, 2, 0, 1, 2}
	if len(out) != len(want) {
		t.Fatalf("len = %d, want %d", len(out), len(want))
	}
	for i := range out {
		if out[i] != want[i] {
			t.Errorf("[%d] = %d, want %d", i, out[i], want[i])
		}
	}
}

func TestQueryInstruct(t *testing.T) {
	if QueryInstruct("qwen3-embedding:4b") == "" {
		t.Errorf("qwen3-embedding:4b should carry a query instruct")
	}
	if QueryInstruct("qwen3-embedding:0.6b") == "" {
		t.Errorf("qwen3-embedding:0.6b should carry a query instruct")
	}
	if got := QueryInstruct("bge-large-en-v1.5"); got != "" {
		t.Errorf("symmetric bge model should have no query instruct, got %q", got)
	}
	if got := QueryInstruct("nonexistent-model"); got != "" {
		t.Errorf("unknown model should return empty, got %q", got)
	}
}

func TestKnownDims(t *testing.T) {
	d4 := KnownDims("qwen3-embedding:4b")
	if len(d4) == 0 || d4[len(d4)-1] != 2560 {
		t.Errorf("4b KnownDims should end at native 2560: %v", d4)
	}
	found1024 := false
	for _, d := range d4 {
		if d == 1024 {
			found1024 = true
		}
	}
	if !found1024 {
		t.Errorf("4b should support 1024: %v", d4)
	}
	d06 := KnownDims("qwen3-embedding:0.6b")
	if len(d06) == 0 || d06[len(d06)-1] != 1024 {
		t.Errorf("0.6b KnownDims should end at native 1024: %v", d06)
	}
	if KnownDims("bge-large-en-v1.5") != nil {
		t.Errorf("non-MRL model should have nil KnownDims")
	}
	if KnownDims("nonexistent-model") != nil {
		t.Errorf("unknown model should return nil")
	}
}

func TestBGECodeV1Entry(t *testing.T) {
	cfg, err := Lookup("bge-code-v1")
	if err != nil {
		t.Fatalf("bge-code-v1 should be registered: %v", err)
	}
	if cfg.Dim != 1536 {
		t.Errorf("Dim = %d, want 1536", cfg.Dim)
	}
	if cfg.Pooling != PoolingLastToken {
		t.Errorf("Pooling = %v, want last_token (decoder-only)", cfg.Pooling)
	}
	if _, ok := cfg.ExtraInputs["position_ids"]; !ok {
		t.Errorf("bge-code-v1 (Qwen2) must synthesize position_ids, ExtraInputs=%v", cfg.ExtraInputs)
	}
	if _, ok := cfg.ExtraInputs["token_type_ids"]; ok {
		t.Errorf("decoder-only model must not use token_type_ids")
	}
}
