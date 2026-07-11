package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/prdoc"
)

func TestPRCutoff(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	metas := []prdoc.PRMeta{
		{Repo: "o/r", PRNumber: 63, MergedAt: base},
		{Repo: "o/r", PRNumber: 109, MergedAt: base.Add(48 * time.Hour)}, // newest number+date
		{Repo: "o/r", PRNumber: 70, MergedAt: base.Add(24 * time.Hour)},
	}
	c := prCutoff(metas)
	if c == nil || c.Repo != "o/r" || c.LastPRNumber != 109 {
		t.Fatalf("cutoff = %+v, want repo=o/r last=109", c)
	}
	if c.LastMergedAt != base.Add(48*time.Hour).Format(time.RFC3339) {
		t.Errorf("LastMergedAt = %q", c.LastMergedAt)
	}
	if prCutoff(nil) != nil {
		t.Error("empty metas must yield nil")
	}
}

func TestContentHash_FileAndTree_DeterministicAndSensitive(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := contentHash(f)
	if h1 == "" || h1 != contentHash(f) {
		t.Fatal("file hash must be non-empty and deterministic")
	}
	_ = os.WriteFile(f, []byte("world"), 0o644)
	if contentHash(f) == h1 {
		t.Error("file hash must change with content")
	}

	// tree hash: deterministic + sensitive to an added file
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "b.md"), []byte("x"), 0o644)
	th1 := contentHash(dir)
	if th1 == "" || th1 != contentHash(dir) {
		t.Fatal("tree hash must be non-empty and deterministic")
	}
	_ = os.WriteFile(filepath.Join(sub, "c.md"), []byte("y"), 0o644)
	if contentHash(dir) == th1 {
		t.Error("tree hash must change when a file is added")
	}
	if contentHash(filepath.Join(dir, "nope")) != "" {
		t.Error("missing path must hash to empty")
	}
}
