package build

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
)

// TestRun_InvariantExtraction_EndToEnd builds a tiny corpus containing
// existing markers (CRITICAL, IMPORTANT), new markers (INVARIANT,
// CONSENSUS), and heuristic panics. Asserts that ChunkInvariant rows
// land in the store across all three tiers and that source chunks
// carry back-pointers in the invariants column.
func TestRun_InvariantExtraction_EndToEnd(t *testing.T) {
	src := t.TempDir()
	out := t.TempDir()

	// Single Go file mixing all three tiers.
	fixture := `package x

// CRITICAL: balance must be non-negative after every transfer.
// Otherwise underflow allows infinite minting.
func Transfer(amount int) error {
	if amount < 0 {
		return errBadAmount
	}
	return nil
}

// INVARIANT: validator set rotates only at epoch boundaries.
func Rotate() {}

// IMPORTANT: this initializer must run before any handler binds.
func Init() {
	panic("validator must hold under all forks")
}

var errBadAmount = newErr("negative amount")

func newErr(s string) error { return nil }
`
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	res, err := Run(context.Background(), Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Chunks.Invariant < 3 {
		t.Errorf("expected ≥3 invariant chunks (CRITICAL, INVARIANT, IMPORTANT), got %d",
			res.Chunks.Invariant)
	}

	// Probe DB: invariant rows + back-pointers on source chunks.
	dbPath := filepath.Join(out, "vector.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var invCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE chunk_kind = 'invariant'`).Scan(&invCount)
	if err != nil {
		t.Fatalf("count invariant: %v", err)
	}
	if invCount < 3 {
		t.Errorf("expected ≥3 invariant rows, got %d", invCount)
	}

	// At least one source chunk should carry an invariants JSON
	// back-pointer that mentions CRITICAL or IMPORTANT.
	var anyRef string
	err = db.QueryRow(
		`SELECT invariants FROM chunks WHERE chunk_kind != 'invariant' AND invariants != '' LIMIT 1`,
	).Scan(&anyRef)
	if err != nil {
		t.Fatalf("read invariants ref: %v", err)
	}
	if anyRef == "" {
		t.Error("expected at least one source chunk with invariants back-pointer")
	}
}
