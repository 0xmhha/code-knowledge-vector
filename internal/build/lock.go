package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrLocked signals another ckv build/reindex already holds the dataset lock.
var ErrLocked = errors.New("build: dataset is locked by another ckv build/reindex")

// dirLock is an advisory (flock) exclusive lock on a dataset directory. It
// serializes concurrent build/reindex runs that would otherwise interleave
// writes to the same vector.db (reindex-migration-design §5.3, "Concurrent
// builds undefined"). flock is released when the fd closes — on unlock or when
// the process exits — so a crash never leaves a stuck lock.
type dirLock struct{ f *os.File }

// acquireDatasetLock takes a non-blocking exclusive flock on <dir>/.ckv.lock.
// dir must already exist. Returns ErrLocked (wrapped) when another process
// holds the lock, rather than blocking.
func acquireDatasetLock(dir string) (*dirLock, error) {
	path := filepath.Join(dir, ".ckv.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("dataset lock: open %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, dir)
		}
		return nil, fmt.Errorf("dataset lock: flock %s: %w", path, err)
	}
	return &dirLock{f: f}, nil
}

// release drops the lock and closes the fd. Safe to call once; nil-safe.
func (l *dirLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
