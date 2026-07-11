package build

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

// TestReindex_ResumeSkipsCheckpointedFile is the P4c guard: a reindex must skip
// files an interrupted prior run already re-embedded (recorded in the resume
// checkpoint for the same target head, matched by content hash), rather than
// re-processing them.
func TestReindex_ResumeSkipsCheckpointedFile(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()
	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	newHead, _ := detectCommit(src)
	if newHead == "" {
		t.Skip("testdata/sample not under git — reindex precondition unavailable")
	}

	// Pre-seed a checkpoint marking server.go as already re-embedded for newHead.
	c := loadCheckpoint(out, newHead)
	sha := fileSHA(filepath.Join(src, "server.go"))
	if err := c.markDone("server.go", sha); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	_ = c.f.Close()

	res, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Files:    []string{"server.go"},
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if res.FilesResumed != 1 {
		t.Fatalf("FilesResumed = %d, want 1 (server.go was checkpointed)", res.FilesResumed)
	}
	if res.FilesProcessed != 0 {
		t.Fatalf("FilesProcessed = %d, want 0 (server.go should be skipped)", res.FilesProcessed)
	}
}
