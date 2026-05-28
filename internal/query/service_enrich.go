package query

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// CommitSummary is one git log entry attached to a hit.
type CommitSummary struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// EnrichService attaches git history to each hit. Implements
// Phase 3 spec Step 3 (Metadata Enrichment). Per-file results are
// cached within a single Run to avoid duplicate git log calls when
// multiple hits share a file.
type EnrichService struct{}

// Run executes `git log` for each unique file in hits and attaches
// the results. srcRoot is the repo root. maxCommits is the per-file
// history depth (5 matches the Phase 3 spec).
func (s *EnrichService) Run(ctx context.Context, hits []Hit, srcRoot string, maxCommits int) []Hit {
	if len(hits) == 0 || srcRoot == "" {
		return hits
	}
	if maxCommits <= 0 {
		maxCommits = 5
	}

	cache := map[string][]CommitSummary{}
	for i, h := range hits {
		file := h.Citation.File
		if file == "" {
			continue
		}
		summaries, ok := cache[file]
		if !ok {
			summaries = gitLog(ctx, srcRoot, file, maxCommits)
			cache[file] = summaries
		}
		hits[i].GitHistory = summaries
	}
	return hits
}

// RunContext attaches git history to sc.FinalHits and sc.FinalExamples
// when Options.EnableMetadataEnrichment is set.
func (s *EnrichService) RunContext(ctx context.Context, sc *SearchContext, srcRoot string) {
	if !sc.Options.EnableMetadataEnrichment {
		return
	}
	max := sc.Options.MaxHistoryCommits
	sc.FinalHits = s.Run(ctx, sc.FinalHits, srcRoot, max)
	sc.FinalExamples = s.Run(ctx, sc.FinalExamples, srcRoot, max)
}

// gitLog runs `git log -n <N> --pretty=format:... -- <file>` and parses
// the output into CommitSummary records. Returns nil on any error
// (enrichment is best-effort; query should not fail when git is absent).
func gitLog(ctx context.Context, srcRoot, file string, maxCommits int) []CommitSummary {
	const sep = "|"
	format := fmt.Sprintf("--pretty=format:%%h%s%%an%s%%ad%s%%s", sep, sep, sep)
	args := []string{
		"-C", srcRoot,
		"log",
		fmt.Sprintf("-n%d", maxCommits),
		"--date=short",
		format,
		"--",
		file,
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var summaries []CommitSummary
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, sep, 4)
		if len(parts) != 4 {
			continue
		}
		summaries = append(summaries, CommitSummary{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Subject: parts[3],
		})
	}
	return summaries
}

// Compile-time check that types.Citation still works with our File access pattern.
var _ = types.Citation{}.File
