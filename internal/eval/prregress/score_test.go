package prregress

import (
	"math"
	"testing"
)

func TestExtractJudgeVerdict_PlainJSON(t *testing.T) {
	out := []byte(`{"score": 0.85, "rationale": "Plan covers both core file changes."}`)
	v, ok := ExtractJudgeVerdict(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if math.Abs(v.Score-0.85) > 1e-9 {
		t.Errorf("score = %g, want 0.85", v.Score)
	}
	if v.Rationale == "" {
		t.Error("rationale lost")
	}
}

func TestExtractJudgeVerdict_FencedJSON(t *testing.T) {
	// Claude sometimes wraps in ``` despite the "no fences" instruction.
	out := []byte("```json\n{\"score\": 0.7, \"rationale\": \"partial match\"}\n```")
	v, ok := ExtractJudgeVerdict(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if math.Abs(v.Score-0.7) > 1e-9 {
		t.Errorf("score = %g, want 0.7", v.Score)
	}
}

func TestExtractJudgeVerdict_PrefixedWithProse(t *testing.T) {
	// LLMs add throat-clearing despite "Output JSON only."
	out := []byte(`Here is my analysis: {"score": 0.5, "rationale": "right files wrong approach"}`)
	v, ok := ExtractJudgeVerdict(out)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if math.Abs(v.Score-0.5) > 1e-9 {
		t.Errorf("score = %g, want 0.5", v.Score)
	}
}

func TestExtractJudgeVerdict_ClampOutOfRange(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{`{"score": 1.05, "rationale": "great"}`, 1.0},
		{`{"score": -0.2, "rationale": "bad"}`, 0.0},
		{`{"score": 1.0, "rationale": "perfect"}`, 1.0},
	}
	for _, tc := range cases {
		v, ok := ExtractJudgeVerdict([]byte(tc.raw))
		if !ok {
			t.Errorf("ExtractJudgeVerdict(%q) ok=false", tc.raw)
			continue
		}
		if math.Abs(v.Score-tc.want) > 1e-9 {
			t.Errorf("ExtractJudgeVerdict(%q) score=%g, want %g", tc.raw, v.Score, tc.want)
		}
	}
}

func TestExtractJudgeVerdict_RejectsUnparseable(t *testing.T) {
	cases := [][]byte{
		[]byte(``),
		[]byte(`not JSON at all`),
		// Empty result: zero score AND empty rationale — treat as "didn't answer."
		[]byte(`{"score": 0, "rationale": ""}`),
		[]byte(`{"score": 0}`), // missing rationale + zero score
	}
	for _, tc := range cases {
		if _, ok := ExtractJudgeVerdict(tc); ok {
			t.Errorf("expected ok=false for %q", string(tc))
		}
	}
}

// TestScore_PopulatesMultiStageWhenGroundTruthPresent verifies multi-stage
// scoring: when an Entry has IntentGroundTruth + ChangedSymbols and Meta
// has CommitMessages, the DeterministicScorer populates IntentScore /
// SymbolF1 fields / PlanStepsScore alongside the legacy FileF1 — all
// pure-Go, no LLM judge involved.
func TestScore_PopulatesMultiStageWhenGroundTruthPresent(t *testing.T) {
	scorer := DeterministicScorer{}
	plan := Plan{
		Markdown: "## Problem\nRefresh GasTip tracking on header change.\n\n" +
			"## Approach\nModify AnzeonTipEnv.SetCurrentBlock to compare header GasTip.\n\n" +
			"## Expected Changes\n- gas_policy.go: add header GasTip guard\n",
		ExpectedFiles: []string{"gas_policy.go"},
	}
	entry := Entry{
		IntentGroundTruth: "Refresh AnzeonTipEnv currentBlock when header GasTip value changes",
		ChangedSymbols: []string{
			"AnzeonTipEnv.SetCurrentBlock",
			"AnzeonTipEnv.gasTipChanged",
		},
	}
	meta := Meta{
		Title:          "fix: refresh AnzeonTipEnv current block when GasTip changes",
		Files:          []ChangedFile{{Path: "gas_policy.go"}},
		CommitMessages: []string{"fix: refresh AnzeonTipEnv on header GasTip change"},
	}
	got, err := scorer.Score(nil, entry, meta, plan, "diff body")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	// E1: plan mentions GasTip + AnzeonTipEnv, so non-zero overlap expected.
	if got.IntentScore <= 0 {
		t.Errorf("expected IntentScore > 0 (token overlap), got %g", got.IntentScore)
	}
	// E2: plan extracts AnzeonTipEnv.SetCurrentBlock, matches truth[0].
	// 1/1 plan / 2 truth → P=1.0, R=0.5, F1≈0.667.
	if got.SymbolF1 == 0 {
		t.Errorf("expected SymbolF1 > 0 with PascalCase match, got 0; planSymbols=%v truthSymbols=%v", got.PlanSymbols, got.TruthSymbols)
	}
	if len(got.PlanSymbols) == 0 {
		t.Error("plan_symbols should be captured as evidence")
	}
	if len(got.TruthSymbols) != 2 {
		t.Errorf("truth_symbols should mirror Entry.ChangedSymbols, got %v", got.TruthSymbols)
	}
	// E3: commit message and plan share "GasTip" + "refresh" + "AnzeonTipEnv".
	if got.PlanStepsScore <= 0 {
		t.Errorf("expected PlanStepsScore > 0 with overlapping commits, got %g", got.PlanStepsScore)
	}
	// FileF1 still computed (perfect match on gas_policy.go).
	if got.FileF1 != 1 {
		t.Errorf("FileF1 = %g, want 1.0 (single perfect match)", got.FileF1)
	}
}

// TestScore_MultiStageSilentOnLegacyEntries — Entries without
// IntentGroundTruth / ChangedSymbols / CommitMessages should not emit
// any multi-stage fields (omitempty), preserving JSON output stability
// for the four legacy fixture rows (pr69/pr70/pr72/pr74).
func TestScore_MultiStageSilentOnLegacyEntries(t *testing.T) {
	scorer := DeterministicScorer{}
	plan := Plan{Markdown: "some plan", ExpectedFiles: []string{"a.go"}}
	got, err := scorer.Score(nil, Entry{}, Meta{Files: []ChangedFile{{Path: "a.go"}}}, plan, "diff")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got.IntentScore != 0 || got.SymbolF1 != 0 || got.PlanStepsScore != 0 {
		t.Errorf("legacy entry produced multi-stage signal: I=%g S=%g P=%g",
			got.IntentScore, got.SymbolF1, got.PlanStepsScore)
	}
	if len(got.PlanSymbols) != 0 || len(got.TruthSymbols) != 0 {
		t.Errorf("legacy entry leaked symbol evidence: plan=%v truth=%v",
			got.PlanSymbols, got.TruthSymbols)
	}
}

// TestScore_DeterministicFileSet verifies the file-set F1 is computed by the
// deterministic scorer and that NO LLM-judge fields are populated (the binary
// does deterministic scoring only; LLM judging is the agent layer's job).
func TestScore_DeterministicFileSet(t *testing.T) {
	scorer := DeterministicScorer{}
	plan := Plan{
		ExpectedFiles: []string{"a.go", "b.go", "c.go"},
	}
	meta := Meta{
		Files: []ChangedFile{
			{Path: "a.go"},
			{Path: "b.go"},
		},
		Background: "test",
	}
	got, err := scorer.Score(nil, Entry{}, meta, plan, "diff body")
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	// plan=[a,b,c], truth=[a,b] → P=2/3, R=1, F1=0.8.
	if math.Abs(got.FileF1-0.8) > 1e-9 {
		t.Errorf("FileF1 = %g, want 0.8", got.FileF1)
	}
	// Deterministic scorer leaves the LLM-judge fields untouched.
	if got.JudgeScore != 0 || got.JudgeRaw != "" || got.JudgeError != "" {
		t.Errorf("deterministic scorer should not set judge fields: score=%g raw=%q err=%q",
			got.JudgeScore, got.JudgeRaw, got.JudgeError)
	}
}
