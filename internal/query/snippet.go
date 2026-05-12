package query

import (
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// charsPerToken approximates a sub-word tokenizer for budget math.
// Same constant as internal/chunk; kept duplicated rather than shared
// so the read path stays independent of the write path.
const charsPerToken = 4

// signatureContextLines is how many lines after the signature we keep
// in the compressed (signature + N) density mode.
const signatureContextLines = 5

// DensityAdjust converts store hits into response Hits with snippets
// sized to fit budgetTokens. The algorithm:
//
//  1. Start with every hit at "full body" density.
//  2. If the total token count exceeds budget, downgrade hits one by
//     one from the lowest-ranked (worst score) upward, in this order:
//     full → signature+N → signature only.
//  3. Stop as soon as the running total fits, or every hit is at the
//     minimum.
//
// Citation is *not* counted against the budget — plan §4.3.
// Returns the response hits plus the final tokensUsed estimate.
func DensityAdjust(hits []types.Hit, budgetTokens int) (out []Hit, tokensUsed int) {
	// Start at full density; we'll downgrade below if needed.
	out = make([]Hit, len(hits))
	for i, h := range hits {
		out[i] = toResponseHit(h, h.Chunk.Text)
	}
	tokensUsed = estimateTokens(out)
	if budgetTokens <= 0 || tokensUsed <= budgetTokens {
		return out, tokensUsed
	}

	// Downgrade pass 1: full → signature+N
	for i := len(out) - 1; i >= 0 && tokensUsed > budgetTokens; i-- {
		out[i].Snippet = signatureWithContext(hits[i].Chunk.Text, signatureContextLines)
		tokensUsed = estimateTokens(out)
	}
	if tokensUsed <= budgetTokens {
		return out, tokensUsed
	}

	// Downgrade pass 2: signature+N → signature only
	for i := len(out) - 1; i >= 0 && tokensUsed > budgetTokens; i-- {
		out[i].Snippet = signatureOnly(hits[i].Chunk.Text)
		tokensUsed = estimateTokens(out)
	}
	return out, tokensUsed
}

func toResponseHit(h types.Hit, snippet string) Hit {
	c := h.Chunk
	return Hit{
		ChunkID:    c.ID,
		Citation:   c.Citation(),
		Snippet:    snippet,
		Score:      h.Score,
		Language:   c.Language,
		Symbol:     c.SymbolName,
		SymbolKind: c.SymbolKind,
		CKGNodeID:  c.CKGNodeID,
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
