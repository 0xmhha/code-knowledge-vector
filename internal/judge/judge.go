// Package judge is the LLM-as-judge layer for ckv eval. The default
// implementation (ClaudeCLI) invokes the Claude Code CLI in headless
// non-interactive mode (`claude -p "<prompt>"`) and parses a small
// JSON verdict from stdout.
//
// Why a thin interface? cli-wrapper (the canonical harness for
// managing Claude Code via Go) is designed for long-running PTY
// processes; our use case is one-shot per query. The Judge interface
// here keeps us source-compatible if we later swap to cli-wrapper for
// streaming, retry, or resource-cap features.
//
// Eval treats judge failures as non-fatal: a missing `claude` binary,
// a network blip, or an unparseable verdict just yields a zero-score
// Verdict with the raw output captured for inspection — the
// quantitative recall/MRR metrics still report.
package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// Verdict is one LLM judgment about one (intent, hits) pair.
type Verdict struct {
	QueryID   string `json:"query_id"`
	Score     int    `json:"score"` // 1..5; 0 if parsing failed
	Rationale string `json:"rationale,omitempty"`
	Raw       string `json:"raw,omitempty"` // raw LLM stdout (truncated)
	Error     string `json:"error,omitempty"`
}

// Judge grades one (intent, hits) pair. Implementations MUST be safe
// to call concurrently from multiple goroutines.
type Judge interface {
	Grade(ctx context.Context, queryID, intent string, hits []query.Hit) Verdict
}

// ClaudeCLI invokes the Claude Code CLI. The user is responsible for
// having `claude` installed and authenticated (claude.ai login).
type ClaudeCLI struct {
	// Binary is the executable name or path. Defaults to "claude".
	Binary string
	// Timeout caps per-grade wall time. Defaults to 60s.
	Timeout time.Duration
	// Model lets callers pin a model id (forwarded as `--model`).
	// Empty → CLI default.
	Model string
}

// NewClaudeCLI returns a Judge with sane defaults.
func NewClaudeCLI() *ClaudeCLI {
	return &ClaudeCLI{Binary: "claude", Timeout: 60 * time.Second}
}

// Grade builds a prompt, invokes the CLI, and parses the verdict.
// Errors are folded into the returned Verdict (Score=0, Error filled);
// callers (eval runner) keep going so one bad grade doesn't tank the
// whole report.
func (c *ClaudeCLI) Grade(ctx context.Context, queryID, intent string, hits []query.Hit) Verdict {
	v := Verdict{QueryID: queryID}
	if c.Binary == "" {
		c.Binary = "claude"
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if _, err := exec.LookPath(c.Binary); err != nil {
		v.Error = fmt.Sprintf("judge: %s not in PATH", c.Binary)
		return v
	}

	prompt := buildPrompt(intent, hits)
	cctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	args := []string{"-p", prompt}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	out, err := exec.CommandContext(cctx, c.Binary, args...).Output()
	if err != nil {
		v.Error = fmt.Sprintf("judge: %s exec failed: %v", c.Binary, err)
		// Capture stderr from ExitError when present so the user can
		// debug auth / network issues without rerunning.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			v.Raw = truncate(string(ee.Stderr), 1000)
		}
		return v
	}
	v.Raw = truncate(string(out), 2000)
	if parsed, ok := extractVerdict(out); ok {
		v.Score = parsed.Score
		v.Rationale = parsed.Rationale
	} else {
		v.Error = "judge: could not parse {score, rationale} from output"
	}
	return v
}

// buildPrompt composes the one-shot judging prompt. We keep it small
// — the LLM should see the intent, the top hits with citations and
// snippets, and the output schema. Snippets are truncated to keep
// the prompt under a few KB.
func buildPrompt(intent string, hits []query.Hit) string {
	var b strings.Builder
	b.WriteString("You are grading a code search system. Output JSON ONLY (no prose, no fences).\n")
	b.WriteString("Schema: {\"score\": <integer 1-5>, \"rationale\": \"<one sentence>\"}\n")
	b.WriteString("Score rubric:\n")
	b.WriteString("  5 — top hit precisely answers the intent at the right file:line.\n")
	b.WriteString("  4 — correct file in top-3, line range close but not perfect.\n")
	b.WriteString("  3 — relevant file appears in top-5 but not at top.\n")
	b.WriteString("  2 — partially relevant hits only.\n")
	b.WriteString("  1 — none of the hits answer the intent.\n\n")
	b.WriteString("INTENT: ")
	b.WriteString(intent)
	b.WriteString("\n\nHITS:\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s:%d-%d  symbol=%s  kind=%s  score=%.3f\n",
			i+1, h.Citation.File, h.Citation.StartLine, h.Citation.EndLine,
			defaultIfEmpty(h.Symbol, "(none)"), h.SymbolKind, h.Score.Normalized)
		fmt.Fprintf(&b, "   snippet: %s\n", truncate(strings.ReplaceAll(h.Snippet, "\n", " "), 200))
	}
	b.WriteString("\nOutput JSON only.")
	return b.String()
}

// extractVerdict pulls the first JSON object containing "score" out
// of the LLM output. Tolerant: Claude Code sometimes adds preamble
// or wraps in code fences despite the prompt — we look for the
// outermost {...} block first.
func extractVerdict(out []byte) (parsed struct {
	Score     int    `json:"score"`
	Rationale string `json:"rationale"`
}, ok bool) {
	// Strip ``` fences if present.
	body := strings.TrimSpace(string(out))
	body = stripFences(body)
	// First, try the whole body.
	if err := json.Unmarshal([]byte(body), &parsed); err == nil && parsed.Score > 0 {
		return parsed, true
	}
	// Otherwise, look for the first {...} block.
	re := regexp.MustCompile(`(?s)\{[^{}]*"score"[^{}]*\}`)
	if loc := re.FindString(body); loc != "" {
		if err := json.Unmarshal([]byte(loc), &parsed); err == nil && parsed.Score > 0 {
			return parsed, true
		}
	}
	return parsed, false
}

var fenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)```")

func stripFences(s string) string {
	if m := fenceRE.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

func truncate(s string, n int) string {
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
