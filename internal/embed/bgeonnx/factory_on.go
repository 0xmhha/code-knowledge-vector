//go:build bgeonnx

// Default factories for the `bgeonnx` build. Wires the real CGO
// implementations (HF Rust tokenizers + ONNX Runtime).

package bgeonnx

func defaultTokenizer(modelDir string, cfg ModelConfig) (Tokenizer, error) {
	return newHFTokenizer(modelDir, cfg)
}

func defaultSession(modelDir string, cfg ModelConfig) (Session, error) {
	return newONNXSession(modelDir, cfg)
}
