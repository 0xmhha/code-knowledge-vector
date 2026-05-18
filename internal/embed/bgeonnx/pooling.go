// Pure-Go pooling primitives. No build tag — these are exercised by
// unit tests even without libonnxruntime installed, so the numeric
// correctness of mean pooling + L2 normalize is regression-tested in
// every CI run, not just the smoke build.

package bgeonnx

import (
	"fmt"
	"math"
)

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
		vecs[b] = vec
	}
	return vecs, nil
}
