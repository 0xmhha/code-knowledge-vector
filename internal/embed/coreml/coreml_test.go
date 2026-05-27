package coreml

import (
	"context"
	"runtime"
	"testing"
)

func TestOpen_RequiresModelPath(t *testing.T) {
	_, err := Open(Options{Dim: 768})
	if err == nil {
		t.Fatal("expected error when ModelPath is empty")
	}
}

func TestOpen_RequiresPositiveDim(t *testing.T) {
	_, err := Open(Options{ModelPath: "/fake/model.mlpackage"})
	if err == nil && runtime.GOOS == "darwin" {
		t.Fatal("expected error when Dim <= 0")
	}
}

func TestAdapter_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this test verifies non-darwin behavior")
	}
	_, err := Open(Options{ModelPath: "/fake", ModelName: "test", Dim: 768})
	if err == nil {
		t.Fatal("expected error on non-darwin platform")
	}
}

func TestAdapter_InterfaceMethods(t *testing.T) {
	a := &Adapter{modelName: "test", dim: 768, maxSeqLen: 512}
	if runtime.GOOS != "darwin" {
		if a.Name() != "" {
			t.Errorf("non-darwin Name should be empty")
		}
		return
	}
	if a.Name() != "test" {
		t.Errorf("Name = %q, want test", a.Name())
	}
	if a.Dimension() != 768 {
		t.Errorf("Dimension = %d, want 768", a.Dimension())
	}
	if a.MaxInputTokens() != 512 {
		t.Errorf("MaxInputTokens = %d, want 512", a.MaxInputTokens())
	}
}

func TestEmbed_EmptyBatch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	a := &Adapter{modelName: "test", dim: 768, maxSeqLen: 512}
	vecs, err := a.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty batch")
	}
}

func TestEmbed_RequiresLoadedModel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	// No model loaded → predict should fail with error, not panic
	a := &Adapter{modelName: "test", dim: 768, maxSeqLen: 32}
	_, err := a.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error when no model is loaded")
	}
}
