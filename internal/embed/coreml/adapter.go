// Package coreml provides an Embedder that runs models directly via
// Apple's CoreML framework, bypassing ONNX Runtime. This gives full
// ANE (Apple Neural Engine) utilization on supported models.
//
// macOS only. On other platforms, Open returns an error.
//
// The Objective-C bridge (bridge_darwin.m) calls CoreML's MLModel
// API through CGO. The model must be in .mlpackage or .mlmodelc
// format — use coremltools to convert from ONNX/PyTorch.
//
// Usage:
//
//	ckv build --embedder=coreml --model-dir=./models/bge-m3.mlpackage
package coreml

import "fmt"

// Adapter implements types.Embedder via CoreML Framework.
// Only available on macOS (darwin) builds.
type Adapter struct {
	modelPath string
	modelName string
	dim       int
}

// Options configures the CoreML adapter.
type Options struct {
	ModelPath string // path to .mlpackage or .mlmodelc directory
	ModelName string // display name for manifest
	Dim       int    // output vector dimension (must match model)
}

// errNotAvailable is returned on non-macOS platforms.
var errNotAvailable = fmt.Errorf("coreml: only available on macOS (darwin)")
