package flowcorpus

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func stepChunkAt(file string, line int) types.Chunk {
	return types.Chunk{
		File:      file,
		StartLine: line,
		EndLine:   line,
		ChunkKind: types.ChunkFlowStep,
		FlowStep:  &types.FlowStepMeta{},
	}
}

func TestAlignSteps_TightestContainingSpanWins(t *testing.T) {
	code := CodeIndex{}
	code.Add("a.go", 10, 40, "outer")  // encloses line 25
	code.Add("a.go", 20, 30, "inner")  // tighter, also encloses 25
	code.Add("a.go", 50, 60, "other")  // does not contain 25
	code.Add("b.go", 1, 100, "b-file") // different file

	chunks := []types.Chunk{
		stepChunkAt("a.go", 25),           // → inner (tightest)
		stepChunkAt("a.go", 55),           // → other
		stepChunkAt("a.go", 5),            // no span contains line 5 → unresolved
		stepChunkAt("b.go", 42),           // → b-file
		{ChunkKind: types.ChunkFlowSpine}, // not a step → ignored
	}

	resolved, total := AlignSteps(chunks, code)
	if total != 4 {
		t.Fatalf("total steps = %d, want 4 (spine ignored)", total)
	}
	if resolved != 3 {
		t.Fatalf("resolved = %d, want 3", resolved)
	}
	if got := chunks[0].FlowStep.AlignedChunkID; got != "inner" {
		t.Errorf("step at a.go:25 aligned to %q, want inner (tightest)", got)
	}
	if got := chunks[1].FlowStep.AlignedChunkID; got != "other" {
		t.Errorf("step at a.go:55 aligned to %q, want other", got)
	}
	if got := chunks[2].FlowStep.AlignedChunkID; got != "" {
		t.Errorf("step at a.go:5 should be unaligned, got %q", got)
	}
	if got := chunks[3].FlowStep.AlignedChunkID; got != "b-file" {
		t.Errorf("step at b.go:42 aligned to %q, want b-file", got)
	}
}

func TestAlignSteps_BoundaryLinesInclusive(t *testing.T) {
	code := CodeIndex{}
	code.Add("x.go", 10, 20, "fn")
	chunks := []types.Chunk{stepChunkAt("x.go", 10), stepChunkAt("x.go", 20), stepChunkAt("x.go", 21)}
	resolved, total := AlignSteps(chunks, code)
	if total != 3 || resolved != 2 {
		t.Fatalf("resolved/total = %d/%d, want 2/3 (start and end inclusive, 21 out)", resolved, total)
	}
	if chunks[2].FlowStep.AlignedChunkID != "" {
		t.Errorf("line 21 is past the span end and must not align")
	}
}

func TestCodeIndex_AddRejectsInvalid(t *testing.T) {
	ix := CodeIndex{}
	ix.Add("", 1, 2, "id")     // no file
	ix.Add("f.go", 0, 2, "id") // non-positive start (file header)
	ix.Add("f.go", 1, 2, "")   // no id
	if len(ix) != 0 {
		t.Fatalf("invalid spans should be rejected, got %v", ix)
	}
}
