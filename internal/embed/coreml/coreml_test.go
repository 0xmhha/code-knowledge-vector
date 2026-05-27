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
	a := &Adapter{modelName: "test", dim: 768}
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
}

func TestEmbed_NotYetImplemented(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	a := &Adapter{modelName: "test", dim: 768}
	_, err := a.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error — Embed is placeholder")
	}
}
