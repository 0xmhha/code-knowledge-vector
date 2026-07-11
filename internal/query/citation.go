package query

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// currentGitHead returns `git rev-parse HEAD` for srcRoot, or empty
// when git is unavailable or srcRoot is not a git repo. Used by
// EnforceCitationsAt for stale-citation detection. Failure is
// silent — stale check is best-effort, not a hard requirement.
func currentGitHead(srcRoot string) string {
	if srcRoot == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", srcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// EnforceCitations verifies every hit's citation against the on-disk
// source tree at srcRoot, returning the surviving hits and the count of
// dropped ones. Citation accuracy must be 100%, so any
// hit we can't verify is silently dropped (with a warning aggregated by
// the caller).
//
// Verification:
//
//  1. file exists at srcRoot/<rel>
//  2. start_line ≥ 1 and end_line ≥ start_line
//
// Commit-hash mismatch (stale citation) is NOT a drop — the file
// almost always still has useful content, just at a different
// commit than when it was indexed. Callers see the stale count
// returned alongside dropped and surface a warning. Use
// EnforceCitationsAt to enable the stale check.
//
// When srcRoot is empty (no source tree available; we're running
// against a moved index), every hit is allowed through with a warning.
func EnforceCitations(hits []types.Hit, srcRoot string) (keep []types.Hit, dropped int) {
	keep, dropped, _ = EnforceCitationsAt(hits, srcRoot, "")
	return keep, dropped
}

// EnforceCitationsAt extends EnforceCitations with stale detection:
// when currentHead is non-empty, every surviving hit whose
// Chunk.CommitHash differs gets Hit.StaleCitation=true and counts toward
// the stale return. Useful diagnostic when callers can detect "the
// index is post-reindex-needed but still served." Empty currentHead
// disables the check; stale stays 0.
//
// The surviving slice's order matches the input; only the metadata on
// each hit is touched. Dropped hits are removed as before — line
// validity and file existence remain hard requirements.
// docsRoots are additional roots (manifest.DocsRoots — the `ckv build
// --docs` corpus dirs) used to resolve doc/markdown chunk citations whose
// File is relative to a corpus dir rather than the code srcRoot. A citation
// is valid if its file exists under srcRoot OR any docsRoot; without this,
// every domain-corpus chunk is dropped because it never resolves under the
// code tree.
func EnforceCitationsAt(hits []types.Hit, srcRoot, currentHead string, docsRoots ...string) (keep []types.Hit, dropped int, stale int) {
	if srcRoot == "" {
		// No source tree to verify against — pass through. Stale check
		// also requires srcRoot context, so it's a single no-op.
		return hits, 0, 0
	}
	keep = hits[:0] // reuse underlying array (caller doesn't reuse hits)
	for _, h := range hits {
		if !verifyCitation(srcRoot, h.Chunk, docsRoots) {
			dropped++
			continue
		}
		if currentHead != "" && h.Chunk.CommitHash != "" && h.Chunk.CommitHash != currentHead {
			h.StaleCitation = true
			stale++
		}
		keep = append(keep, h)
	}
	return keep, dropped, stale
}

// hasSyntheticCitation reports whether a chunk's File is a synthetic
// identifier for a non-code artifact rather than a real source path:
//
//   - PR corpus (pr_background / pr_solution / commit_message): File is
//     "pr/<repo>#<n>" — a PR reference, not a file.
//   - convention: File is "<pkg>/<convention>" — a per-package summary.
//   - flow_spine / curated invariant: describe a curated flow / rule, not a
//     code line; they cite the corpus, not the tree.
//
// These chunks carry no on-disk code location, so the file-existence and
// line-sanity checks would drop every one of them from semantic_search
// (the citation is metadata, not a code pointer). Verifying them makes no
// sense, so they are exempt. Code-location chunks (symbol, function_split,
// file_header, doc, flow_step, auto-extracted invariant) are still verified —
// a hallucinated or stale code citation is still caught.
func hasSyntheticCitation(c types.Chunk) bool {
	switch c.ChunkKind {
	case types.ChunkPRBackground, types.ChunkPRSolution, types.ChunkCommitMessage,
		types.ChunkConvention, types.ChunkFlowSpine:
		return true
	case types.ChunkInvariant:
		return c.Provenance == "curated"
	}
	return false
}

// verifyCitation runs the cheap existence + line-sanity check. Returns
// true if the citation is plausible; false if anything is missing. The
// file must exist under srcRoot or under one of docsRoots (the latter
// resolves doc/markdown corpus chunks indexed via `ckv build --docs`,
// whose File is relative to the corpus dir, not the code srcRoot).
// Chunks with a synthetic (non-code) citation are exempt (see
// hasSyntheticCitation) — otherwise every PR / convention / flow-spine hit
// would be silently dropped.
func verifyCitation(srcRoot string, c types.Chunk, docsRoots []string) bool {
	if hasSyntheticCitation(c) {
		return true
	}
	if c.File == "" {
		return false
	}
	if c.StartLine < 1 || c.EndLine < c.StartLine {
		return false
	}
	if fileExistsUnder(srcRoot, c.File) {
		return true
	}
	for _, dr := range docsRoots {
		if dr != "" && fileExistsUnder(dr, c.File) {
			return true
		}
	}
	return false
}

// fileExistsUnder reports whether root/rel exists and is a regular file.
func fileExistsUnder(root, rel string) bool {
	info, err := os.Stat(filepath.Join(root, rel))
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
