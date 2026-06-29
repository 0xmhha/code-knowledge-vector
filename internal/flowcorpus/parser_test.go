package flowcorpus

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestLoad_MiniCorpus_CountsAndSkips(t *testing.T) {
	chunks, st, err := Load(filepath.Join("testdata", "mini-corpus.jsonl"), "docs/corpus/corpus.jsonl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 1 flow + 1 step + 1 invariant chunked; 1 edge counted; 1 bad step +
	// 1 unknown type + 1 id-less flow skipped.
	if st.Flows != 1 || st.Steps != 1 || st.Invariants != 1 {
		t.Errorf("counts flow/step/inv = %d/%d/%d, want 1/1/1", st.Flows, st.Steps, st.Invariants)
	}
	if st.Edges != 1 {
		t.Errorf("Edges=%d, want 1 (counted, not chunked)", st.Edges)
	}
	if st.Skipped != 3 {
		t.Errorf("Skipped=%d, want 3 (bad step + unknown type + id-less flow)", st.Skipped)
	}
	if len(chunks) != 3 {
		t.Fatalf("len(chunks)=%d, want 3", len(chunks))
	}
	if len(st.Warnings) != 3 {
		t.Errorf("Warnings=%d, want 3: %v", len(st.Warnings), st.Warnings)
	}
}

func TestLoad_StepChunk_EmbedTextAndCitation(t *testing.T) {
	chunks, _, err := Load(filepath.Join("testdata", "mini-corpus.jsonl"), "docs/corpus/corpus.jsonl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var step *types.Chunk
	for i := range chunks {
		if chunks[i].ChunkKind == types.ChunkFlowStep {
			step = &chunks[i]
		}
	}
	if step == nil {
		t.Fatal("no flow_step chunk produced")
	}
	// Citation = real code file:line (resolvable under srcRoot at query time).
	if step.File != "cmd/gstable/chaincmd.go" || step.StartLine != 191 || step.EndLine != 191 {
		t.Errorf("citation = %s:%d-%d, want cmd/gstable/chaincmd.go:191-191", step.File, step.StartLine, step.EndLine)
	}
	// Embed text must carry prose + symbol + the branch "when" (so a symptom
	// phrased as the failure condition still retrieves this step).
	for _, want := range []string{"core.Genesis로 디코드", "main.initGenesis", "경로 인자 없음/파일 열기 실패"} {
		if !strings.Contains(step.Text, want) {
			t.Errorf("embed text missing %q: %q", want, step.Text)
		}
	}
	if step.FlowStep == nil || step.FlowStep.FlowID != "ep-cli-init" || step.FlowStep.StepID != "init-01" {
		t.Errorf("FlowStep meta = %+v", step.FlowStep)
	}
	if len(step.FlowStep.Branches) != 1 || step.FlowStep.Branches[0].At != "chaincmd.go:199" {
		t.Errorf("Branches = %+v", step.FlowStep.Branches)
	}
}

func TestLoad_FlowAndInvariant_FilelessCiteCorpus(t *testing.T) {
	chunks, _, err := Load(filepath.Join("testdata", "mini-corpus.jsonl"), "docs/corpus/corpus.jsonl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, c := range chunks {
		switch c.ChunkKind {
		case types.ChunkFlowSpine:
			if c.File != "docs/corpus/corpus.jsonl" {
				t.Errorf("flow spine cite = %q, want corpus path", c.File)
			}
			if c.FlowSpine == nil || c.FlowSpine.FlowID != "ep-cli-init" {
				t.Errorf("FlowSpine = %+v", c.FlowSpine)
			}
		case types.ChunkInvariant:
			if c.Provenance != "curated" {
				t.Errorf("invariant Provenance=%q, want curated", c.Provenance)
			}
			if len(c.EnforcedAt) != 1 || c.EnforcedAt[0].Loc != "commit.go:123" {
				t.Errorf("EnforcedAt=%+v", c.EnforcedAt)
			}
			if !strings.Contains(c.Text, "QuorumSize") {
				t.Errorf("invariant embed text missing statement: %q", c.Text)
			}
		}
	}
}

func TestLoad_ChunkIDsUnique(t *testing.T) {
	chunks, _, err := Load(filepath.Join("testdata", "mini-corpus.jsonl"), "docs/corpus/corpus.jsonl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range chunks {
		if c.ID == "" {
			t.Errorf("empty chunk ID for %s", c.ChunkKind)
		}
		if seen[c.ID] {
			t.Errorf("duplicate chunk ID %s", c.ID)
		}
		seen[c.ID] = true
	}
}
