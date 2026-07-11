package build

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

func buildVersion(t *testing.T, dir string) {
	t.Helper()
	src := resolveTestdataSample(t)
	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   dir,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("build version %s: %v", dir, err)
	}
}

// TestPromoteVersion_AtomicSwapAndGate verifies promote points `current` at a
// valid version, re-points on a subsequent promote, and refuses a version with
// no manifest (integrity gate).
func TestPromoteVersion_AtomicSwapAndGate(t *testing.T) {
	root := t.TempDir()

	buildVersion(t, filepath.Join(root, "v1"))
	if err := PromoteVersion(root, "v1"); err != nil {
		t.Fatalf("promote v1: %v", err)
	}
	if got := resolvedVersion(t, root); got != "v1" {
		t.Fatalf("current resolves to %q, want v1", got)
	}

	buildVersion(t, filepath.Join(root, "v2"))
	if err := PromoteVersion(root, "v2"); err != nil {
		t.Fatalf("promote v2: %v", err)
	}
	if got := resolvedVersion(t, root); got != "v2" {
		t.Fatalf("after re-promote, current resolves to %q, want v2", got)
	}

	if err := PromoteVersion(root, "does-not-exist"); err == nil {
		t.Fatalf("expected error promoting a version with no manifest")
	}
	// The failed promote must not have moved `current` off v2.
	if got := resolvedVersion(t, root); got != "v2" {
		t.Fatalf("failed promote moved current to %q, want v2", got)
	}
}

func resolvedVersion(t *testing.T, root string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	return filepath.Base(resolved)
}
