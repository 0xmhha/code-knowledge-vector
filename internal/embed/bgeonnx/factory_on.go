//go:build bgeonnx

// Default factories for the `bgeonnx` build. Tokenizer swaps to the
// real HF binding; Session stays on stubSession until D1-FU-1 lands.

package bgeonnx

func defaultTokenizer(modelDir string) (Tokenizer, error) {
	return newHFTokenizer(modelDir)
}

func defaultSession(_ string) (Session, error) {
	// TODO(D1-FU-1): replace with newONNXSession(modelDir) once
	// session_impl.go is implemented. Until then Embed() will still
	// fail with ErrNotImplemented at the session step, but the
	// tokenizer half is exercisable in isolation.
	return stubSession{}, nil
}
