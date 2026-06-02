package prregress

import (
	"context"
	"regexp"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// PlanAgent turns a problem description (PR Background) + ckv search
// hits into a markdown implementation plan. The agent never sees the
// PR's Solution / Changes — that's the gap we're measuring.
//
// The interface is the seam where LLM work crosses out of this binary
// (00 §2.2: binary = deterministic). The ckv binary ships NO concrete
// PlanAgent — plan generation is inherently an LLM task, so the
// agent/session layer injects its own implementation. The plan it
// returns is parsed back into structured form by ExtractExpectedFiles,
// which stays here as a deterministic, tested parser.
type PlanAgent interface {
	Generate(ctx context.Context, e Entry, m Meta, hints []query.Hit) (Plan, error)
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
