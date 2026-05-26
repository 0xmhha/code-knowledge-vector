package prdoc

import (
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParse_fullPR(t *testing.T) {
	meta := PRMeta{
		Repo:     "org/repo",
		PRNumber: 42,
		Title:    "fix: resolve timeout on shutdown",
		Body: `## Background

The server hangs on SIGTERM because the listener isn't closed before
the graceful-shutdown timer fires.

## Solution

Close the listener first, then drain in-flight connections.
`,
		CommitMessages: []string{
			"close listener before drain",
			"add integration test for shutdown",
		},
		MergedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	chunks := Parse(meta)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	bg := chunks[0]
	if bg.ChunkKind != types.ChunkPRBackground {
		t.Errorf("chunk[0] kind = %q, want pr_background", bg.ChunkKind)
	}
	if bg.File != "pr/org/repo#42" {
		t.Errorf("chunk[0] file = %q", bg.File)
	}
	if bg.SymbolName != meta.Title {
		t.Errorf("chunk[0] symbol = %q, want %q", bg.SymbolName, meta.Title)
	}
	if bg.Text == "" {
		t.Error("chunk[0] empty text")
	}

	sol := chunks[1]
	if sol.ChunkKind != types.ChunkPRSolution {
		t.Errorf("chunk[1] kind = %q, want pr_solution", sol.ChunkKind)
	}

	cm := chunks[2]
	if cm.ChunkKind != types.ChunkCommitMessage {
		t.Errorf("chunk[2] kind = %q, want commit_message", cm.ChunkKind)
	}
	if cm.Text == "" {
		t.Error("chunk[2] empty text")
	}
}

func TestParse_noSections(t *testing.T) {
	meta := PRMeta{
		Repo:     "org/repo",
		PRNumber: 10,
		Title:    "chore: bump deps",
		Body:     "Just bumping dependencies.",
		CommitMessages: []string{
			"bump go.mod",
		},
		MergedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}

	chunks := Parse(meta)
	// No Background/Solution headers → only commit_message chunk
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (commit_message only)", len(chunks))
	}
	if chunks[0].ChunkKind != types.ChunkCommitMessage {
		t.Errorf("kind = %q, want commit_message", chunks[0].ChunkKind)
	}
}

func TestParse_emptyBody(t *testing.T) {
	meta := PRMeta{
		Repo:     "org/repo",
		PRNumber: 5,
		Title:    "empty",
	}
	chunks := Parse(meta)
	if len(chunks) != 0 {
		t.Errorf("got %d chunks for empty PR, want 0", len(chunks))
	}
}

func TestParse_boldHeaders(t *testing.T) {
	meta := PRMeta{
		Repo:     "org/repo",
		PRNumber: 99,
		Title:    "feat: new feature",
		Body: `**Background**

Something broke.

**Changes**

Fixed it.
`,
		MergedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	chunks := Parse(meta)
	var hasBG, hasSol bool
	for _, c := range chunks {
		switch c.ChunkKind {
		case types.ChunkPRBackground:
			hasBG = true
		case types.ChunkPRSolution:
			hasSol = true
		}
	}
	if !hasBG {
		t.Error("expected pr_background chunk for bold **Background**")
	}
	if !hasSol {
		t.Error("expected pr_solution chunk for bold **Changes**")
	}
}

func TestExtractSection(t *testing.T) {
	body := `## Background

Server hangs on shutdown.

## Solution

Close listener first.

## Test Plan

Added integration test.
`
	bg := extractBackground(body)
	if bg != "Server hangs on shutdown." {
		t.Errorf("background = %q", bg)
	}
	sol := extractSolution(body)
	if sol != "Close listener first." {
		t.Errorf("solution = %q", sol)
	}
}
