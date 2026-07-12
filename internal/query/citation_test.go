package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestEnforceCitationsDropsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		mkHit("a", "func A(){}", 1, 0.1),                                // file "x.go" — doesn't exist
		{Chunk: types.Chunk{File: "real.go", StartLine: 1, EndLine: 1}}, // exists
		{Chunk: types.Chunk{File: "missing.go", StartLine: 1, EndLine: 1}},
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 {
		t.Errorf("expected 2 dropped, got %d", dropped)
	}
	if len(keep) != 1 || keep[0].Chunk.File != "real.go" {
		t.Errorf("expected only real.go kept, got %+v", keep)
	}
}

func TestEnforceCitationsPassesThroughWhenNoSrcRoot(t *testing.T) {
	hits := []types.Hit{
		mkHit("a", "func A(){}", 1, 0.1),
	}
	keep, dropped := EnforceCitations(hits, "")
	if len(keep) != 1 || dropped != 0 {
		t.Errorf("empty srcRoot must pass through: keep=%d dropped=%d", len(keep), dropped)
	}
}

// TestEnforceCitationsAt_StaleCommitHashFlag exercises B4: when the
// chunk's recorded commit_hash differs from currentHead, the hit
// survives (file is fine) but carries StaleCitation=true and counts
// toward the stale return.
func TestEnforceCitationsAt_StaleCommitHashFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "old-commit"}},
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "new-commit"}},
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: ""}}, // unset, treat as fresh
	}
	keep, dropped, stale := EnforceCitationsAt(hits, dir, "new-commit")
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
	if stale != 1 {
		t.Errorf("expected 1 stale, got %d", stale)
	}
	if len(keep) != 3 {
		t.Errorf("expected all 3 hits to survive, got %d", len(keep))
	}
	if !keep[0].StaleCitation {
		t.Errorf("keep[0] (old-commit) should be marked stale")
	}
	if keep[1].StaleCitation {
		t.Errorf("keep[1] (new-commit) should NOT be stale")
	}
	if keep[2].StaleCitation {
		t.Errorf("keep[2] (empty hash) should NOT be stale (no signal to compare)")
	}
}

// TestEnforceCitationsAt_FlowStepLineDrift is the Phase C2 guard: a flow_step
// whose curated line drifted past the current file's end is flagged stale (not
// dropped), while an in-bounds step is left fresh. Flow chunks carry no commit
// hash, so this line-bounds check is their only staleness signal and runs even
// with an empty currentHead (drift is commit-independent).
func TestEnforceCitationsAt_FlowStepLineDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "svc.go"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flowStep := func(line int) types.Hit {
		return types.Hit{Chunk: types.Chunk{
			File: "svc.go", StartLine: line, EndLine: line,
			ChunkKind: types.ChunkFlowStep, FlowStep: &types.FlowStepMeta{StepID: "s"},
		}}
	}
	hits := []types.Hit{flowStep(2), flowStep(99)} // in-bounds, drifted past EOF

	keep, dropped, stale := EnforceCitationsAt(hits, dir, "") // empty head: only flow drift can fire
	if dropped != 0 {
		t.Fatalf("dropped=%d, want 0 (drift is stale, not dropped)", dropped)
	}
	if stale != 1 {
		t.Fatalf("stale=%d, want 1 (only the drifted step)", stale)
	}
	if len(keep) != 2 {
		t.Fatalf("keep=%d, want 2 (both survive)", len(keep))
	}
	if keep[0].StaleCitation {
		t.Errorf("in-bounds step (line 2 of 3) should not be stale")
	}
	if !keep[1].StaleCitation {
		t.Errorf("drifted step (line 99 of 3) should be stale")
	}
}

// TestEnforceCitationsAt_EmptyCurrentHeadSkipsStaleCheck verifies the
// stale check is opt-in: empty currentHead means "we don't know what
// fresh looks like" → don't mark anything stale.
func TestEnforceCitationsAt_EmptyCurrentHeadSkipsStaleCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1, CommitHash: "any-commit"}},
	}
	_, _, stale := EnforceCitationsAt(hits, dir, "")
	if stale != 0 {
		t.Errorf("empty currentHead must disable stale check, got stale=%d", stale)
	}
}

