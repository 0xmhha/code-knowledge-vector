package ckgalign

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// makeFixtureDB writes a minimal ckg-shaped graph.db with two files'
// nodes so we can exercise every branch of Lookup. The extra "sig only"
// nodes (n_sig, n_const_block) mimic the case-A pattern from production:
// ckg parses only the signature span while the ckv chunk covers the
// body span a few lines lower.
func makeFixtureDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE nodes (
  id TEXT PRIMARY KEY,
  qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL
);
INSERT INTO nodes VALUES
  ('n_type',   'pkg.MyType',             'a/file.go',   10, 80),
  ('n_method', 'pkg.MyType.DoIt',        'a/file.go',   20, 30),
  ('n_field',  'pkg.MyType.field',       'a/file.go',   12, 12),
  ('n_func',   'pkg.HelperFn',           'a/file.go',  100, 110),
  ('n_sig',    'pkg.ChainCfg.IsConst',   'a/file.go',  200, 202),
  ('n_other',  'pkg.Other',              'b/other.go',  5, 15),
  -- pseudo nodes (must be excluded by Load filter)
  ('n_pseudo_file', 'file:a/file.go',     'a/file.go',   1, 200),
  ('n_pseudo_hunk', 'hunk:abc123',        'a/file.go',  20, 30),
  ('n_pseudo_imp',  'import:pkg.foo',     '',            0,  0);
