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

// Score_FileSetOnly verifies that when the judge subprocess can't be
// invoked (claude not in PATH), we still produce a Score with the file
// F1 portion populated and a non-empty JudgeError. The runner relies
// on this to keep producing useful output in CI / degraded envs.
func TestScore_FileSetSurvivesMissingClaude(t *testing.T) {
	scorer := &ClaudeJudgeScorer{
		Binary:       "definitely-not-a-real-binary-9af3",
		Timeout:      0, // will be defaulted
		MaxDiffBytes: 0, // will be defaulted
	}
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
	if got.JudgeError == "" {
		t.Error("expected JudgeError to be set when binary missing")
	}
	// Score.FileF1 should still reflect the file-set comparison even
	// though the LLM call failed: plan=[a,b,c], truth=[a,b] → P=2/3, R=1, F1=0.8.
	if math.Abs(got.FileF1-0.8) > 1e-9 {
		t.Errorf("FileF1 = %g, want 0.8 (file-set part must survive judge failure)", got.FileF1)
	}
}
