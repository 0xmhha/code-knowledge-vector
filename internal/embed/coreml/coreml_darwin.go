//go:build darwin

package coreml

/*
#cgo LDFLAGS: -framework CoreML -framework Foundation
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

// Open loads a CoreML model (.mlpackage or .mlmodelc) and returns an
// Adapter that runs inference on the ANE/GPU/CPU via CoreML.
func Open(opts Options) (*Adapter, error) {
	if opts.ModelPath == "" {
		return nil, fmt.Errorf("coreml: model path is required")
	}
	if opts.Dim <= 0 {
		return nil, fmt.Errorf("coreml: dimension must be > 0")
	}

	cPath := C.CString(opts.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	result := C.ckv_coreml_load(cPath)
	if result != 0 {
		return nil, fmt.Errorf("coreml: failed to load model from %s (error %d)", opts.ModelPath, result)
	}

	return &Adapter{
		modelPath: opts.ModelPath,
		modelName: opts.ModelName,
		dim:       opts.Dim,
	}, nil
}

func (a *Adapter) Name() string        { return a.modelName }
func (a *Adapter) Dimension() int      { return a.dim }
func (a *Adapter) MaxInputTokens() int { return 512 }

func (a *Adapter) Close() error {
	C.ckv_coreml_unload()
	return nil
}

// Embed runs inference via CoreML. Tokenization must be done before
// calling this method (the bridge expects token IDs, not raw text).
// For the initial integration, this is a placeholder that returns an
// error — full implementation requires the tokenizer + bridge wiring.
func (a *Adapter) Embed(_ context.Context, batch []string) ([][]float32, error) {
	return nil, fmt.Errorf("coreml: Embed not yet implemented — bridge wiring in progress")
}
