package build

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
)

// writeCKGWithNode writes a minimal ckg graph.db carrying one alignment node
// that matches testdata/sample/server.go's NewServer (line 15) plus the
// manifest pin coordinates, so a CKV build/reindex can inherit its
// canonical_id. Schema 1.23 + a populated canonical_id make the aligner treat
// canonical_id as a trustworthy join key (ADR-007 gate).
func writeCKGWithNode(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE nodes (id TEXT PRIMARY KEY, qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, canonical_id TEXT);
CREATE TABLE manifest (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO manifest VALUES ('src_commit','seedcommit'),('schema_version','1.23');
INSERT INTO nodes VALUES ('n1','NewServer','server.go',15,20,'sample.NewServer');
`); err != nil {
		t.Fatal(err)
	}
	return dir
}

// serverChunkAlignment opens the built store and reports how many server.go
// chunks carry a non-empty canonical_id.
func serverChunkAlignment(t *testing.T, outDir string) (total, aligned int) {
	t.Helper()
	st, err := sqlitevec.Open(filepath.Join(outDir, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	chunks, err := st.LookupByFileOrdered(context.Background(), "server.go")
	if err != nil {
		t.Fatalf("lookup server.go: %v", err)
	}
	for _, c := range chunks {
		total++
		if c.CanonicalID != "" {
			aligned++
		}
	}
	return total, aligned
}

// TestReindex_PreservesCanonicalAlignment is the P2a guard: reindex must
// re-run ckgalign against the ckg graph recorded in manifest.Sources.CKG.Path
// so re-embedded chunks keep their canonical_id join key. Without
// realignment the reindexed chunks silently lose canonical_id — the
// "quietly stale" gap in reindex-migration-design §0.2 gap1.
func TestReindex_PreservesCanonicalAlignment(t *testing.T) {
	src := resolveTestdataSample(t)
	ckg := writeCKGWithNode(t)
	out := t.TempDir()

	// Seed: build with --ckg. This stamps canonical_id on the NewServer
	// chunk and records the ckg path in manifest.Sources.CKG.Path.
	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		CKGPath:  ckg,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	if _, aligned := serverChunkAlignment(t, out); aligned == 0 {
		t.Fatalf("seed build did not stamp canonical_id on server.go — fixture/alignment broken")
	}

	// Reindex server.go WITHOUT passing the ckg path: reindex must recover
	// it from the manifest and re-align.
	if _, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Files:    []string{"server.go"},
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	}); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	total, aligned := serverChunkAlignment(t, out)
	if total == 0 {
		t.Fatalf("no server.go chunks after reindex")
	}
	if aligned == 0 {
		t.Fatalf("reindex dropped canonical_id on all %d server.go chunks — realignment missing (P2a)", total)
	}
}

// writeCKGGraph writes (or overwrites) a ckg graph.db at dir with one node for
// server.go:15 carrying the given canonical_id, plus manifest pin coords
// (schema_version + graph_digest). Overwriting with a new (digest, canonical,
// schema) simulates a CKG graph regeneration / schema bump under the same
// source commit.
func writeCKGGraph(t *testing.T, dir, digest, canonical, schema string) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS manifest;
CREATE TABLE nodes (id TEXT PRIMARY KEY, qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, canonical_id TEXT);
CREATE TABLE manifest (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO manifest VALUES ('src_commit','seedcommit'),('schema_version','` + schema + `'),('graph_digest','` + digest + `');
INSERT INTO nodes VALUES ('n1','NewServer','server.go',15,20,'` + canonical + `');
`); err != nil {
		t.Fatal(err)
	}
}

