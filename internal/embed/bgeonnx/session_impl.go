//go:build bgeonnx

// onnxSession wraps yalue/onnxruntime_go (CGO around Microsoft's
// libonnxruntime). Holds one ONNX inference session per process —
// session construction is expensive (~1.5s cold start), so Adapter
// is intended to be long-lived and shared.
//
// This file builds only with `-tags bgeonnx` so the default build
// avoids the libonnxruntime system dependency. See docs/d1-installation-guide.md.

package bgeonnx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ortInitOnce gates ort.InitializeEnvironment() — it's process-global
// and must not be called twice. We never call DestroyEnvironment in
// CKV's CLI lifecycle: the process exits and the OS reclaims the
// resources. A long-running server would need to track an explicit
// shutdown hook.
var (
	ortInitOnce sync.Once
	ortInitErr  error
)

func initORT() error {
	ortInitOnce.Do(func() {
		// yalue/onnxruntime_go's default lookup is "onnxruntime.so" —
		// only matches Linux. macOS/Windows need an explicit dylib/dll
		// path. Allow user override via env so unusual install
		// locations work without a recompile.
		if libPath := os.Getenv("CKV_ONNXRUNTIME_LIB"); libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		} else if runtime.GOOS == "darwin" {
			// brew's standard location on Apple Silicon. Intel Macs
			// would set CKV_ONNXRUNTIME_LIB=/usr/local/lib/libonnxruntime.dylib.
			ort.SetSharedLibraryPath("/opt/homebrew/lib/libonnxruntime.dylib")
		}
		ortInitErr = ort.InitializeEnvironment()
	})
	return ortInitErr
}

// Input/output names emitted by HuggingFace's BERT ONNX export.
// bge-large-en-v1.5 requires token_type_ids (BERT inherits the
// next-sentence-prediction architecture even though we only embed
// single sequences — all zeros is the right value). Future non-BERT
// models will need a different signature; gate on ModelName then.
var (
	onnxInputNames  = []string{"input_ids", "attention_mask", "token_type_ids"}
	onnxOutputNames = []string{"last_hidden_state"}
)

type onnxSession struct {
	sess *ort.DynamicAdvancedSession
}

func newONNXSession(modelDir string) (*onnxSession, error) {
	if err := initORT(); err != nil {
		return nil, fmt.Errorf("init ONNX environment: %w", err)
	}
	modelPath := filepath.Join(modelDir, fileModel)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model.onnx missing at %s: %w", modelPath, err)
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("session options: %w", err)
	}
	defer opts.Destroy()
	// Default optimization level + intra-op threading. Tuning is FU-3
	// once we have actual latency numbers from the runbook.

	sess, err := ort.NewDynamicAdvancedSession(modelPath, onnxInputNames, onnxOutputNames, opts)
	if err != nil {
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}
	return &onnxSession{sess: sess}, nil
}

// Run executes one batch and applies mean pooling (attention-masked)
// + L2 normalization. The bge-* family was trained against pooled+
// normalized vectors so cosine similarity in downstream search lines
// up with the training objective — skip either step and the recall
// numbers drop measurably.
func (s *onnxSession) Run(ctx context.Context, tokens TokenizedBatch) ([][]float32, error) {
	if s == nil || s.sess == nil {
		return nil, fmt.Errorf("bgeonnx: session closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	batch := len(tokens.InputIDs)
	if batch == 0 {
		return nil, nil
	}
	seqLen := len(tokens.InputIDs[0])
	if seqLen == 0 {
		return nil, fmt.Errorf("bgeonnx: zero-length sequence in batch")
	}

	// Flatten 2D rows → 1D row-major for ORT tensor backing.
	idsFlat := make([]int64, batch*seqLen)
	maskFlat := make([]int64, batch*seqLen)
	for i := 0; i < batch; i++ {
		if len(tokens.InputIDs[i]) != seqLen || len(tokens.AttentionMask[i]) != seqLen {
			return nil, fmt.Errorf("bgeonnx: ragged tensor at row %d (expected seqLen=%d)", i, seqLen)
		}
		copy(idsFlat[i*seqLen:(i+1)*seqLen], tokens.InputIDs[i])
		copy(maskFlat[i*seqLen:(i+1)*seqLen], tokens.AttentionMask[i])
	}

	shape := ort.NewShape(int64(batch), int64(seqLen))
	idsTensor, err := ort.NewTensor[int64](shape, idsFlat)
	if err != nil {
		return nil, fmt.Errorf("input_ids tensor: %w", err)
	}
	defer idsTensor.Destroy()
	maskTensor, err := ort.NewTensor[int64](shape, maskFlat)
	if err != nil {
		return nil, fmt.Errorf("attention_mask tensor: %w", err)
	}
	defer maskTensor.Destroy()
	// bge-large-en-v1.5 is a single-sequence embedder so every token
	// belongs to segment 0. The BERT ONNX graph still requires the
	// input, so allocate a zero tensor of the matching shape.
	typeIDsTensor, err := ort.NewTensor[int64](shape, make([]int64, batch*seqLen))
	if err != nil {
		return nil, fmt.Errorf("token_type_ids tensor: %w", err)
	}
	defer typeIDsTensor.Destroy()

	outputs := []ort.Value{nil} // nil → ORT auto-allocates; we free below.
	if err := s.sess.Run([]ort.Value{idsTensor, maskTensor, typeIDsTensor}, outputs); err != nil {
		return nil, fmt.Errorf("ONNX run: %w", err)
	}
	defer func() {
		if outputs[0] != nil {
			_ = outputs[0].Destroy()
		}
	}()

	hidden, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("bgeonnx: output is %T, want *Tensor[float32] — check ONNX export FP32 vs FP16", outputs[0])
	}
	outShape := hidden.GetShape()
	if len(outShape) != 3 || outShape[0] != int64(batch) || outShape[1] != int64(seqLen) || outShape[2] != int64(ModelDim) {
		return nil, fmt.Errorf("bgeonnx: output shape %v, want [%d,%d,%d]", outShape, batch, seqLen, ModelDim)
	}

	// bge-large-en-v1.5 → CLS pooling (mask is unused here; [CLS] is
	// always at position 0 and always attended in BERT encoders).
	return clsPoolNormalize(hidden.GetData(), batch, seqLen, ModelDim)
}

func (s *onnxSession) Close() error {
	if s == nil || s.sess == nil {
		return nil
	}
	err := s.sess.Destroy()
	s.sess = nil
	return err
}
