package query

import (
	"os"
	"path/filepath"
	"strings"
)

// HallucinationResult is the verdict for one hit-vs-source comparison.
// Verified=true means the snippet's content actually exists at the
// claimed file location; Verified=false flags a hallucination signal
// with a short Reason so log readers can debug without re-running.
//
// Phase 3 (D5) of docs/evaluation-design-2026-05-22.md. The operational
// definition: a hit is *not* a hallucination if its snippet appears as
// a (whitespace-normalized) substring of the cited file's line range.
// This handles every density tier — DensitySignatureOnly is naturally a
// substring of a longer body, DensityFull is the full body itself.
type HallucinationResult struct {
	Verified bool   `json:"verified"`
	Reason   string `json:"reason,omitempty"` // empty when Verified=true
	// File-level diagnostics for false cases. Populated only when
	// Verified=false so the JSON payload stays compact for the common
	// case.
	ExpectedFile string `json:"expected_file,omitempty"`
}

// VerifyHit reports whether h.Snippet aligns with the source file at
// srcRoot/<h.Citation.File>. Three failure modes are distinguished so
// log readers can route the response: "file_missing" (citation points
// nowhere), "out_of_range" (line numbers exceed file length), and
// "snippet_not_found" (content disagrees — the actual hallucination).
//
// Whitespace normalization: tabs / spaces / newlines collapse to a
// single space and leading / trailing whitespace is trimmed. This
// matches how a reader would compare the snippet against the file
// (formatting differences are not hallucinations).
//
// Empty srcRoot returns Verified=false with reason="no_src_root" —
// the caller asked for verification but didn't provide a tree to
// verify against.
func VerifyHit(h Hit, srcRoot string) HallucinationResult {
	if srcRoot == "" {
		return HallucinationResult{Verified: false, Reason: "no_src_root"}
	}
	full := filepath.Join(srcRoot, h.Citation.File)
	data, err := os.ReadFile(full)
	if err != nil {
		return HallucinationResult{
			Verified:     false,
			Reason:       "file_missing",
			ExpectedFile: full,
		}
	}
	lines := strings.Split(string(data), "\n")
	if h.Citation.StartLine < 1 || h.Citation.EndLine > len(lines) || h.Citation.EndLine < h.Citation.StartLine {
		return HallucinationResult{
			Verified:     false,
			Reason:       "out_of_range",
			ExpectedFile: full,
		}
	}
	fileSlice := strings.Join(lines[h.Citation.StartLine-1:h.Citation.EndLine], "\n")
	if !strings.Contains(stripBlanks(fileSlice), stripBlanks(h.Snippet)) {
		return HallucinationResult{
			Verified:     false,
			Reason:       "snippet_not_found",
			ExpectedFile: full,
		}
	}
	return HallucinationResult{Verified: true}
}

// VerifyResponse runs VerifyHit across both Hits and Examples of the
// response and aggregates the verdicts. Returns the per-hit results
// (in response order: hits first, then examples) plus a count of
// non-verified hits. Convenient for eval pipelines that just want
// a hallucination rate.
func VerifyResponse(resp *Response, srcRoot string) (verdicts []HallucinationResult, hallucinated int) {
	if resp == nil {
		return nil, 0
	}
	total := len(resp.Hits) + len(resp.Examples)
	verdicts = make([]HallucinationResult, 0, total)
	for _, h := range resp.Hits {
		v := VerifyHit(h, srcRoot)
		if !v.Verified {
			hallucinated++
		}
		verdicts = append(verdicts, v)
	}
	for _, h := range resp.Examples {
		v := VerifyHit(h, srcRoot)
		if !v.Verified {
			hallucinated++
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, hallucinated
}

// stripBlanks collapses every whitespace run to a single space and
// trims leading / trailing whitespace. Used to compare snippets
// against file slices without flagging tab-vs-space or trailing-
// whitespace cosmetics as hallucinations.
func stripBlanks(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := true // start-of-string treated as whitespace so leading runs collapse
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		b.WriteRune(r)
		inSpace = false
	}
	return strings.TrimRight(b.String(), " ")
}

