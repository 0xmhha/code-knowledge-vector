package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestEnforceCitationsDropsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		mkHit("a", "func A(){}", 1, 0.1),                          // file "x.go" — doesn't exist
		{Chunk: types.Chunk{File: "real.go", StartLine: 1, EndLine: 1}}, // exists
		{Chunk: types.Chunk{File: "missing.go", StartLine: 1, EndLine: 1}},
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 {
		t.Errorf("expected 2 dropped, got %d", dropped)
	}
	if len(keep) != 1 || keep[0].Chunk.File != "real.go" {
		t.Errorf("expected only real.go kept, got %+v", keep)
	}
}

func TestEnforceCitationsPassesThroughWhenNoSrcRoot(t *testing.T) {
	hits := []types.Hit{
		mkHit("a", "func A(){}", 1, 0.1),
	}
	keep, dropped := EnforceCitations(hits, "")
	if len(keep) != 1 || dropped != 0 {
		t.Errorf("empty srcRoot must pass through: keep=%d dropped=%d", len(keep), dropped)
	}
}

func TestEnforceCitationsRejectsInvalidLineRange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 0, EndLine: 0}},  // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 5, EndLine: 3}}, // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1}}, // ok
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 || len(keep) != 1 {
		t.Errorf("expected 2 dropped + 1 kept, got dropped=%d keep=%d", dropped, len(keep))
	}
}
