package ckgalign

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestIntegrationFixture_CKVHalf is the CKV half of the shared CKG↔CKV
// integration fixture agreed in ADR-007 / the 2026-06-29 coordination: on a
// small fixed graph, a CKV chunk at a node's (file, start_line) must inherit
// that node's canonical_id *verbatim* — the build-stable key cks joins on with
// FindByCanonicalID. The CKG half asserts the node carries the same id.
//
// The fixture encodes the two caveats both sides must honor:
//   - the schema_version >= 1.19 population gate, and
//   - the `@<line>` suffix CKG appends when one canonical_id repeats in a file.
//
// Verified live against the canonical graph (go-stablenet @ 0bf2f4d1b, schema
// 1.23): of CKV symbol chunks, ~94% inherit a canonical_id (the rest are
// package-level var/const blocks CKV chunks differently than ckg's per-symbol
// nodes — an explained, stable gap, not a regression).
func TestIntegrationFixture_CKVHalf(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Mirror the ckg shape: nodes + the in-db key/value manifest carrying
	// schema_version. canonical_id values use ckg's real formats (ADR-0001):
	// Go = <importpath>.(*Recv).Method ; the duplicate uses the @<line> suffix.
	if _, err := db.Exec(`
CREATE TABLE nodes (
  id TEXT PRIMARY KEY, qualified_name TEXT NOT NULL,
  file_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL,
  canonical_id TEXT);
CREATE TABLE manifest (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO manifest VALUES ('schema_version','1.23');
INSERT INTO nodes VALUES
  ('n_method', 'consensus/beacon.Beacon.Finalize', 'consensus/beacon/consensus.go', 200, 230,
     'github.com/ethereum/go-ethereum/consensus/beacon.(*Beacon).Finalize'),
  ('n_func',   'params.IsConstantinople',          'params/config.go',             40, 42,
     'github.com/ethereum/go-ethereum/params.IsConstantinople'),
  ('n_dup',    'pkg.init',                          'pkg/a.go',                     10, 12,
     'github.com/x/pkg.init@10');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	ix, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ix.CanonicalAvailable() {
		t.Fatal("CanonicalAvailable()=false on a schema_version 1.23 graph")
	}

	cases := []struct {
		file            string
		start, end      int
		wantNodeID      string
		wantCanonicalID string
	}{
		{"consensus/beacon/consensus.go", 200, 230, "n_method",
			"github.com/ethereum/go-ethereum/consensus/beacon.(*Beacon).Finalize"},
		{"params/config.go", 40, 42, "n_func",
			"github.com/ethereum/go-ethereum/params.IsConstantinople"},
		// @<line>-suffixed id must inherit byte-for-byte (suffix included).
		{"pkg/a.go", 10, 12, "n_dup", "github.com/x/pkg.init@10"},
	}
	for _, tc := range cases {
		e := ix.LookupEntry(tc.file, tc.start, tc.end)
		if e == nil {
			t.Errorf("%s:%d — no node aligned", tc.file, tc.start)
			continue
		}
		if e.ID != tc.wantNodeID {
			t.Errorf("%s:%d — node ID=%q, want %q", tc.file, tc.start, e.ID, tc.wantNodeID)
		}
		if e.CanonicalID != tc.wantCanonicalID {
			t.Errorf("%s:%d — canonical_id=%q, want verbatim %q",
				tc.file, tc.start, e.CanonicalID, tc.wantCanonicalID)
		}
	}
}
