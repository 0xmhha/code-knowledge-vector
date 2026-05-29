package build

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

// TestRun_ConventionExtraction_EndToEnd builds the testdata/sample
// corpus and asserts that ChunkConvention rows land in the store with
// the convention_stats column populated.
func TestRun_ConventionExtraction_EndToEnd(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	res, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Chunks.Total == 0 {
		t.Fatal("expected at least one chunk")
	}

	dbPath := filepath.Join(out, "vector.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var convCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE chunk_kind = 'convention'`).Scan(&convCount)
	if err != nil {
		t.Fatalf("count convention: %v", err)
	}
	if convCount == 0 {
		t.Fatal("expected at least one convention chunk")
	}

	// convention_stats column should hold JSON.
	var stats string
	err = db.QueryRow(
		`SELECT convention_stats FROM chunks WHERE chunk_kind = 'convention' AND convention_stats != '' LIMIT 1`,
	).Scan(&stats)
	if err != nil {
		t.Fatalf("read convention_stats: %v", err)
	}
	// Always-present keys (set even when the underlying count is 0).
	// Logger / errgroup keys appear only when detected, so we don't
	// assert them here — the unit tests cover that path.
	for _, want := range []string{"file_count", "new_constructors", "mutexes", "channels"} {
		if !strings.Contains(stats, want) {
			t.Errorf("convention_stats should contain %q: %s", want, stats)
		}
	}

	// Text column should contain the deterministic summary.
	var text string
	err = db.QueryRow(
		`SELECT text FROM chunks WHERE chunk_kind = 'convention' LIMIT 1`,
	).Scan(&text)
	if err != nil {
		t.Fatalf("read convention text: %v", err)
	}
	if !strings.HasPrefix(text, "package:") {
		t.Errorf("convention text should start with 'package:' summary, got %q", text)
	}
}
