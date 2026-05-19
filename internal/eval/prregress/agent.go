package prregress

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// PlanAgent turns a problem description (PR Background) + ckv search
// hits into a markdown implementation plan. The agent never sees the
// PR's Solution / Changes — that's the gap we're measuring.
//
// Interface kept small so the test harness can stub it without dragging
// in Claude CLI / network. Production impl is ClaudePlanAgent.
type PlanAgent interface {
	Generate(ctx context.Context, e Entry, m Meta, hints []query.Hit) (Plan, error)
}

// ClaudePlanAgent invokes the Claude Code CLI in headless mode to
// produce an implementation plan. Same binary as internal/judge but
// a different prompt — keeping the two as separate types avoids
// implicit coupling between "score this" and "plan this."
type ClaudePlanAgent struct {
	// Binary is the executable. Defaults to "claude".
	Binary string
	// Timeout caps wall time per Generate(). Plan generation usually
	// takes longer than grading (the agent may inspect snippets,
	// reason about architecture) so the default is generous.
	Timeout time.Duration
	// Model lets callers pin a model id (forwarded as `--model`).
	Model string
}

// NewClaudePlanAgent returns a ClaudePlanAgent with sane defaults.
func NewClaudePlanAgent() *ClaudePlanAgent {
	return &ClaudePlanAgent{Binary: "claude", Timeout: 5 * time.Minute}
}

// Generate builds a planning prompt that hands the agent the
// problem description plus top ckv hits as evidence, runs Claude CLI,
// and parses the markdown response into Plan{Markdown, ExpectedFiles}.
//
// On failure the returned Plan keeps the raw output (truncated) for
// inspection and the error explains why parsing or invocation failed.
// The runner can choose to treat that as a zero-score Result rather
// than aborting the whole fixture.
func (a *ClaudePlanAgent) Generate(ctx context.Context, e Entry, m Meta, hints []query.Hit) (Plan, error) {
	if a.Binary == "" {
		a.Binary = "claude"
	}
	if a.Timeout == 0 {
		a.Timeout = 5 * time.Minute
	}
	if _, err := exec.LookPath(a.Binary); err != nil {
		return Plan{}, fmt.Errorf("plan agent: %s not in PATH", a.Binary)
	}
	if strings.TrimSpace(m.Background) == "" {
		return Plan{}, fmt.Errorf("plan agent: empty Background — nothing to plan from")
	}

	prompt := buildPlanPrompt(e, m.Background, hints)
	cctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()

	args := []string{"-p", prompt}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	out, err := exec.CommandContext(cctx, a.Binary, args...).Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = truncStr(string(ee.Stderr), 1000)
		}
		return Plan{}, fmt.Errorf("plan agent: %s exec failed: %w (stderr: %s)", a.Binary, err, stderr)
	}

	markdown := strings.TrimSpace(string(out))
	if markdown == "" {
		return Plan{}, fmt.Errorf("plan agent: empty response")
	}
	return Plan{
		Markdown:      markdown,
		ExpectedFiles: ExtractExpectedFiles(markdown),
	}, nil
}

// buildPlanPrompt assembles the planning prompt. We feed the agent:
//   - The problem statement (PR Background only).
//   - The repo path so it can inspect files.
//   - Top ckv search hits as the "candidates worth looking at" — this
//     mirrors how the runtime coding agent will use ckv.
//   - An explicit output schema (markdown body + structured
//     "Expected Changes" section we can parse downstream).
func buildPlanPrompt(e Entry, background string, hints []query.Hit) string {
	var b strings.Builder
	b.WriteString("You are an experienced software engineer.\n")
	b.WriteString("You will be shown a software problem and a set of candidate code locations from a vector-search index over the codebase. ")
	b.WriteString("Your task: write a concise implementation plan in markdown that explains HOW you would solve the problem.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Focus on WHICH files change and WHAT the high-level approach is. Do not write full implementation code.\n")
	b.WriteString("- Use the search hints below as a starting point, but you may name additional files if the hints look incomplete.\n")
	b.WriteString("- You may inspect files in the repo at: " + e.SourcePath + "\n")
	b.WriteString("- End your response with a section titled exactly `## Expected Changes` listing one bullet per file in the form:\n")
	b.WriteString("    - <relative/path/to/file>: <one-line description of the change>\n\n")
	b.WriteString("PROBLEM:\n")
	b.WriteString(background)
	b.WriteString("\n\nVECTOR-SEARCH HINTS (top results from ckv on this problem):\n")
	if len(hints) == 0 {
		b.WriteString("(no hints — proceed with file inspection)\n")
	} else {
		for i, h := range hints {
			fmt.Fprintf(&b, "%d. %s:%d-%d  symbol=%s  kind=%s  score=%.3f\n",
				i+1, h.Citation.File, h.Citation.StartLine, h.Citation.EndLine,
				defaultIfEmpty(h.Symbol, "(none)"), h.SymbolKind, h.Score.Normalized)
			fmt.Fprintf(&b, "   snippet: %s\n", truncStr(strings.ReplaceAll(h.Snippet, "\n", " "), 200))
		}
	}
	b.WriteString("\nOutput markdown only. The final section must be exactly `## Expected Changes`.")
	return b.String()
}

// expectedHeaderRE matches the structured "Expected Changes" section
// header in a few forms the LLM might emit. Case-insensitive; the
// surrounding text after the header is the bullet list we parse.
var expectedHeaderRE = regexp.MustCompile(`(?im)^\s*(?:#{1,6}|\*\*)\s*expected\s*(?:file|change)s?\s*(?:\*\*)?\s*:?\s*$`)

// filePathBulletRE captures the first whitespace-bounded token after
// a bullet marker. We're permissive about the separator that follows
// the path (colon, dash, em-dash, space) — LLMs vary, but the token
// itself is well-defined.
var filePathBulletRE = regexp.MustCompile(`^\s*[-*+]\s+([^\s:]+)`)

// ExtractExpectedFiles parses the "Expected Changes" section of a
// markdown plan and returns the file paths the agent listed.
//
// Robust to:
//   - Different header levels (`##`, `###`, `**Expected Changes**`).
//   - Singular vs plural ("Expected File", "Expected Changes", etc.).
//   - Various bullet markers (`-`, `*`, `+`).
//   - A trailing colon and the agent's free-form description after the path.
//
// Returned slice is deduplicated but order-preserving (first occurrence
// wins) so the agent's ranking is observable in logs. The caller can
// sort downstream when needed.
func ExtractExpectedFiles(markdown string) []string {
	if markdown == "" {
		return nil
	}
	header := expectedHeaderRE.FindStringIndex(markdown)
	if header == nil {
		return nil
	}
	rest := markdown[header[1]:]
	// A bullet section ends at the next markdown header at any level,
	// or end-of-string. Same rule as fetcher.go's stop pattern.
	stopRE := regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s+\S|\*\*[^*\n]+\*\*\s*:?\s*$)`)
	if stop := stopRE.FindStringIndex(rest); stop != nil {
		rest = rest[:stop[0]]
	}
	seen := make(map[string]struct{})
	var out []string
	for _, line := range strings.Split(rest, "\n") {
		m := filePathBulletRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Strip a trailing colon: LLMs sometimes emit `- foo.go:` and
		// sometimes `- foo.go : description`. We want "foo.go" either way.
		p := strings.TrimSuffix(m[1], ":")
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
