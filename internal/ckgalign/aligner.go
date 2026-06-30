// Package ckgalign builds an in-memory index from a CKG SQLite store
// (graph.db) and resolves each CKV chunk's CKGNodeID by matching
// (file_path, start_line) — exact start-line preferred, then smallest
// containing line range.
//
// Used by `ckv build --ckg <dir>` to populate chunks.ckg_node_id, the
// 1:1 alignment that cks composer relies on to disambiguate same-named
// symbols across packages (e.g. eight different `Finalize` methods).
//
// One-shot use: Load() reads every alignment-candidate node row into
// RAM (~25 MB for a 256k-node graph) and closes the DB handle before
// return. Lookup() is O(N_per_file) — fine for the chunk-emit loop
// because per-file slice size is small (avg ~tens of symbols).
package ckgalign

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3" // CGO driver — already pulled in by sqlite-vec
)

// Entry is one ckg node row keyed by (start_line, end_line).
type Entry struct {
	ID        string
	StartLine int
	EndLine   int
	// CanonicalID is ckg's globally-unique, import-path-qualified symbol id
	// (ADR-0001), copied verbatim so a CKV chunk inherits the exact key ckg's
	// FindByCanonicalID resolves on. Empty when the ckg graph predates
	// canonical_id (schema < 1.16) or for symbols ckg leaves unqualified.
	CanonicalID string
}

// Index holds every alignment-candidate ckg node grouped by file_path,
// each file's slice sorted ascending by start_line.
type Index struct {
	byFile map[string][]Entry
	// canonicalAvailable is true when the source ckg graph actually populates
	// canonical_id (a schema >= 1.19 cache). The column exists from schema 1.16
	// but carries values only from 1.19, so column-presence alone is not enough:
	// a 1.16–1.18 graph would silently yield empty join keys. See ADR-007.
	canonicalAvailable bool
}

