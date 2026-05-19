package bgeonnx

import "context"

// Session is the ONNX Runtime concern: take tokenized tensors, run
// the model graph, return pooled+normalized 1024-d vectors.
//
// Production impl (D1-FU-1): `yalue/onnxruntime_go` behind the
// `bgeonnx` build tag so existing CI without libonnxruntime stays green.
//
// Pooling + normalization happen here, not at the caller. bge-* models
// use mean pooling over the token dimension (masked by attention),
// then L2 normalize. Doing it inside Session keeps the interface
// uniform across embedders.
type Session interface {
	// Run executes one batch through the graph. Output is
	// len(batch) × ModelDim float32 vectors, already L2-normalized.
	Run(ctx context.Context, tokens TokenizedBatch) ([][]float32, error)

	// Provider names the execution backend the session ended up using.
	// Values are short tokens suitable for log fields:
	//   "cpu"      — default ORT CPU EP, no acceleration attached.
	//   "coreml"   — CoreML EP V2 attached and accepted on Apple
	//                hardware.
	//   "coreml-fallback-to-cpu" — CoreML attach attempted but failed
	//                at session creation; ORT silently uses CPU.
	//   "stub"     — non-bgeonnx build; no real session.
	// Surfaced via Adapter.Provider() so the build footprint records
	// which backend ran an index.
	Provider() string

	// Close releases the underlying ONNX session + environment.
	// Idempotent.
	Close() error
}

// stubSession returns ErrNotImplemented. Used until D1-FU-1 lands.
type stubSession struct{}

func (stubSession) Run(_ context.Context, _ TokenizedBatch) ([][]float32, error) {
	return nil, ErrNotImplemented
}

func (stubSession) Provider() string { return "stub" }

func (stubSession) Close() error { return nil }
