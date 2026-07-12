//go:build darwin && tokenizers

package coreml

import (
	"fmt"
	"os"

	"github.com/daulet/tokenizers"
)

// tokenizer wraps daulet/tokenizers (CGO around HuggingFace Rust
// tokenizers crate). Reads tokenizer.json directly so the
// CoreML adapter stays bit-exact with the upstream model.
type tokenizer struct {
	tk *tokenizers.Tokenizer
}

// newTokenizer loads tokenizer.json from the given path.
func newTokenizer(path string) (*tokenizer, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("coreml: tokenizer.json missing at %s: %w", path, err)
	}
	tk, err := tokenizers.FromFile(path)
	if err != nil {
		return nil, fmt.Errorf("coreml: load tokenizer: %w", err)
	}
	return &tokenizer{tk: tk}, nil
}

// Encode tokenizes one text and returns padded input_ids + attention_mask
// of length maxLen.
func (t *tokenizer) Encode(text string, maxLen int) (ids, mask []int64, err error) {
	if t == nil || t.tk == nil {
		return nil, nil, fmt.Errorf("coreml: tokenizer closed")
	}
	enc, err := t.tk.EncodeWithOptionsErr(text, true, tokenizers.WithReturnAttentionMask())
	if err != nil {
		return nil, nil, fmt.Errorf("coreml: encode: %w", err)
	}
	// Truncate to maxLen
	if len(enc.IDs) > maxLen {
		enc.IDs = enc.IDs[:maxLen]
		enc.AttentionMask = enc.AttentionMask[:maxLen]
	}
	ids = make([]int64, maxLen)
	mask = make([]int64, maxLen)
	for i, id := range enc.IDs {
		ids[i] = int64(id)
	}
	for i, m := range enc.AttentionMask {
		mask[i] = int64(m)
	}
	return ids, mask, nil
}

// Close releases the underlying tokenizer.
func (t *tokenizer) Close() error {
	if t == nil || t.tk == nil {
		return nil
	}
	err := t.tk.Close()
	t.tk = nil
	return err
}
