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

	_ "github.com/mattn/go-sqlite3" // CGO driver — already pulled in by sqlite-vec
)

// Entry is one ckg node row keyed by (start_line, end_line).
type Entry struct {
	ID        string
	StartLine int
	EndLine   int
}

// Index holds every alignment-candidate ckg node grouped by file_path,
// each file's slice sorted ascending by start_line.
type Index struct {
	byFile map[string][]Entry
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

	const q = `
SELECT id, file_path, start_line, end_line
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

	ix := &Index{byFile: make(map[string][]Entry)}
	var id, file string
	var start, end int
	for rows.Next() {
		if err := rows.Scan(&id, &file, &start, &end); err != nil {
			return nil, fmt.Errorf("ckgalign: scan: %w", err)
		}
		ix.byFile[file] = append(ix.byFile[file], Entry{ID: id, StartLine: start, EndLine: end})
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
func (ix *Index) Lookup(filePath string, startLine, endLine int) string {
	if ix == nil {
		return ""
	}
	entries := ix.byFile[filePath]
	if len(entries) == 0 {
		return ""
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
		return entries[bestExact].ID
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
		return entries[bestContain].ID
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
		return entries[bestOverlap].ID
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
		return entries[bestNear].ID
	}
	return ""
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
