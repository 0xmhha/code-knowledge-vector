package query

import (
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// EnforceCitations verifies every hit's citation against the on-disk
// source tree at srcRoot, returning the surviving hits and the count of
// dropped ones. Plan §5 + §7.4: citation accuracy must be 100%, so any
// hit we can't verify is silently dropped (with a warning aggregated by
// the caller).
//
// W2 verification is **cheap**:
//   1. file exists at srcRoot/<rel>
//   2. start_line ≥ 1 and end_line ≥ start_line
//
// Commit-hash verification via `git cat-file` is deferred to W4 — it
// requires shelling out per hit and would dominate query latency.
//
// When srcRoot is empty (no source tree available; we're running
// against a moved index), every hit is allowed through with a warning.
// Callers can detect that case via the dropped count being 0 alongside
// missing files.
func EnforceCitations(hits []types.Hit, srcRoot string) (keep []types.Hit, dropped int) {
	if srcRoot == "" {
		// No source tree to verify against — pass through.
		return hits, 0
	}
	keep = hits[:0] // reuse underlying array (caller doesn't reuse hits)
	for _, h := range hits {
		if verifyCitation(srcRoot, h.Chunk) {
			keep = append(keep, h)
		} else {
			dropped++
		}
	}
	return keep, dropped
}

// verifyCitation runs the cheap existence + line-sanity check. Returns
// true if the citation is plausible; false if anything is missing.
func verifyCitation(srcRoot string, c types.Chunk) bool {
	if c.File == "" {
		return false
	}
	if c.StartLine < 1 || c.EndLine < c.StartLine {
		return false
	}
	full := filepath.Join(srcRoot, c.File)
	info, err := os.Stat(full)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return true
}
