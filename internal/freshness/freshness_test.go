package freshness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit creates a tiny git repo at dir with one commit, returning
// the resulting HEAD sha. Tests use this to exercise the real `git`
// binary against a known fixture rather than mocking exec.Command.
func gitInit(t *testing.T, dir string) string {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "a.go"},
		{"commit", "-q", "-m", "first"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %v: %s", err, out)
	}
	return string(out[:len(out)-1])
}

func TestCheckFreshWhenHeadsMatch(t *testing.T) {
	dir := t.TempDir()
	head := gitInit(t, dir)
	r, err := Check(dir, head)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if r.Stale || !r.Fresh {
		t.Errorf("expected fresh, got Stale=%v Fresh=%v", r.Stale, r.Fresh)
	}
	if len(r.ChangedFiles) != 0 {
		t.Errorf("unexpected changes: %v", r.ChangedFiles)
	}
}

func TestCheckStaleAndListsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	indexedHead := gitInit(t, dir)
	// Add a second commit so HEAD drifts.
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "b.go"}, {"commit", "-q", "-m", "add b"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}

	r, err := Check(dir, indexedHead)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !r.Stale {
		t.Error("expected Stale=true after second commit")
	}
	if len(r.ChangedFiles) != 1 || r.ChangedFiles[0] != "b.go" {
		t.Errorf("expected changed = [b.go], got %v", r.ChangedFiles)
	}
}

func TestCheckEmitsWarningOnNonGit(t *testing.T) {
	r, err := Check(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(r.Warnings) == 0 {
		t.Errorf("expected git_unavailable warning, got none")
	}
}
