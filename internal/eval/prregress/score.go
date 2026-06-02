package prregress

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// JudgeScorer evaluates how closely a Plan's intent matches the actual
// PR change. The combined Score carries the deterministic file/intent/
// symbol/plan-step metrics; an agent-layer implementation may also fill
// the LLM-judge fields (JudgeScore/JudgeRaw). The threshold gate reads
// JudgeScore, so a deterministic-only run (JudgeScore=0) never "passes"
// the LLM gate — that's intentional: LLM judging is the agent layer's job.
type JudgeScorer interface {
	Score(ctx context.Context, e Entry, m Meta, plan Plan, diff string) (Score, error)
}

// DeterministicScorer computes every pure-Go metric in the Score (file-set
// F1, intent token-F1, symbol F1, plan-steps overlap) with NO LLM call —
// the binary = deterministic half of pr-regression scoring (00 §2.2). It
// leaves the LLM-judge fields (JudgeScore/JudgeRaw) zero; the agent/session
// layer injects a JudgeScorer that adds those when an LLM judge is wanted.
// The diff argument is accepted to satisfy JudgeScorer but is unused here
// (the deterministic metrics compare plan vs. PR metadata, not raw diff text).
type DeterministicScorer struct{}

// Score computes the deterministic plan-vs-PR metrics. Each multi-stage
// metric is guarded so legacy fixture rows (no IntentGroundTruth,
// ChangedSymbols, or Commits) emit zero/omitempty fields instead of false
// positives. IntentCosine is populated separately by RunEntry when a real
// embedder is configured (see runner.go).
func (DeterministicScorer) Score(_ context.Context, e Entry, m Meta, plan Plan, _ string) (Score, error) {
	truth := TruthFiles(m)
	planFiles := SortedFiles(plan.ExpectedFiles)
	precision, recall, f1 := FileSetF1(plan.ExpectedFiles, truth)

	score := Score{
		FileF1:        f1,
		FilePrecision: precision,
		FileRecall:    recall,
		PlanFiles:     planFiles,
		TruthFiles:    truth,
	}

	planSteps := ExtractPlanSteps(plan.Markdown)

	reference := strings.TrimSpace(e.IntentGroundTruth)
	if reference == "" {
		reference = strings.TrimSpace(m.Title)
	}
	if reference != "" && planSteps != "" {
		score.IntentScore = IntentScore(planSteps, reference)
	}

	if len(e.ChangedSymbols) > 0 {
		planSymbols := ExtractPlanSymbols(plan.Markdown)
		sp, sr, sf := SymbolF1(planSymbols, e.ChangedSymbols)
		score.SymbolPrecision = sp
		score.SymbolRecall = sr
		score.SymbolF1 = sf
		score.PlanSymbols = planSymbols
		score.TruthSymbols = e.ChangedSymbols
	}

	if len(m.CommitMessages) > 0 && planSteps != "" {
		score.PlanStepsScore = PlanStepsScore(planSteps, strings.Join(m.CommitMessages, "\n\n"))
	}

	return score, nil
}

// Verdict is the parsed judge JSON. Score is float [0..1].
type Verdict struct {
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale"`
}

// ExtractJudgeVerdict pulls the first {score, rationale} object out of
// LLM output. Tolerant to:
//   - ``` fences (json or plain)
//   - leading/trailing prose ("Here's my analysis: { ... }")
//   - score out of range — clamped to [0, 1], not rejected
//
// Returns ok=false if no parseable object found.
func ExtractJudgeVerdict(out []byte) (Verdict, bool) {
	body := strings.TrimSpace(string(out))
	body = stripJudgeFences(body)

	tryUnmarshal := func(s string) (Verdict, bool) {
		var v Verdict
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return v, false
		}
		// Empty rationale + zero score = parse-but-not-meaningful.
		// We treat that as a failed parse rather than a valid 0-grade
		// because in practice it means the LLM didn't actually answer.
		if v.Score == 0 && v.Rationale == "" {
			return v, false
		}
		// Clamp instead of reject: the LLM occasionally emits 1.05 or
		// -0.1 in the rationale's confidence; we'd rather have a
		// slightly clamped value than a parse failure.
		if v.Score < 0 {
			v.Score = 0
		}
		if v.Score > 1 {
			v.Score = 1
		}
		return v, true
	}

	if v, ok := tryUnmarshal(body); ok {
		return v, true
	}
	// Fallback: regex-find the first {...} that mentions "score".
	re := regexp.MustCompile(`(?s)\{[^{}]*"score"[^{}]*\}`)
	if loc := re.FindString(body); loc != "" {
		if v, ok := tryUnmarshal(loc); ok {
			return v, true
		}
	}
	return Verdict{}, false
}

var judgeFenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)```")

func stripJudgeFences(s string) string {
	if m := judgeFenceRE.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}
