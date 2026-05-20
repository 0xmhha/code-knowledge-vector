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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		var envOpts []ort.EnvironmentOption
		if ortVerbose() {
			envOpts = append(envOpts, ort.WithLogLevelVerbose())
		}
		ortInitErr = ort.InitializeEnvironment(envOpts...)
	})
	return ortInitErr
}

// ortVerbose returns true when CKV_ORT_VERBOSE is set to a truthy value.
// Enables ORT env- and session-level verbose logging — useful when
// diagnosing CoreML compile / EP attach failures. See
// docs/issue-coreml-compile-io-error.md.
func ortVerbose() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CKV_ORT_VERBOSE"))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

type onnxSession struct {
	sess     *ort.DynamicAdvancedSession
	cfg      ModelConfig
	provider string
}

// coreMLDisabled returns true when the user wants ORT to skip CoreML
// attach (e.g. when debugging an operator mismatch). Recognized truthy
// tokens match Go's stdlib (strconv.ParseBool) so "1", "true", "TRUE"
// all work; everything else (including empty) is false.
func coreMLDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CKV_DISABLE_COREML")))
	switch v {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

// attachCoreML tries to bind CoreML EP V2 onto opts. Returns the
// provider tag to record:
//
//   - "coreml"                 — attach succeeded.
//   - "coreml-fallback-to-cpu" — attach errored; ORT will use CPU. We
//                                deliberately do NOT propagate the
//                                error: a missing/broken CoreML stack
//                                must not block index builds, only
//                                slow them.
//
// MLComputeUnits selects which compute units ORT may dispatch to:
//
//   - "ALL"        (default) — CPU + GPU + ANE
//   - "CPUAndGPU"            — CPU + GPU, no ANE (workaround for ANE
//                              compile I/O errors — see
//                              docs/issue-coreml-compile-io-error.md)
//   - "CPUOnly"             — CoreML attached but runs CPU only
//
// Override via env CKV_COREML_UNITS. Note: this only affects the V2
// CoreML EP attach options — ORT may still fall back to CPU at
// per-op granularity for unsupported ops regardless of this setting.
//
// CKV_COREML_CACHE_DIR enables ModelCacheDirectory — first run pays
// the GPU/ANE compile cost, subsequent runs read the cached compiled
// model from disk. Without this, the compile artifact lives in a temp
// dir that ORT cleans up on session destroy, so every build re-pays
// the multi-minute compile cost. yalue/onnxruntime_go v1.30.1 passes
// this key straight to ORT's C API (verified against the binding's
// own test in onnxruntime_test.go::getCoreMLV2SessionOptions).
//
// CKV_COREML_MODEL_FORMAT picks between "MLProgram" (newer, Core ML 5+,
// macOS 12+) and "NeuralNetwork" (legacy default). Unset leaves the
// ORT default — currently NeuralNetwork, which silently casts FP32 →
// FP16 on the MPS / ANE path. MLProgram keeps the model at its
// declared precision, which is a candidate root-cause fix for the
// ANE compile I/O error.
//
// CKV_COREML_GPU_FP16 maps to AllowLowPrecisionAccumulationOnGPU.
// Enables FP16 accumulation for GPU ops only; ANE / CPU paths are
// unaffected. Trades a small (<1 ulp) numerical error for measurable
// GPU throughput on Apple Silicon.
//
// CKV_STATIC_SHAPES asks ORT to compile for fixed input shapes
// (RequireStaticInputShapes=1). Pair with tokenizer-side max-length
// padding (the tokenizer reads the same env). Useful on ANE, which
// is designed for fixed-shape ops — a single compile + reuse beats
// per-batch dynamic recompiles. Bumps padding overhead but flattens
// the cold-compile tail.
func attachCoreML(opts *ort.SessionOptions, w io.Writer) string {
	units := strings.TrimSpace(os.Getenv("CKV_COREML_UNITS"))
	if units == "" {
		units = "ALL"
	}
	coreMLOpts := map[string]string{"MLComputeUnits": units}
	if format := strings.TrimSpace(os.Getenv("CKV_COREML_MODEL_FORMAT")); format != "" {
		coreMLOpts["ModelFormat"] = format
		if w != nil {
			fmt.Fprintf(w, "bgeonnx: CoreML ModelFormat=%s\n", format)
		}
	}
	if envBool("CKV_COREML_GPU_FP16") {
		coreMLOpts["AllowLowPrecisionAccumulationOnGPU"] = "1"
		if w != nil {
			fmt.Fprintf(w, "bgeonnx: CoreML AllowLowPrecisionAccumulationOnGPU=1\n")
		}
	}
	if envBool("CKV_STATIC_SHAPES") {
		coreMLOpts["RequireStaticInputShapes"] = "1"
		if w != nil {
			fmt.Fprintf(w, "bgeonnx: CoreML RequireStaticInputShapes=1\n")
		}
	}
	if cacheDir := strings.TrimSpace(os.Getenv("CKV_COREML_CACHE_DIR")); cacheDir != "" {
		coreMLOpts["ModelCacheDirectory"] = cacheDir
		if w != nil {
			fmt.Fprintf(w, "bgeonnx: CoreML ModelCacheDirectory=%s\n", cacheDir)
		}
	}
	if err := opts.AppendExecutionProviderCoreMLV2(coreMLOpts); err != nil {
		if w != nil {
			fmt.Fprintf(w, "bgeonnx: CoreML attach failed (%v), falling back to CPU\n", err)
		}
		return "coreml-fallback-to-cpu"
	}
	return "coreml"
}

// envBool parses a truthy env var: "1", "t", "true", "y", "yes", "on"
// (case-insensitive) return true; everything else, including unset,
// returns false. Matches ortVerbose() / coreMLDisabled() — kept inline
// instead of refactoring those callers to avoid mixing one feature
// addition with a sweep.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

// chooseProvider decides which EP to attach based on platform + env.
// Pulled out so it's unit-testable without touching ORT. The returned
// closure is what session creation calls; it returns the resolved
// provider tag.
func chooseProvider(goos string, disabled bool) func(*ort.SessionOptions, io.Writer) string {
	if goos != "darwin" || disabled {
		return func(_ *ort.SessionOptions, _ io.Writer) string { return "cpu" }
	}
	return attachCoreML
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

	if ortVerbose() {
		if e := opts.SetLogSeverityLevel(ort.LoggingLevelVerbose); e != nil {
			fmt.Fprintf(os.Stderr, "bgeonnx: set session log level failed: %v\n", e)
		}
	}

	provider := chooseProvider(runtime.GOOS, coreMLDisabled())(opts, os.Stderr)

	sess, err := ort.NewDynamicAdvancedSession(modelPath, cfg.InputOrder, cfg.Outputs, opts)
	if err != nil {
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}
	return &onnxSession{sess: sess, cfg: cfg, provider: provider}, nil
}

// Provider reports which execution backend was attached. See the
// Session interface doc for the value set.
func (s *onnxSession) Provider() string {
	if s == nil {
		return ""
	}
	return s.provider
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
