package ollama

import "testing"

func TestValidateTargetDim(t *testing.T) {
	const nativeDim = 2560 // qwen3-embedding:4b

	// 0 (no truncation / native) is always valid.
	if err := validateTargetDim("qwen3-embedding:4b", 0, nativeDim); err != nil {
		t.Fatalf("target 0 should be valid: %v", err)
	}
	// A standard MRL dim is accepted.
	if err := validateTargetDim("qwen3-embedding:4b", 1024, nativeDim); err != nil {
		t.Fatalf("1024 is a known dim for 4b: %v", err)
	}
	// A non-standard dim is rejected.
	if err := validateTargetDim("qwen3-embedding:4b", 777, nativeDim); err == nil {
		t.Fatalf("777 is not a known dim — expected error")
	}
	// Exceeding native is rejected.
	if err := validateTargetDim("qwen3-embedding:4b", 4096, nativeDim); err == nil {
		t.Fatalf("4096 > native — expected error")
	}
	// A model with no MRL ladder (or unknown) allows any dim <= native.
	if err := validateTargetDim("bge-large-en-v1.5", 512, 1024); err != nil {
		t.Fatalf("non-MRL model should not restrict dims: %v", err)
	}
}