// TestEnforceCitationsAt_DocsRootResolution verifies that doc/markdown
// chunks whose File is relative to a `--docs` corpus root (outside srcRoot)
// survive citation enforcement when that corpus root is supplied. Without
// it they are dropped — the file does not exist under the code srcRoot —
// which is why domain-corpus chunks never surfaced before this fix.
func TestEnforceCitationsAt_DocsRootResolution(t *testing.T) {
	src := t.TempDir()
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "code.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "flows", "ep.md"), []byte("# flow"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkHits := func() []types.Hit {
		return []types.Hit{
			{Chunk: types.Chunk{File: "code.go", StartLine: 1, EndLine: 1, Language: "go"}},
			{Chunk: types.Chunk{File: "flows/ep.md", StartLine: 1, EndLine: 1, Language: "markdown", ChunkKind: types.ChunkDoc}},
		}
	}

	// Without a docs root the markdown chunk is dropped (file absent under src).
	keepNoDocs, droppedNoDocs, _ := EnforceCitationsAt(mkHits(), src, "")
	if droppedNoDocs != 1 || len(keepNoDocs) != 1 || keepNoDocs[0].Chunk.File != "code.go" {
		t.Fatalf("without docsRoots: want 1 dropped(md)+1 kept(go), got dropped=%d keep=%+v", droppedNoDocs, keepNoDocs)
	}

	// With the docs root supplied, both the code and the doc chunk survive.
	keep, dropped, _ := EnforceCitationsAt(mkHits(), src, "", docs)
	if dropped != 0 || len(keep) != 2 {
		t.Fatalf("with docsRoots: want 0 dropped+2 kept, got dropped=%d keep=%d", dropped, len(keep))
	}
}

func TestEnforceCitationsRejectsInvalidLineRange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []types.Hit{
		{Chunk: types.Chunk{File: "x.go", StartLine: 0, EndLine: 0}}, // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 5, EndLine: 3}}, // bad
		{Chunk: types.Chunk{File: "x.go", StartLine: 1, EndLine: 1}}, // ok
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 || len(keep) != 1 {
		t.Errorf("expected 2 dropped + 1 kept, got dropped=%d keep=%d", dropped, len(keep))
	}
}

// TestEnforceCitations_SyntheticExempt verifies the fix for the "generated
// but never queryable" bug: chunks whose File is a synthetic identifier
// (PR / convention / flow_spine / curated invariant) must survive citation
// enforcement even though no such file exists on disk, while code-location
// chunks with a bogus file are still dropped.
func TestEnforceCitations_SyntheticExempt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	syn := func(kind types.ChunkKind, file string, start int, prov string) types.Hit {
		return types.Hit{Chunk: types.Chunk{
			File: file, StartLine: start, EndLine: start,
			ChunkKind: kind, Provenance: prov,
		}}
	}
	hits := []types.Hit{
		syn(types.ChunkPRBackground, "pr/owner/repo#63", 1, ""),                                       // synthetic → keep
		syn(types.ChunkPRSolution, "pr/owner/repo#63", 2, ""),                                         // synthetic → keep
		syn(types.ChunkCommitMessage, "pr/owner/repo#63", 3, ""),                                      // synthetic → keep
		syn(types.ChunkConvention, "pkg/<convention>", 0, ""),                                         // synthetic (line 0) → keep
		syn(types.ChunkFlowSpine, "corpus.jsonl", 0, ""),                                              // synthetic → keep
		syn(types.ChunkInvariant, "corpus.jsonl", 0, "curated"),                                       // curated → keep
		{Chunk: types.Chunk{File: "real.go", StartLine: 1, EndLine: 1, ChunkKind: types.ChunkSymbol}}, // real → keep
		// regressions: must still drop
		syn(types.ChunkInvariant, "gone.go", 1, "auto"),                                                  // auto-extracted, bogus file → drop
		{Chunk: types.Chunk{File: "missing.go", StartLine: 1, EndLine: 1, ChunkKind: types.ChunkSymbol}}, // bogus code → drop
	}
	keep, dropped := EnforceCitations(hits, dir)
	if dropped != 2 {
		t.Errorf("expected 2 dropped (auto-invariant + missing symbol), got %d", dropped)
	}
	if len(keep) != 7 {
		t.Fatalf("expected 7 kept (6 synthetic/real), got %d: %+v", len(keep), keep)
	}
	// sanity: none of the kept are the two bogus code chunks
	for _, h := range keep {
		if h.Chunk.File == "gone.go" || h.Chunk.File == "missing.go" {
			t.Errorf("bogus code chunk survived: %s", h.Chunk.File)
		}
	}
}