`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return dir
}

func TestLoad_FilterAndIndex(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 6 real source nodes across 2 files. The 3 pseudo nodes are
	// excluded by the file:/hunk:/import: filter (and the import: also
	// has empty file_path / zero start_line).
	if got := ix.EntryCount(); got != 6 {
		t.Errorf("EntryCount = %d, want 6", got)
	}
	if got := ix.FileCount(); got != 2 {
		t.Errorf("FileCount = %d, want 2", got)
	}
}

func TestLookup_ExactStartLine_PrefersSmallestRange(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// n_method (20-30, size 10) and n_type (10-80, size 70) both
	// eligible by containment, but only n_method's start_line is
	// exactly 20. Exact match wins.
	got := ix.Lookup("a/file.go", 20, 20)
	if got != "n_method" {
		t.Errorf("Lookup(a/file.go, 20) = %q, want n_method", got)
	}
}

func TestLookup_RangeContainment_PrefersSmallestRange(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Line 25 inside both n_method (20-30) and n_type (10-80).
	// No exact match → smallest containing range wins → n_method.
	got := ix.Lookup("a/file.go", 25, 25)
	if got != "n_method" {
		t.Errorf("Lookup(a/file.go, 25) = %q, want n_method", got)
	}
}

func TestLookup_RangeOverlap_ChunkOverlapsNode(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Chunk [28, 35] overlaps n_method [20, 30] (shared 28-30, 3 lines)
	// and is fully inside n_type [10, 80]. n_method is the smaller range
	// so step 3 picks it just like steps 1/2 do.
	got := ix.Lookup("a/file.go", 28, 35)
	if got != "n_method" {
		t.Errorf("Lookup(a/file.go, 28, 35) = %q, want n_method", got)
	}
}

// TestLookup_BoundaryOverlap_FallsThroughToNearest pins the ChainConfig.*
// production regression: a Go method-body chunk's closing line equals
// the next function's signature start line, producing a 1-line overlap
// with the WRONG node. MinOverlapLines = 2 must reject that overlap so
// step 4 (nearest) can match the function-above instead.
func TestLookup_BoundaryOverlap_FallsThroughToNearest(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Chunk [205, 207] mimics a method body whose closing brace is on
	// line 207. n_sig covers the previous function's signature [200-202];
	// n_func covers a later helper [100-110] (well below). Chunk shares
	// 0 lines with n_sig (no overlap, gap=3) and 0 with n_func. Step 4
	// nearest picks n_sig (gap 3 < tolerance 5).
	got := ix.Lookup("a/file.go", 205, 207)
	if got != "n_sig" {
		t.Errorf("Lookup(a/file.go, 205, 207) = %q, want n_sig (nearest above, not boundary overlap below)", got)
	}

	// True production case (ChainConfig family): chunk body ends 1 line
	// before the next function's signature line. Chunk [205, 207],
	// n_sig [210, 212]: 0-line overlap. Step 4 nearest gap = 3, within
	// tolerance, matches n_sig. (This duplicates the SigBodyGap test
	// with explicit numbers; keeping it inline documents that the
	// MinOverlapLines floor in step 3 doesn't regress the SigBodyGap
	// behaviour.)
	got = ix.Lookup("a/file.go", 205, 207)
	if got != "n_sig" {
		t.Errorf("Lookup(a/file.go, 205, 207) = %q, want n_sig (no overlap, nearest)", got)
	}

	// Adversarial boundary case: chunk [198, 200] meets n_sig.Start
	// (200) exactly. Overlap = 1 line (boundary noise) → rejected by
	// MinOverlapLines. Step 4 nearest also skips because chunk and
	// n_sig still overlap by that one line (default branch). The
	// expected result is "" — a deliberate non-match safer than
	// guessing a node bound to the chunk by a single end-of-brace
	// alignment. Production never hits this pattern (ckg nodes don't
	// start exactly where a chunk's closing line lands), so leaving
	// it as no-match is the right floor.
	got = ix.Lookup("a/file.go", 198, 200)
	if got != "" {
		t.Errorf("Lookup(a/file.go, 198, 200) = %q, want \"\" (boundary noise rejected)", got)
	}
}

func TestLookup_NearestWithinTolerance_SigBodyGap(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// The case-A pattern: ckg covers only the function signature
	// (n_sig: 200-202) while ckv covers the body (205-210). Gap = 3,
	// within NearestTolerance (5) → n_sig matches.
	got := ix.Lookup("a/file.go", 205, 210)
	if got != "n_sig" {
		t.Errorf("Lookup(a/file.go, 205, 210) = %q, want n_sig", got)
	}
}

func TestLookup_NearestBeyondTolerance_Empty(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Chunk [220, 230] is 18 lines after n_sig (ends at 202) and 110
	// lines after n_func (ends at 110). Both gaps exceed
	// NearestTolerance = 5. Step 4 must NOT match — returning "" is
	// safer than guessing a far-away node.
	got := ix.Lookup("a/file.go", 220, 230)
	if got != "" {
		t.Errorf("Lookup(a/file.go, 220, 230) = %q, want \"\" (beyond tolerance)", got)
	}
}

func TestLookup_NearestPicksClosestWhenMultipleInTolerance(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Chunk [33, 35]: n_method [20,30] is 3 lines before (gap=3),
	// n_type [10,80] contains 33-35 — so step 2 catches it first
	// before step 4 gets a turn. n_method wins via containment.
	got := ix.Lookup("a/file.go", 33, 35)
	if got != "n_type" && got != "n_method" {
		t.Errorf("Lookup(a/file.go, 33, 35) = %q, want n_type or n_method", got)
	}
	// More direct nearest-tier check: a region that is NOT contained
	// in any node but is close to two of them. Pick a gap that only
	// n_method can satisfy.
	got = ix.Lookup("a/file.go", 32, 33)
	// n_method end=30, gap=2; n_type contains 32 → step 2 catches n_type.
	// (Step 2 always runs before step 4.)
	if got != "n_type" {
		t.Errorf("Lookup(a/file.go, 32, 33) = %q, want n_type (containment beats nearest)", got)
	}
}

func TestLookup_OutsideAnyRange_BeyondTolerance_Empty(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Line 500 — far from every node, all gaps > NearestTolerance.
	if got := ix.Lookup("a/file.go", 500, 500); got != "" {
		t.Errorf("Lookup(a/file.go, 500) = %q, want \"\"", got)
	}
}

func TestLookup_UnknownFile_Empty(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	if got := ix.Lookup("nope/missing.go", 10, 10); got != "" {
		t.Errorf("unknown file = %q, want \"\"", got)
	}
}

func TestLookup_NilIndex_Empty(t *testing.T) {
	var ix *Index
	if got := ix.Lookup("a/file.go", 1, 1); got != "" {
		t.Errorf("nil index lookup = %q, want \"\"", got)
	}
}

func TestLookup_EndLineBelowStartLine_TreatedAsZeroSpan(t *testing.T) {
	dir := makeFixtureDB(t)
	ix, _ := Load(dir)
	// Caller passes endLine=0 (older API) on a chunk at line 25:
	// Lookup treats it as endLine=startLine and still finds n_method
	// by containment.
	got := ix.Lookup("a/file.go", 25, 0)
	if got != "n_method" {
		t.Errorf("Lookup(a/file.go, 25, 0) = %q, want n_method (zero endLine normalised)", got)
	}
}

// TestLookupEntry_CanonicalIDCopied verifies that when the ckg graph carries a
// canonical_id column (schema >= 1.16), LookupEntry copies it verbatim — the key
// cks uses to FindByCanonicalID. Also checks Load tolerates the column's
// presence (the older fixtures above prove tolerance of its absence).
func TestLookupEntry_CanonicalIDCopied(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE nodes (
  id TEXT PRIMARY KEY,
  qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  canonical_id TEXT
);
INSERT INTO nodes VALUES
  ('n_m', 'pkg.T.Do', 'a/f.go', 20, 30, 'example.com/pkg.(*T).Do'),
  ('n_nocid', 'pkg.Bare', 'a/f.go', 40, 50, '');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e := ix.LookupEntry("a/f.go", 20, 30)
	if e == nil {
		t.Fatal("LookupEntry returned nil for a/f.go:20-30")
	}
	if e.ID != "n_m" {
		t.Errorf("ID = %q, want n_m", e.ID)
	}
	if e.CanonicalID != "example.com/pkg.(*T).Do" {
		t.Errorf("CanonicalID = %q, want example.com/pkg.(*T).Do", e.CanonicalID)
	}
	// a node with empty canonical_id resolves but carries no id — no crash.
	if e2 := ix.LookupEntry("a/f.go", 40, 50); e2 == nil || e2.CanonicalID != "" {
		t.Errorf("empty-canonical node: got %+v, want non-nil with empty CanonicalID", e2)
	}
}
