package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

// TestAcquireDatasetLock_ExclusiveAndRelease verifies the advisory lock is
// exclusive (a second acquire fails with ErrLocked) and re-acquirable after
// release.
func TestAcquireDatasetLock_ExclusiveAndRelease(t *testing.T) {
	dir := t.TempDir()

	l1, err := acquireDatasetLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := acquireDatasetLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire: got %v, want ErrLocked", err)
	}

	l1.release()

	l2, err := acquireDatasetLock(dir)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	l2.release()
}

// TestRun_RefusesWhenLocked verifies Run acquires the dataset lock and refuses
// with ErrLocked when another holder has it.
func TestRun_RefusesWhenLocked(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	held, err := acquireDatasetLock(out)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer held.release()

	_, err = Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Run under held lock: got %v, want ErrLocked", err)
	}
}
