package query

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestEnrichService_Run_EmptyHits(t *testing.T) {
	s := &EnrichService{}
	got := s.Run(context.Background(), nil, "/tmp", 5)
	if got != nil {
		t.Errorf("expected nil for empty hits")
	}
}

func TestEnrichService_Run_NoSrcRoot(t *testing.T) {
	s := &EnrichService{}
	hits := []Hit{{Citation: types.Citation{File: "x.go"}}}
	got := s.Run(context.Background(), hits, "", 5)
	if len(got[0].GitHistory) != 0 {
		t.Errorf("expected empty history without srcRoot")
	}
}

func TestEnrichService_Run_AttachesHistory(t *testing.T) {
	// Set up a tiny git repo so git log returns real data
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	// First commit
	file := filepath.Join(dir, "x.go")
	if err := writeFile(file, "package x\n"); err != nil {
		t.Fatal(err)
	}
	run("add", "x.go")
	run("commit", "-q", "-m", "initial commit")

	// Second commit
	if err := writeFile(file, "package x\nfunc Foo() {}\n"); err != nil {
		t.Fatal(err)
	}
	run("add", "x.go")
	run("commit", "-q", "-m", "add foo")

	s := &EnrichService{}
	hits := []Hit{{Citation: types.Citation{File: "x.go"}}}
	got := s.Run(context.Background(), hits, dir, 5)
	if len(got[0].GitHistory) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(got[0].GitHistory))
	}
	// Newest first
	if got[0].GitHistory[0].Subject != "add foo" {
		t.Errorf("[0].Subject = %q, want 'add foo'", got[0].GitHistory[0].Subject)
	}
	if got[0].GitHistory[1].Subject != "initial commit" {
		t.Errorf("[1].Subject = %q, want 'initial commit'", got[0].GitHistory[1].Subject)
	}
	if got[0].GitHistory[0].Author != "Test" {
		t.Errorf("Author = %q, want Test", got[0].GitHistory[0].Author)
	}
	if got[0].GitHistory[0].Hash == "" {
		t.Error("Hash empty")
	}
	if got[0].GitHistory[0].Date == "" {
		t.Error("Date empty")
	}
}

func TestEnrichService_Run_CachesPerFile(t *testing.T) {
	// Two hits on the same file should only invoke git log once.
	// We can't easily assert subprocess count, but verify both hits
	// end up with identical history.
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-q").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "t@e.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "T").Run()
	writeFile(filepath.Join(dir, "a.go"), "package a\n")
	exec.Command("git", "-C", dir, "add", "a.go").Run()
	exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Run()

	s := &EnrichService{}
	hits := []Hit{
		{Citation: types.Citation{File: "a.go"}},
		{Citation: types.Citation{File: "a.go"}},
	}
	got := s.Run(context.Background(), hits, dir, 5)
	if len(got[0].GitHistory) == 0 {
		t.Skip("no git history (git not available)")
	}
	if len(got[0].GitHistory) != len(got[1].GitHistory) {
		t.Errorf("cached results differ: %d vs %d", len(got[0].GitHistory), len(got[1].GitHistory))
	}
	if got[0].GitHistory[0].Hash != got[1].GitHistory[0].Hash {
		t.Error("cache miss: same file returned different commits")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
