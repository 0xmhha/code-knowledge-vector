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
		mkHit("a", "func A(){}", 1, 0.1),                                // file "x.go" — doesn't exist
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

// TestEnforceCitationsAt_StaleCommitHashFlag exercises B4: when the
// chunk's recorded commit_hash differs from currentHead, the hit
// survives (file is fine) but carries StaleCitation=true and counts
// toward the stale return.
func TestEnforceCitationsAt_StaleCommitHashFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "old-commit"}},
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "new-commit"}},
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: ""}}, // unset, treat as fresh
	}
	keep, dropped, stale := EnforceCitationsAt(hits, dir, "new-commit")
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
	if stale != 1 {
		t.Errorf("expected 1 stale, got %d", stale)
	}
	if len(keep) != 3 {
		t.Errorf("expected all 3 hits to survive, got %d", len(keep))
	}
	if !keep[0].StaleCitation {
		t.Errorf("keep[0] (old-commit) should be marked stale")
	}
	if keep[1].StaleCitation {
		t.Errorf("keep[1] (new-commit) should NOT be stale")
	}
	if keep[2].StaleCitation {
		t.Errorf("keep[2] (empty hash) should NOT be stale (no signal to compare)")
	}
}

// TestEnforceCitationsAt_EmptyCurrentHeadSkipsStaleCheck verifies the
// stale check is opt-in: empty currentHead means "we don't know what
// fresh looks like" → don't mark anything stale.
func TestEnforceCitationsAt_EmptyCurrentHeadSkipsStaleCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "any-commit"}},
	}
	_, _, stale := EnforceCitationsAt(hits, dir, "")
	if stale != 0 {
		t.Errorf("empty currentHead must disable stale check, got stale=%d", stale)
	}
}

func TestEnforceCitationsRejectsInvalidLineRange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 0, EndLine: 0}}, // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 5, EndLine: 3}}, // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1}}, // ok
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 || len(keep) != 1 {
		t.Errorf("expected 2 dropped + 1 kept, got dropped=%d keep=%d", dropped, len(keep))
	}
}
