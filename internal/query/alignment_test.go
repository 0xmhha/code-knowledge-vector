package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

// mkCKGFixture writes a minimal ckg graph.db (empty nodes + a manifest table
// carrying the pin coordinates) so a CKV build can align against it.
func mkCKGFixture(t *testing.T, commit, schema, digest string) string {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE nodes (id TEXT PRIMARY KEY, qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, canonical_id TEXT);
CREATE TABLE manifest (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO manifest VALUES ('src_commit','` + commit + `'),('schema_version','` + schema + `')` +
		func() string {
			if digest != "" {
				return `,('graph_digest','` + digest + `')`
			}
			return ""
		}() + `;`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	return dir
}

func setCKGCommit(t *testing.T, ckgDir, commit string) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(ckgDir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE manifest SET value=? WHERE key='src_commit'`, commit); err != nil {
		t.Fatal(err)
	}
}

func buildAlignedIndex(t *testing.T, ckgDir string) *Engine {
	t.Helper()
	srcAbs, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	out := t.TempDir()
	opts := build.Options{
		SrcRoot: srcAbs, OutDir: out, Embedder: mock.Default(),
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	}
	if ckgDir != "" {
		opts.CKGPath = ckgDir
	}
	if _, err := build.Run(context.Background(), opts); err != nil {
		t.Fatalf("build: %v", err)
	}
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

func TestCheckAlignment_OK_Degraded_Mismatch_NA(t *testing.T) {
	// OK: recorded commit+digest == current.
	ckg := mkCKGFixture(t, "commitAAAA0000", "1.23", "deadbeefdigest")
	if got := buildAlignedIndex(t, ckg).CheckAlignment(); got.Status != AlignmentOK {
		t.Errorf("digest match: status=%q reason=%q, want ok", got.Status, got.Reason)
	}

	// Degraded: digest not published (commit matches, digest unverifiable).
	ckg2 := mkCKGFixture(t, "commitBBBB0000", "1.23", "")
	if got := buildAlignedIndex(t, ckg2).CheckAlignment(); got.Status != AlignmentDegraded {
		t.Errorf("no digest: status=%q, want degraded (warnings=%v)", got.Status, got.Warnings)
	}

	// Mismatch: ckg src_commit changes after the build.
	ckg3 := mkCKGFixture(t, "commitCCCC0000", "1.23", "digest3")
	eng := buildAlignedIndex(t, ckg3)
	setCKGCommit(t, ckg3, "commitDDDD9999")
	if got := eng.CheckAlignment(); got.Status != AlignmentMismatch || got.Serviceable() {
		t.Errorf("commit drift: status=%q serviceable=%v, want mismatch/not-serviceable", got.Status, got.Serviceable())
	}

	// NotAligned: index built without --ckg.
	if got := buildAlignedIndex(t, "").CheckAlignment(); got.Status != AlignmentNotAligned || !got.Serviceable() {
		t.Errorf("no ckg: status=%q, want not_aligned/serviceable", got.Status)
	}
}
