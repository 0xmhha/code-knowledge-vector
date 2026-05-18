//go:build !bgeonnx

// Default factories for the no-tag build. Tokenizer + Session both
// return their stub variants so callers see ErrNotImplemented at
// Embed() time — clearer signal than mysterious zero vectors.

package bgeonnx

func defaultTokenizer(_ string, _ ModelConfig) (Tokenizer, error) {
	return stubTokenizer{}, nil
}

func defaultSession(_ string, _ ModelConfig) (Session, error) {
	return stubSession{}, nil
}
