// Package prdoc parses PR descriptions and commit messages into chunks
// for the PR corpus index (NEW-3). Each PR produces 1-3 chunks:
//
//   - ChunkPRBackground  — the "Background" / "Context" section
//   - ChunkPRSolution    — the "Solution" / "Changes" section
//   - ChunkCommitMessage — one per commit (concatenated into one chunk)
//
// The parser reuses prregress.ExtractBackground for section splitting
// and expects the caller to have already fetched the PR metadata via
// the gh CLI.
package prdoc

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// PRMeta is the input from the gh CLI. Mirrors the subset of
// prregress.Meta that the parser needs, decoupled so we don't import
// the prregress package (which brings exec/os deps).
type PRMeta struct {
	Repo           string
	PRNumber       int
	Title          string
	Body           string
	CommitMessages []string
	ChangedFiles   []string
	MergedAt       time.Time
}

// Parse splits a PR's description and commit messages into chunks.
// The File field on each chunk is set to a synthetic path
// "pr/<repo>#<number>" so retrieval citations point back to the PR.
func Parse(meta PRMeta) []types.Chunk {
	if meta.Body == "" && len(meta.CommitMessages) == 0 {
		return nil
	}

	file := fmt.Sprintf("pr/%s#%d", meta.Repo, meta.PRNumber)
	now := meta.MergedAt.UTC().Format(time.RFC3339)
	var chunks []types.Chunk

	if meta.Body != "" {
		bg := extractBackground(meta.Body)
		sol := extractSolution(meta.Body)

		if bg != "" {
			chunks = append(chunks, types.Chunk{
				ID:            types.ChunkID(file, 1, 1, types.ContentSHA256(bg)),
				File:          file,
				StartLine:     1,
				EndLine:       1,
				Language:      "markdown",
				SymbolName:    meta.Title,
				SymbolKind:    types.KindDocSection,
				ChunkKind:     types.ChunkPRBackground,
				CommitHash:    now,
				ContentSHA256: types.ContentSHA256(bg),
				Text:          bg,
			})
		}

		if sol != "" {
			chunks = append(chunks, types.Chunk{
				ID:            types.ChunkID(file, 2, 2, types.ContentSHA256(sol)),
				File:          file,
				StartLine:     2,
				EndLine:       2,
				Language:      "markdown",
				SymbolName:    meta.Title,
				SymbolKind:    types.KindDocSection,
				ChunkKind:     types.ChunkPRSolution,
				CommitHash:    now,
				ContentSHA256: types.ContentSHA256(sol),
				Text:          sol,
			})
		}
	}

	if len(meta.CommitMessages) > 0 {
		joined := strings.Join(meta.CommitMessages, "\n\n---\n\n")
		chunks = append(chunks, types.Chunk{
			ID:            types.ChunkID(file, 3, 3, types.ContentSHA256(joined)),
			File:          file,
			StartLine:     3,
			EndLine:       3,
			Language:      "markdown",
			SymbolName:    meta.Title,
			SymbolKind:    types.KindDocSection,
			ChunkKind:     types.ChunkCommitMessage,
			CommitHash:    now,
			ContentSHA256: types.ContentSHA256(joined),
			Text:          joined,
		})
	}

	return chunks
}

var backgroundRE = regexp.MustCompile(`(?im)^\s*(#{1,6}\s+|\*\*)background(\s*\*\*)?\s*:?\s*$`)
var solutionRE = regexp.MustCompile(`(?im)^\s*(#{1,6}\s+|\*\*)(solution|changes|approach)(\s*\*\*)?\s*:?\s*$`)

func extractBackground(body string) string {
	return extractSection(body, backgroundRE)
}

func extractSolution(body string) string {
	return extractSection(body, solutionRE)
}

func extractSection(body string, headerRE *regexp.Regexp) string {
	loc := headerRE.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	rest := body[loc[1]:]
	stopRE := regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s+\S|\*\*[^*\n]+\*\*\s*:?\s*$)`)
	if stop := stopRE.FindStringIndex(rest); stop != nil {
		return strings.TrimSpace(rest[:stop[0]])
	}
	return strings.TrimSpace(rest)
}
