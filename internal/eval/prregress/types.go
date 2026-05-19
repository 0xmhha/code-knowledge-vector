// Package prregress implements PR-based regression evaluation:
// given a merged PR, check out the world *before* it landed, build a
// ckv index over that snapshot, hand the PR's Background to an agent,
// and compare the agent's plan against what the PR actually did.
//
// Why this exists: ckv eval's recall@k / MRR metrics measure retrieval
// quality on synthetic queries. They do NOT show whether the system
// helps an agent reach the right answer end-to-end. PR-regression
// closes that gap — every passing PR is empirical proof that an agent
// armed with ckv would have proposed something close to what shipped.
//
// Module layout (one file per concern, all in this package):
//   types.go    — data model + fixture loader (this file)
//   fetcher.go  — gh CLI metadata fetch (PR title/body/files)
//   checkout.go — detached git worktree at base_sha
//   ground.go   — PR diff → changed file set (truth)
//   agent.go    — Claude CLI headless plan generation
//   score.go    — LLM-as-judge + file-set F1
//   runner.go   — flow orchestration over a Fixture
//
// Build tag none — uses standard library + gh CLI subprocess + git.
// Test tag prregress_smoke for the end-to-end PR #70 test that depends
// on the source repo + Claude CLI + network access being available.
package prregress

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FixtureSchemaVersion is the YAML schema version this package
// reads/writes. Bump on incompatible changes.
const FixtureSchemaVersion = "1"

// DefaultThreshold is the minimum LLM-judge similarity score that
// counts as "agent reproduced the intent of the PR." Per
// review-direction-2026-05-18.md §6.1 Challenge 3.
const DefaultThreshold = 0.80

// Fixture is the parsed `testdata/prs.yaml` document.
type Fixture struct {
	SchemaVersion string  `yaml:"schema_version"`
	PRs           []Entry `yaml:"prs"`
}

// Entry is one PR's fixture row. Fields match testdata/prs.yaml; see
// that file's header for semantics.
type Entry struct {
	ID         string  `yaml:"id"`
	Repo       string  `yaml:"repo"`        // owner/name
	PRNumber   int     `yaml:"pr_number"`
	SourcePath string  `yaml:"source_path"` // local clone path
	BaseSHA    string  `yaml:"base_sha"`
	Threshold  float64 `yaml:"threshold,omitempty"`
	Notes      string  `yaml:"notes,omitempty"`
}

// LoadFixture reads + validates a PR fixture YAML. Validation is
// fail-loud: any missing required field on any entry is an error, not
// a warning, because a malformed entry corrupts the regression report
// silently.
func LoadFixture(path string) (*Fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read PR fixture %s: %w", path, err)
	}
	var fx Fixture
	if err := yaml.Unmarshal(raw, &fx); err != nil {
		return nil, fmt.Errorf("parse PR fixture %s: %w", path, err)
	}
	if fx.SchemaVersion != FixtureSchemaVersion {
		return nil, fmt.Errorf("PR fixture %s schema_version=%q, want %q",
			path, fx.SchemaVersion, FixtureSchemaVersion)
	}
	if len(fx.PRs) == 0 {
		return nil, fmt.Errorf("PR fixture %s has no entries", path)
	}
	seen := make(map[string]struct{}, len(fx.PRs))
	for i, e := range fx.PRs {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("PR fixture %s entry[%d]: %w", path, i, err)
		}
		if _, dup := seen[e.ID]; dup {
			return nil, fmt.Errorf("PR fixture %s duplicate id %q", path, e.ID)
		}
		seen[e.ID] = struct{}{}
		// Backfill threshold if omitted.
		if fx.PRs[i].Threshold == 0 {
			fx.PRs[i].Threshold = DefaultThreshold
		}
	}
	return &fx, nil
}

