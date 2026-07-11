package query

import (
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// charsPerToken approximates a sub-word tokenizer for budget math.
// Same constant as internal/chunk; kept duplicated rather than shared
// so the read path stays independent of the write path.
const charsPerToken = 4

// DefaultSignatureContextLines is how many lines after the signature
// we keep in the SignatureWithContext density tier when the caller
// doesn't override Options.SignatureContextLines.
const DefaultSignatureContextLines = 5

// DensityTier names the three snippet shapes the engine can emit.
//
//   - DensityFull          — chunk's complete Text. Default when budget
//     room allows.
//   - DensitySignature5    — first non-blank line (the signature) plus
//     the next N non-blank lines. Middle compression tier; useful for
//     "what does this function start by doing".
//   - DensitySignatureOnly — first non-blank line only (just the
//     signature). Minimum useful representation; caller resolves the
//     body via Citation if they need more.
//
// Reported on every Hit (Hit.Density) so consumers can render
// progressively or count how aggressively the budget squeezed them.
type DensityTier string

const (
	DensityFull          DensityTier = "full"
	DensitySignature5    DensityTier = "signature+N"
	DensitySignatureOnly DensityTier = "signature_only"
)

// DensityAdjust converts store hits into response Hits with snippets
// sized to fit budgetTokens. ctxLines tunes the SignatureWithContext
// tier (0 → DefaultSignatureContextLines). cap caps the maximum tier
// the engine will emit ("" → DensityFull, no cap).
//
// Algorithm:
//
//  1. Start every hit at `cap` (or DensityFull when no cap).
//  2. If the total token count exceeds budget, downgrade hits one by
//     one from the lowest-ranked (worst score) upward:
//     full → signature+N → signature only.
//  3. Stop as soon as the running total fits, or every hit is at the
//     minimum.
//
// Each Hit's Density field reports the final tier it landed in.
// Citation is *not* counted against the budget.
// Returns the response hits plus the final tokensUsed estimate.
func DensityAdjust(hits []types.Hit, budgetTokens int) (out []Hit, tokensUsed int) {
	return DensityAdjustWith(hits, budgetTokens, DensityFull, DefaultSignatureContextLines)
}

// DensityAdjustWith is the configurable variant exposing the density
// ceiling and context-line knob. DensityAdjust is the convenience
// wrapper that uses documented defaults.
func DensityAdjustWith(hits []types.Hit, budgetTokens int, cap DensityTier, ctxLines int) (out []Hit, tokensUsed int) {
	if ctxLines <= 0 {
		ctxLines = DefaultSignatureContextLines
	}
	// Resolve cap. Empty / unknown means no cap (full).
	startTier := cap
	if startTier == "" {
		startTier = DensityFull
	}

	out = make([]Hit, len(hits))
	for i, h := range hits {
		out[i] = toResponseHit(h, renderAt(h.Chunk.Text, startTier, ctxLines))
		out[i].Density = startTier
	}
	tokensUsed = estimateTokens(out)
	if budgetTokens <= 0 || tokensUsed <= budgetTokens {
		return out, tokensUsed
	}

	// Downgrade pass 1: full → signature+N (skip if cap already ≤
	// SignatureWithContext).
	if startTier == DensityFull {
		for i := len(out) - 1; i >= 0 && tokensUsed > budgetTokens; i-- {
			if out[i].Density != DensityFull {
				continue
			}
			out[i].Snippet = signatureWithContext(hits[i].Chunk.Text, ctxLines)
			out[i].Density = DensitySignature5
			tokensUsed = estimateTokens(out)
		}
		if tokensUsed <= budgetTokens {
			return out, tokensUsed
		}
	}

	// Downgrade pass 2: signature+N → signature only.
	for i := len(out) - 1; i >= 0 && tokensUsed > budgetTokens; i-- {
		if out[i].Density == DensitySignatureOnly {
			continue
		}
		out[i].Snippet = signatureOnly(hits[i].Chunk.Text)
		out[i].Density = DensitySignatureOnly
		tokensUsed = estimateTokens(out)
	}
	return out, tokensUsed
}

// renderAt picks the snippet body for a given tier. Centralizes the
// per-tier rendering so the downgrade loop can use the same helpers.
func renderAt(text string, tier DensityTier, ctxLines int) string {
	switch tier {
	case DensitySignatureOnly:
		return signatureOnly(text)
	case DensitySignature5:
		return signatureWithContext(text, ctxLines)
	default:
		return text
	}
}

func toResponseHit(h types.Hit, snippet string) Hit {
	c := h.Chunk
	return Hit{
		ChunkID:       c.ID,
		Citation:      c.Citation(),
		Snippet:       snippet,
		Score:         h.Score,
		Language:      c.Language,
		IsTest:        c.IsTest,
		Symbol:        c.SymbolName,
		SymbolKind:    c.SymbolKind,
		ChunkKind:     c.ChunkKind,
		CanonicalID:   c.CanonicalID,
		Category:      c.Category,
		Guidance:      c.Guidance,
		StaleCitation: h.StaleCitation,
	}
}

// signatureOnly returns the first non-blank line of text. For Go
// `func Foo(...) error {` that's exactly the signature; the caller
// can still resolve the body via Citation.
func signatureOnly(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// signatureWithContext keeps the signature line plus the next n
// non-blank lines. Useful as the middle density tier: enough to see
// "what does this function start by doing" without the full body.
func signatureWithContext(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return ""
	}
	kept := make([]string, 0, n+1)
	added := 0
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) != "" {
			kept = append(kept, line)
			if i > 0 {
				added++
			}
			if added >= n {
				break
			}
		}
	}
	return strings.Join(kept, "\n")
}

func estimateTokens(hits []Hit) int {
	var chars int
	for _, h := range hits {
		chars += len(h.Snippet)
	}
	// Round up: a snippet of charsPerToken bytes shouldn't underestimate
	// to 0 tokens.
	return (chars + charsPerToken - 1) / charsPerToken
}
