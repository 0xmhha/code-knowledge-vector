package prregress

import (
	"sort"
	"strings"
)

// TruthFiles returns the canonical set of files the PR actually
// changed — used as the ground truth for file-set F1 scoring.
//
// The list comes straight from gh CLI's `files` field, which mirrors
// what GitHub UI shows on the "Files changed" tab. Pure additions and
// pure deletions both count. Test files count too: if a PR ships a
// fix without test coverage that's a real signal an agent's plan can
// miss.
//
// Why a separate function instead of just reading Meta.Files: future
// follow-ups will filter (e.g. exclude generated files, normalize
// renames). Centralizing the projection keeps the runner ignorant of
// those decisions.
func TruthFiles(m Meta) []string {
	out := make([]string, 0, len(m.Files))
	for _, f := range m.Files {
		if p := strings.TrimSpace(f.Path); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
