// Package freshness compares an index's manifest against the live git
// HEAD of its source tree. Used by both `ckv freshness` (CLI) and the
// MCP `cks.ops.get_freshness` tool.
package freshness

import (
	"errors"
	"os/exec"
	"strings"
)

// Report is the on-the-wire shape consumed by JSON output + MCP tool.
type Report struct {
	IndexedHead  string   `json:"indexed_head"`
	CurrentHead  string   `json:"current_head"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Stale        bool     `json:"stale"`
	Fresh        bool     `json:"fresh"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Check runs `git rev-parse HEAD` and `git diff --name-only` against
// srcRoot. Stale is true iff CurrentHead != IndexedHead. ChangedFiles
// is populated only when both heads are present and the diff succeeds;
// transient git errors surface as warnings rather than hard failures
// (callers may still want a Report indicating "git unavailable").
func Check(srcRoot, indexedHead string) (Report, error) {
	if srcRoot == "" {
		return Report{}, errors.New("freshness: empty srcRoot")
	}
	r := Report{IndexedHead: indexedHead}

	current, err := gitRevParseHead(srcRoot)
	if err != nil {
		r.Warnings = append(r.Warnings, "git_unavailable: "+err.Error())
		// Without a current HEAD we can't compute staleness; treat as
		// stale-unknown but Fresh=false so callers err on the side of caution.
		return r, nil
	}
	r.CurrentHead = current

	if indexedHead == "" {
		r.Warnings = append(r.Warnings, "indexed_head_missing")
		return r, nil
	}

	r.Stale = current != indexedHead
	r.Fresh = !r.Stale
	if !r.Stale {
		return r, nil
	}

	changed, err := gitDiffNameOnly(srcRoot, indexedHead, current)
	if err != nil {
		r.Warnings = append(r.Warnings, "diff_unavailable: "+err.Error())
		return r, nil
	}
	r.ChangedFiles = changed
	return r, nil
}

func gitRevParseHead(srcRoot string) (string, error) {
	out, err := exec.Command("git", "-C", srcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDiffNameOnly(srcRoot, from, to string) ([]string, error) {
	// `git diff --name-only A B` returns the union of changes between
	// the two revisions. We list both directions (A..B) so renamed
	// files appear once.
	out, err := exec.Command("git", "-C", srcRoot, "diff", "--name-only", from, to).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}
