package build

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResumeCheckpoint_RoundTripAndClear(t *testing.T) {
	dir := t.TempDir()

	c := loadCheckpoint(dir, "head1")
	if c.isDone("a.go", "sha1") {
		t.Fatal("empty checkpoint should have nothing done")
	}
	if err := c.markDone("a.go", "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := c.markDone("b.go", "sha2"); err != nil {
		t.Fatal(err)
	}
	_ = c.f.Close()
	c.f = nil

	c2 := loadCheckpoint(dir, "head1")
	if !c2.isDone("a.go", "sha1") || !c2.isDone("b.go", "sha2") {
		t.Fatalf("reload lost the done set: %+v", c2.done)
	}
	if c2.isDone("a.go", "different-sha") {
		t.Fatal("a content-hash mismatch must not count as done")
	}

	c2.clear()
	if _, err := os.Stat(filepath.Join(dir, checkpointFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clear should remove the ledger")
	}
}

func TestResumeCheckpoint_StaleHeadDiscarded(t *testing.T) {
	dir := t.TempDir()
	c := loadCheckpoint(dir, "head1")
	if err := c.markDone("a.go", "sha1"); err != nil {
		t.Fatal(err)
	}
	_ = c.f.Close()

	// A reindex toward a different head must not inherit the old head's ledger.
	c2 := loadCheckpoint(dir, "head2")
	if c2.isDone("a.go", "sha1") {
		t.Fatal("stale checkpoint (different target head) must be discarded")
	}
	if _, err := os.Stat(filepath.Join(dir, checkpointFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale checkpoint file should be removed on load")
	}
}
