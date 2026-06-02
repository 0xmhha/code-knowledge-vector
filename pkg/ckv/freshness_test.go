package ckv_test

// Structured-Freshness contract test (S-11, 02 §4 / plan Step 3). Proves the
// NEW pkg/ckv.Engine.Freshness() returns the full freshness.Report through the
// public facade AND that an external package can read every field of
// ckv.FreshnessReport (the internal-type-alias re-export must be reachable
// across the module boundary — the risk flagged in plan Part D #3).
//
// The test drives a REAL fresh→stale transition through the public API: it
// builds an index at a temp git repo's HEAD, asserts Fresh, then makes a second
// commit (without rebuilding) and asserts Stale + populated ChangedFiles. The
// IndexedHead is frozen in the manifest at build time; Freshness() re-runs git
// live each call, so the new commit alone flips the verdict — no mocking.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

// git runs a git command in dir, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestFreshness_StructuredFreshThenStale(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// 1. Temp git repo with one parseable Go file → HEAD1.
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	srcFile := filepath.Join(repo, "svc.go")
	if err := os.WriteFile(srcFile, []byte(
		"package svc\n\n// Listen starts the server.\nfunc Listen(addr string) error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "initial")

	// 2. Build a mock index at that HEAD.
	out := t.TempDir()
	if _, err := build.Run(context.Background(), build.Options{
		SrcRoot:  repo,
		OutDir:   out,
		Embedder: ckv.MockEmbedder(),
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("ckv.Open: %v", err)
	}
	defer func() { _ = engine.Close() }()

	// 3. Fresh path: index HEAD == current HEAD.
	var rep ckv.FreshnessReport = mustFreshness(t, engine)
	if !rep.Fresh || rep.Stale {
		t.Errorf("fresh build: Fresh=%v Stale=%v, want Fresh=true Stale=false (warnings=%v)",
			rep.Fresh, rep.Stale, rep.Warnings)
	}
	if rep.IndexedHead == "" || rep.CurrentHead == "" {
		t.Errorf("expected populated heads, got Indexed=%q Current=%q", rep.IndexedHead, rep.CurrentHead)
	}
	if rep.IndexedHead != rep.CurrentHead {
		t.Errorf("fresh: IndexedHead %q != CurrentHead %q", rep.IndexedHead, rep.CurrentHead)
	}

	// 4. Second commit WITHOUT rebuilding → index goes stale. Freshness()
	// recomputes CurrentHead live; IndexedHead stays frozen in the manifest.
	if err := os.WriteFile(srcFile, []byte(
		"package svc\n\n// Listen starts the server (v2).\nfunc Listen(addr string) error { return nil }\n\nfunc Stop() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "commit", "-qam", "change")

	rep = mustFreshness(t, engine)
	if rep.Fresh || !rep.Stale {
		t.Errorf("after new commit: Fresh=%v Stale=%v, want Fresh=false Stale=true", rep.Fresh, rep.Stale)
	}
	if len(rep.ChangedFiles) == 0 {
		t.Errorf("stale: expected non-empty ChangedFiles, got none")
	}
	var sawSvc bool
	for _, f := range rep.ChangedFiles {
		if filepath.Base(f) == "svc.go" {
			sawSvc = true
		}
	}
	if !sawSvc {
		t.Errorf("stale: svc.go not in ChangedFiles %v", rep.ChangedFiles)
	}
}

// mustFreshness asserts Freshness() returns no hard error (engine-closed / no
// manifest) — git-unavailability is reported via warnings, not error.
func mustFreshness(t *testing.T, e *ckv.Engine) ckv.FreshnessReport {
	t.Helper()
	rep, err := e.Freshness()
	if err != nil {
		t.Fatalf("Freshness(): %v", err)
	}
	return rep
}
