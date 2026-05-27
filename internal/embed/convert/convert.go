// Package convert wraps external model conversion tools (optimum-cli,
// coremltools) as subprocess calls. CKV does not implement conversion
// logic itself — it delegates to well-tested Python tools.
//
// Supported conversions:
//   - PyTorch/SafeTensors → ONNX  (via HuggingFace optimum-cli)
//   - ONNX → CoreML .mlpackage   (via Apple coremltools)
package convert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ToONNX converts a HuggingFace model to ONNX format using optimum-cli.
// modelID is the HuggingFace model identifier (e.g. "BAAI/bge-m3") or
// a local directory containing a PyTorch model. outputDir receives the
// converted ONNX files.
//
// Requires: pip install optimum[exporters]
func ToONNX(ctx context.Context, modelID, outputDir string) error {
	if err := requireCommand("optimum-cli"); err != nil {
		return fmt.Errorf("ONNX conversion requires optimum-cli: %w\nInstall: pip install optimum[exporters]", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	cmd := exec.CommandContext(ctx,
		"optimum-cli", "export", "onnx",
		"--model", modelID,
		outputDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("optimum-cli export failed: %w", err)
	}
	return nil
}

// ToCoreML converts an ONNX model to CoreML .mlpackage format using
// coremltools. inputPath is the path to model.onnx. outputPath is
// the destination .mlpackage directory.
//
// Requires: pip install coremltools
func ToCoreML(ctx context.Context, inputPath, outputPath string) error {
	if err := requireCommand("python3"); err != nil {
		return fmt.Errorf("CoreML conversion requires python3: %w", err)
	}

	script := fmt.Sprintf(`
import coremltools as ct
import onnx

onnx_model = onnx.load(%q)
ml_model = ct.converters.onnx.convert(model=onnx_model)
ml_model.save(%q)
print("Conversion complete:", %q)
`, inputPath, outputPath, outputPath)

	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("coremltools conversion failed: %w\nInstall: pip install coremltools", err)
	}
	return nil
}

// DetectFormat inspects a model directory and returns which formats
// are available.
func DetectFormat(modelDir string) (hasONNX, hasCoreML, hasSafeTensors bool) {
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == "onnx" || filepath.Ext(name) == ".onnx":
			hasONNX = true
		case filepath.Ext(name) == ".mlpackage" || filepath.Ext(name) == ".mlmodelc":
			hasCoreML = true
		case filepath.Ext(name) == ".safetensors":
			hasSafeTensors = true
		}
	}
	return
}

func requireCommand(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return nil
}
