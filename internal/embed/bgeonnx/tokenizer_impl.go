//go:build bgeonnx

// hfTokenizer wraps daulet/tokenizers (a CGO binding around the
// HuggingFace Rust `tokenizers` crate). Reading `tokenizer.json`
// directly keeps us bit-exact with the upstream HF reference, which
// matters for bge-code-v1 — drift here would silently change the
// embedding distribution.
//
// This file builds only with `-tags bgeonnx` so the default build
// avoids the libtokenizers system dependency. See docs/d1-installation-guide.md.

package bgeonnx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/daulet/tokenizers"
)

type hfTokenizer struct {
	tk *tokenizers.Tokenizer
}

func newHFTokenizer(modelDir string) (*hfTokenizer, error) {
	path := filepath.Join(modelDir, fileTokenizer)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("tokenizer.json missing at %s: %w", path, err)
	}
	tk, err := tokenizers.FromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer.json: %w", err)
	}
	return &hfTokenizer{tk: tk}, nil
}

// Tokenize encodes batch into uniform-length int64 tensors padded to
// the longest sequence in the batch (truncated to maxLen first). Two
// passes is intentional: pass 1 finds the actual max so we don't pad
// to 8192 when the batch is mostly short snippets — fixed-max padding
// would 10x the inference cost on small batches.
//
// bge-code-v1's ONNX export does not consume token_type_ids, so we
// leave TokenTypeIDs nil — Session.Run treats nil as "zeros" if the
// graph requests it.
func (t *hfTokenizer) Tokenize(ctx context.Context, batch []string, maxLen int) (TokenizedBatch, error) {
	if t == nil || t.tk == nil {
		return TokenizedBatch{}, fmt.Errorf("bgeonnx: tokenizer closed")
	}
	if err := ctx.Err(); err != nil {
		return TokenizedBatch{}, err
	}
	if len(batch) == 0 {
		return TokenizedBatch{}, nil
	}

	encs := make([]tokenizers.Encoding, len(batch))
	maxObserved := 0
	for i, s := range batch {
		enc, err := t.tk.EncodeWithOptionsErr(s, true, tokenizers.WithReturnAttentionMask())
		if err != nil {
			return TokenizedBatch{}, fmt.Errorf("encode[%d]: %w", i, err)
		}
		// Tail truncation: bge models' learned attention treats the
		// [CLS] prefix as a positional anchor, so keeping the head and
		// dropping the tail is the convention. Leading-token retention
		// also matches HF's default truncation_side="right".
		if len(enc.IDs) > maxLen {
			enc.IDs = enc.IDs[:maxLen]
			enc.AttentionMask = enc.AttentionMask[:maxLen]
		}
		encs[i] = enc
		if len(enc.IDs) > maxObserved {
			maxObserved = len(enc.IDs)
		}
	}
	if maxObserved == 0 {
		return TokenizedBatch{}, fmt.Errorf("bgeonnx: every input produced zero tokens — tokenizer.json likely invalid")
	}

	out := TokenizedBatch{
		InputIDs:      make([][]int64, len(batch)),
		AttentionMask: make([][]int64, len(batch)),
	}
	for i, enc := range encs {
		ids := make([]int64, maxObserved)
		mask := make([]int64, maxObserved)
		for j, id := range enc.IDs {
			ids[j] = int64(id)
		}
		for j, m := range enc.AttentionMask {
			mask[j] = int64(m)
		}
		out.InputIDs[i] = ids
		out.AttentionMask[i] = mask
	}
	return out, nil
}

// Close releases the underlying Rust tokenizer. Adapter.Close() invokes
// this via the io.Closer type assertion so the stub variant (which has
// no Close) doesn't need to implement it.
func (t *hfTokenizer) Close() error {
	if t == nil || t.tk == nil {
		return nil
	}
	err := t.tk.Close()
	t.tk = nil
	return err
}