func (e Entry) validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if !strings.Contains(e.Repo, "/") {
		return fmt.Errorf("repo %q must be owner/name", e.Repo)
	}
	if e.PRNumber <= 0 {
		return fmt.Errorf("pr_number must be > 0, got %d", e.PRNumber)
	}
	if strings.TrimSpace(e.SourcePath) == "" {
		return fmt.Errorf("source_path is required")
	}
	if len(e.BaseSHA) < 7 {
		return fmt.Errorf("base_sha %q looks too short (want full or ≥7-char prefix)", e.BaseSHA)
	}
	if e.Threshold < 0 || e.Threshold > 1 {
		return fmt.Errorf("threshold %g must be in [0, 1]", e.Threshold)
	}
	return nil
}

// Meta is the slice of GitHub PR data the harness consumes. Captured
// once via gh CLI and passed forward; we deliberately don't re-fetch
// during scoring to keep the regression deterministic across reruns.
type Meta struct {
	Title      string       `json:"title"`
	Body       string       `json:"body"`        // full PR description (Background + Solution + Changes)
	Background string       `json:"background"`  // extracted: the "what's wrong" piece, agent sees this
	Files      []ChangedFile `json:"files"`      // truth: what the PR actually changed
}

// ChangedFile is one file the PR touched, as reported by gh CLI.
// Line-level diff parsing is deferred to a follow-up (symbol-set F1
// would need it; file-set F1 only needs the path).
type ChangedFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Plan is what the agent produced after seeing only Background.
// Markdown is the free-form rationale; ExpectedFiles is the structured
// section the agent is asked to emit at the end (parser pulls it back
// out for file-set F1 scoring).
type Plan struct {
	Markdown      string   `json:"markdown"`
	ExpectedFiles []string `json:"expected_files"`
}

// Score combines the two metrics the user approved (autoplan v1.1
// Challenge 3): LLM-as-judge primary + file-set F1 secondary.
type Score struct {
	JudgeScore float64 `json:"judge_score"` // 0..1, LLM rubric output
	JudgeRaw   string  `json:"judge_raw,omitempty"`
	JudgeError string  `json:"judge_error,omitempty"`

	FileF1        float64 `json:"file_f1"`
	FilePrecision float64 `json:"file_precision"`
	FileRecall    float64 `json:"file_recall"`

	PlanFiles  []string `json:"plan_files"`
	TruthFiles []string `json:"truth_files"`
}

// Result is one Entry's outcome. Persisted to disk for report
// generation.
type Result struct {
	Entry Entry  `json:"entry"`
	Meta  Meta   `json:"meta"`
	Plan  Plan   `json:"plan"`
	Score Score  `json:"score"`
	Pass  bool   `json:"pass"`            // judge_score >= entry.threshold
	Error string `json:"error,omitempty"` // first-error wins; downstream stages skip
}

// FileSetF1 computes precision / recall / F1 of two file path sets.
// Comparison is path-string equality after normalizing separators (the
// gh API returns POSIX paths; git on macOS/Linux already uses them).
// Empty truth → 0; empty plan with non-empty truth → 0/0/0.
func FileSetF1(plan, truth []string) (precision, recall, f1 float64) {
	if len(truth) == 0 {
		return 0, 0, 0
	}
	truthSet := make(map[string]struct{}, len(truth))
	for _, p := range truth {
		truthSet[p] = struct{}{}
	}
	planSet := make(map[string]struct{}, len(plan))
	var tp int
	for _, p := range plan {
		if _, dup := planSet[p]; dup {
			continue
		}
		planSet[p] = struct{}{}
		if _, ok := truthSet[p]; ok {
			tp++
		}
	}
	if len(planSet) > 0 {
		precision = float64(tp) / float64(len(planSet))
	}
	recall = float64(tp) / float64(len(truthSet))
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}
	return precision, recall, f1
}

// SortedFiles returns a stable-order copy of the file path slice.
// Used to keep JSON output diffable across reruns.
func SortedFiles(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
