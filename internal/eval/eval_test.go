package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// build a sample index for eval tests.
func newSampleEngine(t *testing.T) (*query.Engine, string) {
	t.Helper()
	srcAbs, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	outDir := t.TempDir()
	if _, err := build.Run(context.Background(), build.Options{
		SrcRoot:  srcAbs,
		OutDir:   outDir,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("build: %v", err)
	}
	eng, err := query.Open(outDir, mock.Default())
	if err != nil {
		t.Fatalf("query.Open: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng, srcAbs
}

func TestLoadFixtureFromDisk(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "queries.yaml"))
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if fx.SchemaVersion != FixtureSchemaVersion {
		t.Errorf("schema_version = %q, want %q", fx.SchemaVersion, FixtureSchemaVersion)
	}
	if len(fx.Queries) < 5 {
		t.Fatalf("expected ≥5 queries, got %d", len(fx.Queries))
	}
	// q1 sanity
	q1 := fx.Queries[0]
	if q1.ID != "q1" || q1.Expected.Symbol != "Server.Listen" {
		t.Errorf("q1 not as expected: %+v", q1)
	}
}

func TestLoadFixtureRejectsBadInputs(t *testing.T) {
	bad := []string{
		"schema_version: \"\"\nqueries: []",
		"schema_version: \"99\"\nqueries: []",
		"schema_version: \"1\"\nqueries:\n  - {id: '', intent: x, expected: {file: f, line_range: [1, 1]}}",
		"schema_version: \"1\"\nqueries:\n  - {id: q1, intent: x, expected: {file: f, line_range: [5, 3]}}",
	}
	for i, body := range bad {
		path := filepath.Join(t.TempDir(), "bad.yaml")
		if err := writeFile(t, path, body); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFixture(path); err == nil {
			t.Errorf("case %d: expected error for %q", i, body)
		}
	}
}

// TestLoadFixtureAllowsPendingWithoutLineRange verifies that the loader
// accepts entries flagged `pending: true` even when line_range is
// missing or zero — those entries target corpora (docs, PR/commit) that
// are not yet indexed but should round-trip from YAML for future use.
func TestLoadFixtureAllowsPendingWithoutLineRange(t *testing.T) {
	body := `schema_version: "1"
queries:
  - id: wq1
    intent: "why was X chosen"
    pending: true
    expected:
      file: docs/plan.md
      expected_kind: doc_section
      line_range: [0, 0]
`
	path := filepath.Join(t.TempDir(), "why.yaml")
	if err := writeFile(t, path, body); err != nil {
		t.Fatal(err)
	}
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(fx.Queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(fx.Queries))
	}
	q := fx.Queries[0]
	if !q.Pending {
		t.Errorf("expected Pending=true, got %+v", q)
	}
	if q.Expected.ExpectedKind != "doc_section" {
		t.Errorf("expected ExpectedKind=doc_section, got %q", q.Expected.ExpectedKind)
	}
	// Non-pending entry with the same zero line_range must still fail —
	// loader should only relax for pending=true.
	bad := `schema_version: "1"
queries:
  - id: q1
    intent: "x"
    expected:
      file: f
      line_range: [0, 0]
`
	badPath := filepath.Join(t.TempDir(), "bad.yaml")
	if err := writeFile(t, badPath, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFixture(badPath); err == nil {
		t.Error("expected non-pending entry with zero line_range to be rejected")
	}
}

// TestLoadWhyQueriesFixture covers the on-disk testdata/why-queries.yaml
// scaffold: parses, has the expected count, and every entry is pending
// (until docs / PR / commit corpora are indexed in later phases).
func TestLoadWhyQueriesFixture(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "why-queries.yaml"))
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(fx.Queries) < 10 {
		t.Fatalf("expected ≥10 why-queries, got %d", len(fx.Queries))
	}
	for _, q := range fx.Queries {
		if !q.Pending {
			t.Errorf("query %q: expected Pending=true (no corpus indexed yet) — saw Pending=false", q.ID)
		}
		if q.Expected.ExpectedKind == "" {
			t.Errorf("query %q: missing expected_kind", q.ID)
		}
	}
}

func TestRunComputesMetricsAgainstSample(t *testing.T) {
	eng, _ := newSampleEngine(t)
	path, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "queries.yaml"))
	fx, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	// Use threshold=-1 so the mock embedder's modest scores aren't
	// pre-filtered — eval should reflect retrieval quality, not the
	// CLI's user-facing threshold.
	res, err := Run(context.Background(), eng, fx, Options{K: 5, Threshold: -1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Aggregate.Total != len(fx.Queries) {
		t.Errorf("Total mismatch: got %d, want %d", res.Aggregate.Total, len(fx.Queries))
	}
	// Mock embedder is weak; we expect SOME queries to find their
	// answer in top-5 (specifically the file_header chunk for the
	// matching file should rank well via shared tokens).
	if res.Aggregate.Found == 0 {
		t.Errorf("expected ≥1 query to match in top-5; got %+v", res.Aggregate)
	}
	if res.Aggregate.MRR < 0 || res.Aggregate.MRR > 1 {
		t.Errorf("MRR out of [0,1]: %f", res.Aggregate.MRR)
	}
	if res.Aggregate.CitationAccuracy < 0 || res.Aggregate.CitationAccuracy > 1 {
		t.Errorf("CitationAccuracy out of [0,1]: %f", res.Aggregate.CitationAccuracy)
	}
}

func TestScoreReportsRankAndCitation(t *testing.T) {
	q := Query{
		ID:     "test",
		Intent: "x",
		Expected: Expected{
			File:      "server.go",
			LineRange: [2]int{22, 29},
		},
	}
	resp := &query.Response{
		Hits: []query.Hit{
			{Citation: types.Citation{File: "cache.go", StartLine: 1, EndLine: 5}},
			{Citation: types.Citation{File: "server.go", StartLine: 1, EndLine: 40}},
		},
	}
	got := Score(q, resp, 5)
	if got.FoundRank != 2 {
		t.Errorf("FoundRank = %d, want 2", got.FoundRank)
	}
	if !got.CitationCorrect {
		t.Error("expected CitationCorrect=true (server.go:1-40 overlaps 22-29)")
	}
	if got.ReciprocalRank < 0.49 || got.ReciprocalRank > 0.51 {
		t.Errorf("ReciprocalRank = %f, want ~0.5", got.ReciprocalRank)
	}
}

// writeFile is a tiny helper that mirrors os.WriteFile but returns err
// so tests can assert error-path setup didn't fail.
func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o644)
}
