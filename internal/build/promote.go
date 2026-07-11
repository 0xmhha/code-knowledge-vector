package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
)

// PromoteVersion atomically points <dataset>/current at the built index in
// <dataset>/<version>, after an integrity gate (reindex-migration-design
// §4.1/§5.1). CKS orchestrates *when* to promote and version naming/GC; this is
// the CKV-side atomic-swap primitive it calls.
//
// The version must be a completed build (readable manifest) that passes the
// integrity gate (no orphan chunks/vectors). The swap creates a temporary
// relative symlink and renames it over `current`, so a concurrent reader
// resolving `current` sees either the old or the new target, never a missing
// link.
func PromoteVersion(dataset, version string) error {
	versionDir := filepath.Join(dataset, version)

	m, err := manifest.Load(versionDir)
	if err != nil {
		return fmt.Errorf("promote: version %q has no valid manifest: %w", version, err)
	}

	// Integrity gate: no serving traffic points at an index with orphans.
	store, err := sqlitevec.Open(filepath.Join(versionDir, "vector.db"), m.EmbeddingDim)
	if err != nil {
		return fmt.Errorf("promote: open %q store: %w", version, err)
	}
	val, verr := store.Validate(context.Background())
	_ = store.Close()
	if verr != nil {
		return fmt.Errorf("promote: validate %q: %w", version, verr)
	}
	if !val.OK() {
		return fmt.Errorf("promote: version %q failed integrity gate: %d orphan chunks, %d orphan vectors",
			version, val.OrphanChunks, val.OrphanVectors)
	}

	// Atomic swap: temp relative symlink → rename over `current`.
	current := filepath.Join(dataset, "current")
	tmp := filepath.Join(dataset, fmt.Sprintf(".ckv-current.tmp-%d", os.Getpid()))
	_ = os.Remove(tmp)
	if err := os.Symlink(version, tmp); err != nil {
		return fmt.Errorf("promote: create temp link: %w", err)
	}
	if err := os.Rename(tmp, current); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote: swap current: %w", err)
	}
	return nil
}
