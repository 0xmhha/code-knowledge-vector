//go:build bgeonnx

// onnxSession wraps yalue/onnxruntime_go (CGO around Microsoft's
// libonnxruntime). Holds one ONNX inference session per process —
// session construction is expensive (~1.5s cold start), so Adapter
// is intended to be long-lived and shared.
//
// This file builds only with `-tags bgeonnx` so the default build
// avoids the libonnxruntime system dependency. See docs/d1-installation-guide.md.
//
// Model-specific behavior (input names, extra inputs, pooling) is
// driven entirely by ModelConfig — see model_config.go. Adding a new
// model never requires editing this file.

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

type onnxSession struct {
	sess *ort.DynamicAdvancedSession
	cfg  ModelConfig
}

func newONNXSession(modelDir string, cfg ModelConfig) (*onnxSession, error) {
	if err := initORT(); err != nil {
		return nil, fmt.Errorf("init ONNX environment: %w", err)
	}
	modelPath := filepath.Join(modelDir, cfg.OnnxFile)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("%s missing at %s: %w", cfg.OnnxFile, modelPath, err)
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("session options: %w", err)
	}
	defer opts.Destroy()

	sess, err := ort.NewDynamicAdvancedSession(modelPath, cfg.InputOrder, cfg.Outputs, opts)
	if err != nil {
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}
	return &onnxSession{sess: sess, cfg: cfg}, nil
}

// Run executes one batch and applies pooling per ModelConfig.Pooling
// + L2 normalization. Inputs are assembled in ModelConfig.InputOrder:
// input_ids / attention_mask come from the tokenizer; any extra
// inputs (token_type_ids for BERT, position_ids for Qwen2, etc.) come
// from ModelConfig.ExtraInputs.
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

	// Assemble inputs in the exact order the ONNX graph expects.
	inputs := make([]ort.Value, len(s.cfg.InputOrder))
	for i, name := range s.cfg.InputOrder {
		var t ort.Value
		var err error
		switch name {
		case "input_ids":
			t, err = ort.NewTensor[int64](shape, idsFlat)
		case "attention_mask":
			t, err = ort.NewTensor[int64](shape, maskFlat)
		default:
			fn, ok := s.cfg.ExtraInputs[name]
			if !ok {
				return nil, fmt.Errorf("bgeonnx: input %q has no source — register an ExtraInputFn in model_config.go", name)
			}
			t, err = ort.NewTensor[int64](shape, fn(batch, seqLen))
		}
		if err != nil {
			return nil, fmt.Errorf("%s tensor: %w", name, err)
		}
		inputs[i] = t
	}
	defer func() {
		for _, in := range inputs {
			if in != nil {
				_ = in.Destroy()
			}
		}
	}()

	outputs := []ort.Value{nil} // nil → ORT auto-allocates; we free below.
	if err := s.sess.Run(inputs, outputs); err != nil {
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
	if len(outShape) != 3 || outShape[0] != int64(batch) || outShape[1] != int64(seqLen) || outShape[2] != int64(s.cfg.Dim) {
		return nil, fmt.Errorf("bgeonnx: output shape %v, want [%d,%d,%d]", outShape, batch, seqLen, s.cfg.Dim)
	}

	return poolByMode(s.cfg.Pooling, hidden.GetData(), tokens.AttentionMask, batch, seqLen, s.cfg.Dim)
}

func (s *onnxSession) Close() error {
	if s == nil || s.sess == nil {
		return nil
	}
	err := s.sess.Destroy()
	s.sess = nil
	return err
}
