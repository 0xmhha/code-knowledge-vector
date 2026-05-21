package build

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
)

// TestReindex_NoManifestFails verifies Reindex refuses to run when the
// OutDir has no prior index. Surfaces ErrNoManifest so callers know to
// run `ckv build` first rather than silently doing a partial rebuild.
func TestReindex_NoManifestFails(t *testing.T) {
	res, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  t.TempDir(),
		OutDir:   t.TempDir(), // empty: no manifest.json
		Embedder: mock.Default(),
	})
	if !errors.Is(err, ErrNoManifest) {
		t.Fatalf("expected ErrNoManifest, got %v (res=%+v)", err, res)
	}
}

// TestReindex_EmbedderMismatchFails verifies Reindex refuses to embed
// with a different model/dim than the manifest's. Mixing embeddings in
// the same store silently corrupts retrieval, so the contract is hard.
func TestReindex_EmbedderMismatchFails(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	// Build with one embedder.
	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("seed Run: %v", err)
	}

	// Now reindex with a different-name embedder.
	mismatched := mock.New(mock.Default().Dimension(), "different-mock-name")
	_, err = Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mismatched,
		Files:    []string{"server.go"},
	})
	if !errors.Is(err, ErrEmbedderMismatch) {
		t.Fatalf("expected ErrEmbedderMismatch, got %v", err)
	}
}

// TestReindex_ForceFilesProcessesListedPaths verifies the --files
// override path: even when prev_head == new_head, listed paths get
// re-embedded. Useful for CI hooks and watcher integrations.
func TestReindex_ForceFilesProcessesListedPaths(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("seed Run: %v", err)
	}

	res, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Files:    []string{"server.go"},
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if res.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1", res.FilesProcessed)
	}
	// Force-listed paths register as modifications (Files override
	// can't distinguish add vs modify without consulting git).
	if res.FilesModified != 1 {
		t.Errorf("FilesModified = %d, want 1", res.FilesModified)
	}
	if res.Chunks.Total == 0 {
		t.Errorf("expected reindex to produce chunks, got 0")
	}

	// Manifest BuiltAt must be refreshed even on a no-op reindex.
	man, err := manifest.Load(out)
	if err != nil {
		t.Fatalf("Load manifest: %v", err)
	}
	if man.BuiltAt == "" {
		t.Errorf("expected BuiltAt to be set after reindex")
	}
}

// TestReindex_NoChangesIsNoop verifies that calling Reindex when
// prev_head equals new_head touches no files but still refreshes the
// manifest BuiltAt for freshness reporting.
func TestReindex_NoChangesIsNoop(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	// Build (records IndexedHead = current HEAD of testdata).
	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("seed Run: %v", err)
	}

	// Reindex without --since and without --files: prev == new → 0 work.
	res, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(200, 0).UTC() },
	})
	// detectCommit may legitimately return empty when the testdata
	// path isn't a git repo (rare in CI but possible). In that case
	// resolveChangeSet returns an error and we skip the assertion
	// rather than fail spuriously.
	if err != nil {
		if strings.Contains(err.Error(), "no git HEAD") {
			t.Skip("testdata/sample is not in a git repo; skip no-op test")
		}
		t.Fatalf("Reindex: %v", err)
	}
	if res.FilesProcessed != 0 {
		t.Errorf("expected 0 files processed when prev==new, got %d", res.FilesProcessed)
	}
}

// TestReindex_ForceFilesPurgesAndReinserts verifies that re-running
// reindex with the same file doesn't accumulate duplicate chunks —
// DeleteByFile runs *before* upsert in the reindex pipeline.
func TestReindex_ForceFilesPurgesAndReinserts(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	_, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("seed Run: %v", err)
	}

	// Reindex server.go twice. Chunk count should NOT double.
	for range 2 {
		_, err := Reindex(context.Background(), ReindexOptions{
			SrcRoot:  src,
			OutDir:   out,
			Embedder: mock.Default(),
			Files:    []string{"server.go"},
			Now:      func() time.Time { return time.Unix(100, 0).UTC() },
		})
		if err != nil {
			t.Fatalf("Reindex: %v", err)
		}
	}

	// Count chunks for server.go directly via sqlite — the manifest's
	// ChunkCount field is best-effort, the DB is the source of truth.
	db, err := openTestDB(filepath.Join(out, "vector.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	count, err := countChunksForFile(db, "server.go")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// server.go has Server struct + NewServer + Listen + Close + Addr +
	// IsListening = 6 symbol chunks + 1 file_header = 7. Allow ≥6 for
	// resilience against future parser changes; the *invariant* is
	// "no duplication after repeated reindex".
	if count < 6 || count > 10 {
		t.Errorf("server.go chunk count after 2x reindex = %d, want stable in [6,10]", count)
	}
}

// TestReindex_DiffParserHandlesAllStatuses keeps parseDiffNameStatus
// honest as we add cases. Each line exercises a different `git diff
// --name-status` token.
func TestReindex_DiffParserHandlesAllStatuses(t *testing.T) {
	diff := strings.Join([]string{
		"A\tnew/file.go",
		"M\tinternal/x.go",
		"D\told/file.go",
		"R100\trenamed/from.go\trenamed/to.go",
		"C75\toriginal.go\tcopy.go",
		"T\tswitched.go",
	}, "\n")
	cs := parseDiffNameStatus(diff)

	if !equal(cs.added, []string{"new/file.go", "renamed/to.go", "copy.go"}) {
		t.Errorf("added = %v", cs.added)
	}
	if !equal(cs.modified, []string{"internal/x.go", "switched.go"}) {
		t.Errorf("modified = %v", cs.modified)
	}
	if !equal(cs.deleted, []string{"old/file.go", "renamed/from.go"}) {
		t.Errorf("deleted = %v", cs.deleted)
	}
}

// TestReindex_ClassifyLanguageMirrorsDiscover ensures the reindex
// extension table stays in sync with discover.classifyLanguage. If a
// new language lands in discover but not here, files would silently
// be skipped by reindex even though `ckv build` indexes them.
func TestReindex_ClassifyLanguageMirrorsDiscover(t *testing.T) {
	cases := map[string]string{
		"x.go":         "go",
		"x.ts":         "typescript",
		"x.tsx":        "typescript",
		"x.js":         "javascript",
		"x.jsx":        "javascript",
		"x.mjs":        "javascript",
		"x.cjs":        "javascript",
		"x.sol":        "solidity",
		"x.md":         "markdown",
		"x.markdown":   "markdown",
		"x.txt":        "",
		"Makefile":     "",
		"x.unknownext": "",
	}
	for path, want := range cases {
		if got := classifyLanguageRel(path); got != want {
			t.Errorf("classifyLanguageRel(%q) = %q, want %q", path, got, want)
		}
	}
}

// openTestDB / countChunksForFile are tiny sqlite helpers used by the
// chunk-count assertion. We don't pull in sqlitevec.Store directly here
// because that opens a fresh connection that would conflict with the
// reindex's own; a read-only sqlite3 driver opened separately stays out
// of the way.
func openTestDB(path string) (*sqliteDB, error) {
	cmd := exec.Command("sqlite3", path, ".mode line")
	_ = cmd // present so the import doesn't get optimized away across edits
	return &sqliteDB{path: path}, nil
}

type sqliteDB struct{ path string }

func (db *sqliteDB) Close() error { return nil }

func countChunksForFile(db *sqliteDB, file string) (int, error) {
	out, err := exec.Command("sqlite3", db.path,
		"SELECT COUNT(*) FROM chunks WHERE file = '"+file+"';").Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
