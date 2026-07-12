// Pure-Go pooling primitives + dispatch. No build tag — these are
// exercised by unit tests even without libonnxruntime installed, so
// the numeric correctness of every pooling mode is regression-tested
// in every CI run, not just the smoke build.

package bgeonnx

import (
	"fmt"
	"math"
)

// poolByMode dispatches to the right pooling implementation per
// ModelConfig.Pooling. Session calls this — neither bgeonnx.go nor
// session_impl.go knows which strategy to use; the model registry
// decides.
func poolByMode(mode PoolingMode, raw []float32, mask [][]int64, batch, seqLen, hidden int) ([][]float32, error) {
	switch mode {
	case PoolingCLS:
		return clsPoolNormalize(raw, batch, seqLen, hidden)
	case PoolingMean:
		return meanPoolNormalize(raw, mask, batch, seqLen, hidden)
	case PoolingLastToken:
		return lastTokenPoolNormalize(raw, mask, batch, seqLen, hidden)
	default:
		return nil, fmt.Errorf("bgeonnx: unknown pooling mode %s", mode)
	}
}

// l2Normalize scales vec in place to unit L2 length (no-op for a zero vector).
func l2Normalize(vec []float32) {
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for h := range vec {
			vec[h] /= norm
		}
	}
}

// lastTokenPoolNormalize takes the hidden state of the last attended token per
// row and L2-normalizes it. This is the standard sentence representation for
// decoder-only / causal models (Qwen2-based bge-code-v1, e5-mistral): the final
// non-padded token has attended to the whole sequence under the causal mask, so
// its hidden state summarizes the input. The last attended position is the last
// t with mask[b][t] != 0, which handles both left- and right-padding. raw
// layout: [batch * seqLen * hidden] row-major.
func lastTokenPoolNormalize(raw []float32, mask [][]int64, batch, seqLen, hidden int) ([][]float32, error) {
	if got, want := len(raw), batch*seqLen*hidden; got != want {
		return nil, fmt.Errorf("bgeonnx: raw size %d, want %d (batch=%d seqLen=%d hidden=%d)", got, want, batch, seqLen, hidden)
	}
	if len(mask) != batch {
		return nil, fmt.Errorf("bgeonnx: mask rows %d, want %d", len(mask), batch)
	}
	vecs := make([][]float32, batch)
	for b := 0; b < batch; b++ {
		if len(mask[b]) != seqLen {
			return nil, fmt.Errorf("bgeonnx: mask[%d] len %d, want %d", b, len(mask[b]), seqLen)
		}
		last := -1
		for t := 0; t < seqLen; t++ {
			if mask[b][t] != 0 {
				last = t
			}
		}
		if last < 0 {
			return nil, fmt.Errorf("bgeonnx: row %d has all-zero attention — empty input?", b)
		}
		base := (b*seqLen + last) * hidden
		vec := make([]float32, hidden)
		copy(vec, raw[base:base+hidden])
		l2Normalize(vec)
		vecs[b] = vec
	}
	return vecs, nil
}

// meanPoolNormalize reduces ONNX `last_hidden_state` output of shape
// [batch, seqLen, hidden] to [batch, hidden] by attention-masked mean
// pooling, then L2-normalizes each row. raw is row-major
// (batch * seqLen * hidden float32s).
//
// Note: bge-large-en-v1.5 uses CLS pooling, not mean — see
// clsPoolNormalize below. Keep mean pool around for future models
// like bge-m3 that do use it (1_Pooling/config.json:
// pooling_mode_mean_tokens=true).
func meanPoolNormalize(raw []float32, mask [][]int64, batch, seqLen, hidden int) ([][]float32, error) {
	if got, want := len(raw), batch*seqLen*hidden; got != want {
		return nil, fmt.Errorf("bgeonnx: raw size %d, want %d (batch=%d seqLen=%d hidden=%d)", got, want, batch, seqLen, hidden)
	}
	if len(mask) != batch {
		return nil, fmt.Errorf("bgeonnx: mask rows %d, want %d", len(mask), batch)
	}

	vecs := make([][]float32, batch)
	for b := 0; b < batch; b++ {
		if len(mask[b]) != seqLen {
			return nil, fmt.Errorf("bgeonnx: mask[%d] len %d, want %d", b, len(mask[b]), seqLen)
		}
		vec := make([]float32, hidden)
		var maskSum float32
		for t := 0; t < seqLen; t++ {
			m := float32(mask[b][t])
			if m == 0 {
				continue
			}
			maskSum += m
			base := (b*seqLen + t) * hidden
			for h := 0; h < hidden; h++ {
				vec[h] += raw[base+h] * m
			}
		}
		if maskSum == 0 {
			return nil, fmt.Errorf("bgeonnx: row %d has all-zero attention — empty input?", b)
		}
		for h := range vec {
			vec[h] /= maskSum
		}
		l2Normalize(vec)
		vecs[b] = vec
	}
	return vecs, nil
}

// clsPoolNormalize takes only the [CLS] (position 0) hidden state per
// row and L2-normalizes it. This matches bge-large-en-v1.5's training
// objective (1_Pooling/config.json: pooling_mode_cls_token=true), so
// the resulting embeddings line up bit-exact with what sentence-
// transformers would produce in Python.
//
// raw layout: [batch * seqLen * hidden] row-major. mask is ignored —
// the [CLS] position is always attended in BERT-family encoders.
func clsPoolNormalize(raw []float32, batch, seqLen, hidden int) ([][]float32, error) {
	if got, want := len(raw), batch*seqLen*hidden; got != want {
		return nil, fmt.Errorf("bgeonnx: raw size %d, want %d (batch=%d seqLen=%d hidden=%d)", got, want, batch, seqLen, hidden)
	}
	vecs := make([][]float32, batch)
	for b := 0; b < batch; b++ {
		base := b * seqLen * hidden // start of row b, position 0 = [CLS]
		vec := make([]float32, hidden)
		copy(vec, raw[base:base+hidden])
		l2Normalize(vec)
		vecs[b] = vec
	}
	return vecs, nil
}