// serverCanonical returns the canonical_id of the aligned server.go chunk (the
// only node in the fixture), or "" if none is aligned.
func serverCanonical(t *testing.T, outDir string) string {
	t.Helper()
	st, err := sqlitevec.Open(filepath.Join(outDir, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	chunks, err := st.LookupByFileOrdered(context.Background(), "server.go")
	if err != nil {
		t.Fatalf("lookup server.go: %v", err)
	}
	for _, c := range chunks {
		if c.CanonicalID != "" {
			return c.CanonicalID
		}
	}
	return ""
}

// TestReindex_RealignsOnGraphDigestChange is the P2b-1 guard: when the CKG
// graph is regenerated under the SAME source commit (its logical digest
// changes) the git diff is empty, so P2a's per-changed-file re-alignment never
// runs. reindex must detect the digest change and re-align every chunk against
// the new graph. Without it, canonical_id stays silently stale
// (reindex-migration-design §1 "CKG graph regeneration" row, §0.2 gap1).
func TestReindex_RealignsOnGraphDigestChange(t *testing.T) {
	src := resolveTestdataSample(t)
	ckgDir := t.TempDir()
	writeCKGGraph(t, ckgDir, "digestA", "sample.NewServer.OLD", "1.23")
	out := t.TempDir()

	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		CKGPath:  ckgDir,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	if got := serverCanonical(t, out); got != "sample.NewServer.OLD" {
		t.Fatalf("seed canonical = %q, want sample.NewServer.OLD", got)
	}

	// Regenerate the graph in place: new digest + new canonical, same commit
	// and schema.
	writeCKGGraph(t, ckgDir, "digestB", "sample.NewServer.NEW", "1.23")

	// Reindex with no source change. prev == new HEAD → empty git diff, so
	// only the graph-digest-mismatch full re-align can update canonical_id.
	if _, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	}); err != nil {
		// testdata/sample not under git in this environment → the reindex
		// precondition (a resolvable HEAD) is unavailable; skip rather than
		// fail spuriously (mirrors TestReindex_NoChangesIsNoop).
		t.Skipf("reindex precondition unavailable: %v", err)
	}

	if got := serverCanonical(t, out); got != "sample.NewServer.NEW" {
		t.Fatalf("after graph regen + reindex, canonical = %q, want sample.NewServer.NEW — P2b full re-align missing", got)
	}
}

// TestReindex_ReconcilesChunkCount is the P2b-2 count guard: the old manifest
// ChunkCount arithmetic (+= Total-(deleted+modified)) drifts because a
// re-embedded file is deleted then re-inserted with the same chunks — the net
// count is unchanged while the arithmetic adds Total-1. reindex must set
// ChunkCount to the authoritative SELECT COUNT(*).
func TestReindex_ReconcilesChunkCount(t *testing.T) {
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
	if _, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Files:    []string{"server.go"},
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	}); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	man, err := manifest.Load(out)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	st, err := sqlitevec.Open(filepath.Join(out, "vector.db"), mock.Default().Dimension())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	stats, err := st.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if man.ChunkCount != stats.ChunkCount {
		t.Fatalf("manifest ChunkCount=%d != store COUNT(*)=%d — reconciliation missing (P2b-2)",
			man.ChunkCount, stats.ChunkCount)
	}
}

// TestReindex_ValidationReport checks the P2b-2 integrity report: reindex
// returns authoritative counts with no orphans and, against a graph with an
// aligned node, non-zero canonical coverage.
func TestReindex_ValidationReport(t *testing.T) {
	src := resolveTestdataSample(t)
	ckg := writeCKGWithNode(t)
	out := t.TempDir()
	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		CKGPath:  ckg,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
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

	v := res.Validation
	if v.Chunks == 0 {
		t.Fatalf("validation reported 0 chunks")
	}
	if !v.OK() {
		t.Fatalf("integrity: %d orphan chunks, %d orphan vectors", v.OrphanChunks, v.OrphanVectors)
	}
	if v.Chunks != v.Vectors {
		t.Fatalf("every chunk must have a vector: chunks=%d vectors=%d", v.Chunks, v.Vectors)
	}
	if v.SymbolChunks == 0 {
		t.Fatalf("expected symbol chunks in testdata/sample")
	}
	if v.CanonicalChunks == 0 {
		t.Fatalf("expected >=1 canonical chunk (server.go NewServer aligned), got 0")
	}
}

// TestReindex_RefusesOnSchemaBump is the P2b-3 guard: a CKG cache schema bump
// (recorded vs current schema_version) cold-rebuilds the graph and can change
// canonical_id semantics wholesale, so a partial reindex — even with
// re-alignment — is unsafe. reindex must refuse with ErrSchemaCascade and
// direct the caller to a full build (reindex-migration-design §3.2).
func TestReindex_RefusesOnSchemaBump(t *testing.T) {
	src := resolveTestdataSample(t)
	ckgDir := t.TempDir()
	writeCKGGraph(t, ckgDir, "digestA", "sample.NewServer", "1.22")
	out := t.TempDir()
	if _, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		CKGPath:  ckgDir,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}

	// CKG cache schema bump: 1.22 → 1.23 (with a new digest, as a cold rebuild
	// would produce).
	writeCKGGraph(t, ckgDir, "digestB", "sample.NewServer", "1.23")

	_, err := Reindex(context.Background(), ReindexOptions{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Files:    []string{"server.go"},
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if !errors.Is(err, ErrSchemaCascade) {
		t.Fatalf("expected ErrSchemaCascade on CKG schema bump, got %v", err)
	}
}