// Load opens <ckgPath>/graph.db read-only and indexes alignment-eligible
// node rows. Returns a populated *Index ready for Lookup. The DB handle
// is closed before return.
//
// "Alignment-eligible" excludes pseudo nodes whose file_path does not
// describe a normal source span — `file:`/`hunk:`/`import:` prefixes
// in ckg's qualified_name space, and rows with empty file_path or
// non-positive start_line. Everything else (Function, Method, Type,
// Constant, Variable, Interface, Struct, Field, etc.) is a candidate.
func Load(ckgPath string) (*Index, error) {
	dbPath := filepath.Join(ckgPath, "graph.db")
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("ckgalign: open %s: %w", dbPath, err)
	}
	defer db.Close()

	// canonical_id exists only on ckg schema >= 1.16. Probe for it so this
	// loader still works against older graphs (the column is selected only
	// when present; otherwise Entry.CanonicalID stays ""). COALESCE handles a
	// NULL value, but referencing a missing column is a hard SQL error, hence
	// the probe rather than a blind SELECT.
	//
	// Column presence is necessary but NOT sufficient: ckg added the column at
	// schema 1.16 but only populates it from cache SchemaVersion >= 1.19, so a
	// 1.16–1.18 graph has the column entirely empty. Joining on those empty
	// values fails silently (ADR-007), so we additionally probe for at least one
	// populated value and expose the result via CanonicalAvailable() — the
	// build can then surface "canonical_id unavailable" instead of inheriting
	// empty keys as if the alignment were complete.
	hasCanonical := columnExists(db, "nodes", "canonical_id")
	canonicalSel := "''"
	canonicalAvailable := false
	if hasCanonical {
		canonicalSel = "COALESCE(canonical_id,'')"
		// Agreed gate (ADR-007 / coordination D-2): canonical_id is only a
		// trustworthy join key on a ckg cache at manifest schema_version
		// >= 1.19 (the column exists from 1.16 but is populated only from
		// 1.19). Prefer the recorded schema_version from the in-db manifest
		// table; fall back to a population probe for older graphs that predate
		// that manifest table.
		if maj, min, ok := readManifestSchemaVersion(db); ok {
			canonicalAvailable = maj > 1 || (maj == 1 && min >= 19)
		} else {
			canonicalAvailable = canonicalHasValue(db)
		}
	}
	q := `
SELECT id, file_path, start_line, end_line, ` + canonicalSel + `
FROM nodes
WHERE file_path != ''
  AND start_line > 0
  AND qualified_name NOT LIKE 'file:%'
  AND qualified_name NOT LIKE 'hunk:%'
  AND qualified_name NOT LIKE 'import:%'`
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("ckgalign: query nodes: %w", err)
	}
	defer rows.Close()

	ix := &Index{byFile: make(map[string][]Entry), canonicalAvailable: canonicalAvailable}
	var id, file, canonicalID string
	var start, end int
	for rows.Next() {
		if err := rows.Scan(&id, &file, &start, &end, &canonicalID); err != nil {
			return nil, fmt.Errorf("ckgalign: scan: %w", err)
		}
		ix.byFile[file] = append(ix.byFile[file], Entry{ID: id, StartLine: start, EndLine: end, CanonicalID: canonicalID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ckgalign: rows: %w", err)
	}
	for f := range ix.byFile {
		es := ix.byFile[f]
		sort.Slice(es, func(i, j int) bool { return es[i].StartLine < es[j].StartLine })
	}
	return ix, nil
}

// columnExists reports whether table has a column named col, via
// PRAGMA table_info. Used to stay compatible with older ckg graphs that
// predate a column (e.g. canonical_id, schema < 1.16). Any probe error is
// treated as "absent" so the caller falls back to the safe query.
func columnExists(db *sql.DB, table, col string) bool {
	rows, err := db.Query(`SELECT 1 FROM pragma_table_info(?) WHERE name = ?`, table, col)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

// readManifestSchemaVersion reads ckg's recorded cache schema version from the
// in-db key/value `manifest` table (row key='schema_version', e.g. "1.23") and
// returns it as (major, minor). ok is false when the table or row is absent
// (older graphs) or the value doesn't parse — the caller then falls back to a
// population probe. Compared as integer major/minor (not float) so 1.9 < 1.19.
func readManifestSchemaVersion(db *sql.DB) (major, minor int, ok bool) {
	var v string
	if err := db.QueryRow(`SELECT value FROM manifest WHERE key = 'schema_version'`).Scan(&v); err != nil {
		return 0, 0, false
	}
	return parseMajorMinor(v)
}

// parseMajorMinor parses "MAJOR" or "MAJOR.MINOR" (ignoring any further
// dot-separated parts) into integers. Returns ok=false on malformed input.
func parseMajorMinor(s string) (major, minor int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(s, ".", 3)
	maj, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	min := 0
	if len(parts) > 1 {
		if min, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, false
		}
	}
	return maj, min, true
}

// canonicalHasValue reports whether the nodes table has at least one non-empty
// canonical_id. A graph whose column exists but is entirely empty is a pre-1.19
// ckg cache (the column landed at schema 1.16 but is populated only from 1.19):
// its canonical_id is not a usable join key. Any probe error is treated as
// "no value" so the caller degrades to canonical-id-unavailable. Caller must
// have confirmed the column exists first (this references it directly).
func canonicalHasValue(db *sql.DB) bool {
	rows, err := db.Query(`SELECT 1 FROM nodes WHERE canonical_id IS NOT NULL AND canonical_id != '' LIMIT 1`)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

// NearestTolerance is the maximum line gap between a chunk and a ckg node
// to be considered a candidate for the nearest-match step. 5 lines absorbs
// the common Go pattern where the ckg node covers only the function
// signature (e.g. `params.ChainConfig.IsConstantinople@:1017-1019`) while
// the ckv chunk covers the function body (`@:1020-1022`) — a 3-line gap
// no overlap/containment can catch. Larger tolerances start to introduce
// false positives for densely-packed const/var declarations.
const NearestTolerance = 5

// MinOverlapLines is the minimum number of lines a chunk range and a ckg
// node range must share for step-3 (range overlap) to claim a match. A
// single shared line is almost always a boundary artifact — the chunk's
// closing `}` lying on the same line as the next function's opening
// brace. Requiring >= 2 shared lines avoids the Go method-body family
// mismatch (IsHomestead chunk binding to IsDAOFork node, etc.) while
// preserving the substantive-overlap case the step was designed for.
const MinOverlapLines = 2

// minInt / maxInt are small helpers so the file builds on Go versions
// older than 1.21 where min/max builtins were introduced.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Lookup returns the ckg node id that best matches (filePath, startLine,
// endLine) by trying four progressively looser strategies in order:
//
//  1. exact start_line match (smallest range tiebreak),
//  2. node whose [start_line, end_line] contains chunk startLine
//     (smallest range tiebreak),
//  3. range overlap: chunk [startLine, endLine] and node [s, e] share
//     at least one line (smallest gap tiebreak — same as smallest |s − startLine|
//     when both ranges are valid),
//  4. nearest non-overlapping node within NearestTolerance lines, picking
//     the smallest gap.
//
// endLine == 0 (or < startLine) is treated as endLine = startLine so
// older callers and zero-span chunks still match exactly + range-contain.
//
// filePath must be the same shape ckg stored — src-root-relative.
// The smallest-range tiebreak picks the inner Method/Field node over
// the enclosing Type node when both fire, so chunks emitted at a method
// body line map to the method, not its enclosing type.
// Lookup returns the matched ckg node ID (or "" when nothing matches). It is a
// thin wrapper over LookupEntry, kept for callers that only need the id.
func (ix *Index) Lookup(filePath string, startLine, endLine int) string {
	if e := ix.LookupEntry(filePath, startLine, endLine); e != nil {
		return e.ID
	}
	return ""
}

// LookupEntry returns the full matched ckg node (id + canonical_id) or nil. The
// matching ladder is unchanged from the original Lookup; exposing the Entry lets
// callers copy both the node ID and the canonical_id in one pass.
func (ix *Index) LookupEntry(filePath string, startLine, endLine int) *Entry {
	if ix == nil {
		return nil
	}
	entries := ix.byFile[filePath]
	if len(entries) == 0 {
		return nil
	}
	if endLine < startLine {
		endLine = startLine
	}

	// 1. Exact start_line match — smallest range wins.
	bestExact := -1
	bestSize := -1
	for i, e := range entries {
		if e.StartLine == startLine {
			size := e.EndLine - e.StartLine
			if bestExact == -1 || size < bestSize {
				bestExact = i
				bestSize = size
			}
		}
	}
	if bestExact != -1 {
		return &entries[bestExact]
	}

	// 2. Range containment — node range contains chunk startLine.
	bestContain := -1
	bestSize = -1
	for i, e := range entries {
		if e.StartLine <= startLine && startLine <= e.EndLine {
			size := e.EndLine - e.StartLine
			if bestContain == -1 || size < bestSize {
				bestContain = i
				bestSize = size
			}
		}
	}
	if bestContain != -1 {
		return &entries[bestContain]
	}

	// 3. Range overlap — chunk [startLine, endLine] and node [s, e]
	// share at least MinOverlapLines lines (substantial, not boundary).
	// A single shared line is usually the chunk's closing brace meeting
	// the next function's opening line (Go method-body chunks ending at
	// the `}` of foo followed by `func bar() {`). Without this floor,
	// step 3 silently maps every chunk to the NEXT function — see the
	// ChainConfig.* family in params/config.go where IsHomestead's chunk
	// would otherwise bind to IsDAOFork. Tiebreak: smallest node range.
	bestOverlap := -1
	bestSize = -1
	for i, e := range entries {
		// inclusive overlap span: [max(starts), min(ends)]
		ov := minInt(endLine, e.EndLine) - maxInt(startLine, e.StartLine) + 1
		if ov >= MinOverlapLines {
			size := e.EndLine - e.StartLine
			if bestOverlap == -1 || size < bestSize {
				bestOverlap = i
				bestSize = size
			}
		}
	}
	if bestOverlap != -1 {
		return &entries[bestOverlap]
	}

	// 4. Nearest non-overlapping within NearestTolerance — picks up
	// the Go method-signature vs method-body split (ckg covers
	// `[sig_line, sig_line+2]`, ckv covers `[body_start, body_end]`,
	// 1–3 lines apart). Smallest gap wins.
	bestNear := -1
	bestGap := NearestTolerance + 1
	for i, e := range entries {
		var gap int
		switch {
		case endLine < e.StartLine:
			gap = e.StartLine - endLine
		case e.EndLine < startLine:
			gap = startLine - e.EndLine
		default:
			// overlapping ranges were claimed by step 3 already; skip.
			continue
		}
		if gap < bestGap {
			bestGap = gap
			bestNear = i
		}
	}
	if bestNear != -1 {
		return &entries[bestNear]
	}
	return nil
}

// CanonicalAvailable reports whether the loaded ckg graph actually populates
// canonical_id (a schema >= 1.19 cache). When false, inherited chunk
// canonical_ids are empty and cks FindByCanonicalID joins are unavailable, so
// the build should surface this rather than treat the alignment as complete
// (ADR-007). The wiring/measurement layer additionally asserts the ckg
// build's published manifest schema_version >= 1.19 before pointing at a graph.
func (ix *Index) CanonicalAvailable() bool {
	if ix == nil {
		return false
	}
	return ix.canonicalAvailable
}

// FileCount returns the number of unique files indexed (diagnostic /
// footprint emission).
func (ix *Index) FileCount() int {
	if ix == nil {
		return 0
	}
	return len(ix.byFile)
}

// EntryCount returns the total entry count across all files (diagnostic /
// footprint emission).
func (ix *Index) EntryCount() int {
	if ix == nil {
		return 0
	}
	n := 0
	for _, e := range ix.byFile {
		n += len(e)
	}
	return n
}
