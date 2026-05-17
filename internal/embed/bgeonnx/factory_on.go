//go:build bgeonnx

// Default factories for the `bgeonnx` build. Tokenizer swaps to the
// real HF binding; Session stays on stubSession until D1-FU-1 lands.

package bgeonnx

func defaultTokenizer(modelDir string) (Tokenizer, error) {
	return newHFTokenizer(modelDir)
}

func defaultSession(modelDir string) (Session, error) {
	return newONNXSession(modelDir)
}
