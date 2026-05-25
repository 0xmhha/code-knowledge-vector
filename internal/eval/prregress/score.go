package prregress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// JudgeScorer evaluates how closely a Plan's intent matches the actual
// PR diff. Returns the combined Score (LLM-judge primary + file F1
// secondary). Per autoplan v1.1 Challenge 3, both metrics are reported
// and the threshold gate uses the LLM-judge score.
type JudgeScorer interface {
	Score(ctx context.Context, e Entry, m Meta, plan Plan, diff string) (Score, error)
}

// ClaudeJudgeScorer calls Claude CLI to grade the plan against the
// actual diff on a 0..1 semantic similarity scale, and computes the
// file-set F1 in pure Go.
type ClaudeJudgeScorer struct {
	Binary  string
	Timeout time.Duration
	Model   string

	// MaxDiffBytes truncates the diff before it's embedded in the
	// judge prompt. Large PRs (hundreds of KB) blow past Claude's
	// context window; we cap per-call to keep wall time predictable.
	// Truncation is by-byte at this layer — fine-grained per-file
	// truncation is a follow-up if it matters.
	MaxDiffBytes int
}

// NewClaudeJudgeScorer returns a ClaudeJudgeScorer with sane defaults.
func NewClaudeJudgeScorer() *ClaudeJudgeScorer {
	return &ClaudeJudgeScorer{
		Binary:       "claude",
		Timeout:      3 * time.Minute,
		MaxDiffBytes: 64 * 1024, // 64 KB ≈ 1500 LOC of diff, plenty for most PRs
	}
}

// Score grades (plan vs diff). The pure-Go parts run first — file-set
// F1 and the NEW-4 multi-stage metrics (E1 intent, E2 symbol, E3 plan
// steps) — so a report has signal even when the LLM call fails. The
// LLM call runs last; on failure JudgeScore stays 0 and JudgeError
// records why.
func (s *ClaudeJudgeScorer) Score(ctx context.Context, e Entry, m Meta, plan Plan, diff string) (Score, error) {
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

	// NEW-4 multi-stage metrics — all pure-Go, deterministic. Each is
	// guarded so legacy fixture rows (no IntentGroundTruth,
	// ChangedSymbols, or Commits) emit zero/omitempty fields instead of
	// false positives. The IntentCosine variant is populated by
	// RunEntry when a real embedder is configured (see runner.go).
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

	if s.Binary == "" {
		s.Binary = "claude"
	}
	if s.Timeout == 0 {
		s.Timeout = 3 * time.Minute
	}
	if s.MaxDiffBytes <= 0 {
		s.MaxDiffBytes = 64 * 1024
	}

	if _, err := exec.LookPath(s.Binary); err != nil {
		score.JudgeError = fmt.Sprintf("judge scorer: %s not in PATH", s.Binary)
		return score, nil
	}

	prompt := buildJudgePrompt(m.Background, plan.Markdown, truncStr(diff, s.MaxDiffBytes))
	cctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	args := []string{"-p", prompt}
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	out, err := exec.CommandContext(cctx, s.Binary, args...).Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = truncStr(string(ee.Stderr), 1000)
		}
		score.JudgeError = fmt.Sprintf("judge scorer: %s exec failed: %v (stderr: %s)", s.Binary, err, stderr)
		return score, nil
	}

	score.JudgeRaw = truncStr(string(out), 2000)
	verdict, ok := ExtractJudgeVerdict(out)
	if !ok {
		score.JudgeError = "judge scorer: could not parse {score, rationale} from output"
		return score, nil
	}
	score.JudgeScore = verdict.Score
	if verdict.Rationale != "" {
		// Keep both raw and rationale — raw helps debug parsing
		// surprises; rationale is the human-readable summary.
		score.JudgeRaw = verdict.Rationale
	}
	return score, nil
}

// buildJudgePrompt assembles the grading prompt. Rubric is deliberately
// concrete (0.0 / 0.5 / 1.0 anchored to clear cases) so the LLM lands
// on stable scores across reruns instead of drifting per-call.
//
// Alternative-solution policy (PRR-5): software problems usually admit
// more than one correct fix. The rubric now credits plans whose
// approach would actually solve the PROBLEM, even when the files they
// touch differ from the merged diff. The diff is *one* known-good
// solution, not the only one.
func buildJudgePrompt(background, planMarkdown, diff string) string {
	var b strings.Builder
	b.WriteString("You are grading an AI-written implementation plan against the actual code change that solved the same problem. Output JSON only (no prose, no fences).\n\n")
	b.WriteString("Schema: {\"score\": <float in [0.0, 1.0]>, \"rationale\": \"<one sentence>\"}\n\n")
	b.WriteString("Rubric — rate whether the plan would *solve the PROBLEM*, not whether it reproduces the diff verbatim:\n")
	b.WriteString("  1.0 — Plan and diff converge: same key files, same approach. Plan would clearly fix the problem.\n")
	b.WriteString("  0.8 — Either (a) plan covers the core change with minor file or detail misses, OR (b) plan proposes a different-but-valid approach that would solve the problem (e.g., adds a guard at the caller instead of the callee; both ship a fix). Different files are OK if the engineering trade-off makes sense.\n")
	b.WriteString("  0.5 — Plan identifies the right area (subsystem, package, layer) but the proposed change either misses the failure mode or contradicts the diff's intent without offering a valid alternative.\n")
	b.WriteString("  0.2 — Plan mentions related code but the proposed change wouldn't actually fix the reported problem.\n")
	b.WriteString("  0.0 — Plan is unrelated to what the diff does.\n\n")
	b.WriteString("Alternative-solution rule: if the plan's approach would *actually fix the PROBLEM described in the PR Background* — verify by reading the plan against the problem, not against the diff — assign at least 0.8 even when the plan touches different files than the diff. The diff is one known-good answer; do not punish a different correct answer.\n\n")
	b.WriteString("Avoid false-credit: a plan that lists the right files but proposes the wrong change (e.g., 'modify foo.go to add logging' when the actual fix is a bounds check) is 0.5, not 0.8. The approach has to actually solve the problem.\n\n")
	b.WriteString("PROBLEM (PR Background):\n")
	b.WriteString(background)
	b.WriteString("\n\nPLAN:\n")
	b.WriteString(planMarkdown)
	b.WriteString("\n\nACTUAL DIFF:\n")
	b.WriteString(diff)
	b.WriteString("\n\nOutput JSON only.")
	return b.String()
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
