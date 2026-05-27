package convert

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat_Empty(t *testing.T) {
	dir := t.TempDir()
	hasONNX, hasCoreML, hasSafe := DetectFormat(dir)
	if hasONNX || hasCoreML || hasSafe {
		t.Errorf("empty dir: onnx=%v coreml=%v safetensors=%v", hasONNX, hasCoreML, hasSafe)
	}
}

func TestDetectFormat_ONNX(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "onnx"), 0755)
	hasONNX, _, _ := DetectFormat(dir)
	if !hasONNX {
		t.Error("expected hasONNX=true when onnx/ directory exists")
	}
}

func TestDetectFormat_CoreML(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "model.mlpackage"), 0755)
	_, hasCoreML, _ := DetectFormat(dir)
	if !hasCoreML {
		t.Error("expected hasCoreML=true when .mlpackage exists")
	}
}

func TestDetectFormat_SafeTensors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("fake"), 0644)
	_, _, hasSafe := DetectFormat(dir)
	if !hasSafe {
		t.Error("expected hasSafeTensors=true when .safetensors exists")
	}
}

func TestDetectFormat_NonexistentDir(t *testing.T) {
	hasONNX, hasCoreML, hasSafe := DetectFormat("/nonexistent/path")
	if hasONNX || hasCoreML || hasSafe {
		t.Error("nonexistent dir should return all false")
	}
}

func TestRequireCommand_Go(t *testing.T) {
	// "go" should be available in test environment
	if err := requireCommand("go"); err != nil {
		t.Errorf("requireCommand(go): %v", err)
	}
}

func TestRequireCommand_Missing(t *testing.T) {
	err := requireCommand("definitely-not-a-real-command-xyz")
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}
