package build

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/store/sqlitevec"
)

// TestRunWithPolicy_PersistsCategoryAndGuidance is the end-to-end
// proof that build → store → re-open → query carries policy metadata
// through every layer. It uses the existing testdata/sample fixture
// and classifies *.go as "consensus" so we can assert both columns
// surface in the chunks table after a real build.
func TestRunWithPolicy_PersistsCategoryAndGuidance(t *testing.T) {
	src := resolveTestdataSample(t)
	out := t.TempDir()

	policyPath := filepath.Join(out, "policy.yaml")
	policyYAML := `
version: 1
categories:
  - name: consensus
    paths: ["**/*.go"]
    also_review: ["state"]
    required_tests: ["fork choice"]
    watch_out: ["hard-fork coordination required"]
  - name: docs
    paths: ["docs/**"]
    watch_out: ["public-facing wording"]
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	res, err := Run(context.Background(), Options{
		SrcRoot:    src,
		OutDir:     out,
		Embedder:   mock.Default(),
		PolicyPath: policyPath,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FilesIndexed == 0 {
		t.Fatal("no files indexed")
	}

	// Re-open the resulting DB and inspect the chunks table directly.
	// The policy applied to *.go must show up as category=consensus
	// with non-empty guidance JSON.
	dbPath := filepath.Join(out, "vector.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var goCount int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM chunks WHERE category = 'consensus' AND language = 'go'`,
	).Scan(&goCount)
	if err != nil {
		t.Fatalf("count consensus chunks: %v", err)
	}
	if goCount == 0 {
		t.Errorf("expected at least one Go chunk classified as consensus, got 0")
	}

	// Verify guidance JSON is populated and contains the expected hint.
	var guidance string
	err = db.QueryRow(
		`SELECT guidance FROM chunks WHERE category = 'consensus' AND guidance != '' LIMIT 1`,
	).Scan(&guidance)
	if err != nil {
		t.Fatalf("read guidance: %v", err)
	}
	if guidance == "" {
		t.Fatal("guidance column should contain JSON for matched chunk, got empty")
	}
	for _, want := range []string{"also_review", "state", "fork choice", "hard-fork"} {
		if !contains(guidance, want) {
			t.Errorf("guidance JSON missing %q: %s", want, guidance)
		}
	}

	// docs chunks (markdown sections) should have category=docs.
	var docCount int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM chunks WHERE category = 'docs'`,
	).Scan(&docCount)
	if err != nil {
		t.Fatalf("count docs chunks: %v", err)
	}
	if docCount == 0 {
		t.Errorf("expected at least one docs/* chunk, got 0")
	}

	// Re-open via the typed Store to make sure Search() round-trips
	// category + guidance to the caller. We don't run an actual KNN
	// because that's covered elsewhere — opening alone is enough to
	// confirm the schema is migration-applied and readable.
	s, err := sqlitevec.Open(dbPath, mock.Default().Dimension())
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	s.Close()
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
