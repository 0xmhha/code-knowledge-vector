package types

import "testing"

func TestEmbeddingIdentityChecksum(t *testing.T) {
	base := EmbeddingIdentity{Provider: "ollama", Model: "bge-m3", Dim: 1024}

	// Stable and equal for identical identities.
	if base.Checksum() != base.Checksum() {
		t.Fatal("checksum must be stable for the same identity")
	}
	same := EmbeddingIdentity{Provider: "ollama", Model: "bge-m3", Dim: 1024}
	if base.Checksum() != same.Checksum() {
		t.Errorf("identical identities must share a checksum:\n base=%q\n same=%q",
			base.Checksum(), same.Checksum())
	}

	// Any single-field difference must change the checksum. Each case is a
	// real swap we need Open to reject.
	cases := map[string]EmbeddingIdentity{
		"provider (Ollama vs ONNX, same model+dim)": {Provider: "bgeonnx", Model: "bge-m3", Dim: 1024},
		"model swap":                                {Provider: "ollama", Model: "qwen3-embedding", Dim: 1024},
		"dimension":                                 {Provider: "ollama", Model: "bge-m3", Dim: 768},
		"pooling":                                   {Provider: "ollama", Model: "bge-m3", Dim: 1024, Pooling: "cls"},
		"normalize":                                 {Provider: "ollama", Model: "bge-m3", Dim: 1024, Normalize: "l2"},
	}
	for name, id := range cases {
		if id.Checksum() == base.Checksum() {
			t.Errorf("%s: expected a different checksum, both were %q", name, id.Checksum())
		}
	}
}
