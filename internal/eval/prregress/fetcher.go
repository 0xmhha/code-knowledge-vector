package prregress

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// FetchMeta gets the PR's title, body, and changed-file list from
// GitHub via the `gh` CLI. The user must have run `gh auth login`
// once; we do not embed a GitHub token.
//
// We deliberately shell out to `gh` instead of using a Go GitHub
// client because:
//   - `gh` already manages auth (token in OS keyring, scope renewal)
//   - it follows the user's GH_HOST / enterprise config
//   - one binary call is cheaper than pulling in an SDK
//
// Returned Body is the full PR description. Background is the
// extracted "what's wrong" piece — that's the only thing we hand to
// the agent (Solution and Changes stay hidden).
func FetchMeta(ctx context.Context, e Entry) (Meta, error) {
	if err := requireGH(ctx); err != nil {
		return Meta{}, err
	}

	args := []string{
		"pr", "view", fmt.Sprintf("%d", e.PRNumber),
		"--repo", e.Repo,
		"--json", "title,body,files",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return Meta{}, fmt.Errorf("gh pr view %s#%d: %w (stderr: %s)",
			e.Repo, e.PRNumber, err, stderr)
	}

	var raw struct {
		Title string         `json:"title"`
		Body  string         `json:"body"`
		Files []ChangedFile `json:"files"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Meta{}, fmt.Errorf("parse gh pr view JSON: %w", err)
	}
	if raw.Title == "" {
		return Meta{}, fmt.Errorf("gh pr view returned no title — PR %s#%d may not exist", e.Repo, e.PRNumber)
	}
	if len(raw.Files) == 0 {
		return Meta{}, fmt.Errorf("gh pr view returned no files — PR %s#%d may be empty or inaccessible", e.Repo, e.PRNumber)
	}
	return Meta{
		Title:      raw.Title,
		Body:       raw.Body,
		Background: ExtractBackground(raw.Body),
		Files:      raw.Files,
	}, nil
}

// FetchDiff pulls the PR's unified diff via `gh pr diff`. This is the
// "actual code change" the judge compares the agent's plan against.
//
// gh returns the diff as one large text blob (all files concatenated,
// standard git diff format). The score layer caps how much of this
// gets embedded in the judge prompt, so callers don't need to chunk
// upstream.
func FetchDiff(ctx context.Context, e Entry) (string, error) {
	if err := requireGH(ctx); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "diff",
		fmt.Sprintf("%d", e.PRNumber),
		"--repo", e.Repo,
	)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("gh pr diff %s#%d: %w (stderr: %s)",
			e.Repo, e.PRNumber, err, stderr)
	}
	return string(out), nil
}

// requireGH fails fast with an actionable message if `gh` is missing
// or not authenticated. Without auth gh returns a confusing 401 deep
// in the JSON path; checking up front is friendlier.
func requireGH(ctx context.Context) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found in PATH — install with: brew install gh")
	}
	// `gh auth status` exits 0 when logged in, non-zero otherwise.
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh CLI not authenticated — run: gh auth login")
	}
	return nil
}

// backgroundSection matches the PR description's "Background" header.
// We intentionally match a wide set of formats people actually use:
//   ### Background
//   ## Background
//   **Background**
// Case-insensitive; the next same-or-higher-level header stops the body.
var backgroundHeaderRE = regexp.MustCompile(`(?im)^\s*(#{1,6}\s+|\*\*)background(\s*\*\*)?\s*:?\s*$`)

// ExtractBackground pulls the "Background" section out of a PR
// description. If the section isn't present (some teams don't use the
// pattern), the whole body is returned as a conservative fallback —
// better to give the agent too much than to give it nothing.
//
// The boundary is "the next header at the same or higher level."
// Practically: "### Background" runs until the next "### " or "## " or
// "# ". Anything beyond that header (Solution, Changes, etc.) is
// stripped so the agent doesn't peek at the answer.
func ExtractBackground(body string) string {
	if body == "" {
		return ""
	}
	loc := backgroundHeaderRE.FindStringIndex(body)
	if loc == nil {
		// No header found — return the whole body. This is the right
		// fallback because the failure mode of "agent sees too much
		// context" is far less harmful than "agent sees nothing."
		return strings.TrimSpace(body)
	}
	rest := body[loc[1]:]
	// Find the next header line that ends a section. We accept either
	//   (a) a markdown header: 1-6 #s followed by space
	//   (b) a bold-only line: **Section Name** (optional trailing colon)
	// to mirror the same set of styles backgroundHeaderRE matches.
	// Mixing styles inside one PR is rare but harmless to support.
	stopRE := regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s+\S|\*\*[^*\n]+\*\*\s*:?\s*$)`)
	if stop := stopRE.FindStringIndex(rest); stop != nil {
		return strings.TrimSpace(rest[:stop[0]])
	}
	return strings.TrimSpace(rest)
}
