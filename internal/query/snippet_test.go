package query

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func mkHit(id, text string, rank int, dist float64) types.Hit {
	return types.Hit{
		Chunk: types.Chunk{
			ID:        id,
			File:      "x.go",
			StartLine: 1,
			EndLine:   10,
			Language:  "go",
			Text:      text,
		},
		Score: types.HitScore{
			Normalized:     1 - dist/2,
			VectorDistance: dist,
			VectorRank:     rank,
		},
	}
}

func TestDensityFullWhenUnderBudget(t *testing.T) {
	hits := []types.Hit{
		mkHit("a", "func A() {\n  return 1\n}", 1, 0.2),
		mkHit("b", "func B() {\n  return 2\n}", 2, 0.3),
	}
	out, tokens := DensityAdjust(hits, 1000) // plenty of room
	if len(out) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(out))
	}
	if out[0].Snippet != hits[0].Chunk.Text {
		t.Errorf("expected full text under budget, got %q", out[0].Snippet)
	}
	if tokens <= 0 {
		t.Errorf("tokensUsed should be positive, got %d", tokens)
	}
}

func TestDensityDowngradesLowestRankedFirst(t *testing.T) {
	// Each hit's text is ~812 chars → ~203 tokens. Two fulls ≈ 406.
	long := strings.Repeat("// comment line\n", 50)
	hits := []types.Hit{
		mkHit("a", "func A() {\n"+long+"}", 1, 0.1),
		mkHit("b", "func B() {\n"+long+"}", 2, 0.2),
	}
	// Budget chosen so that downgrading just hit[1] to sig+5 fits, but
	// two fulls don't. Demonstrates proportional downgrade.
	out, _ := DensityAdjust(hits, 230)
	if len(out[1].Snippet) >= len(out[0].Snippet) {
		t.Errorf("expected hit[1] (rank 2) snippet shorter than hit[0]: lens %d vs %d",
			len(out[0].Snippet), len(out[1].Snippet))
	}
	if out[0].Snippet != hits[0].Chunk.Text {
		t.Errorf("hit[0] should remain at full body, got len %d (orig %d)",
			len(out[0].Snippet), len(hits[0].Chunk.Text))
	}
}

func TestDensityAllToMinimumOnTightBudget(t *testing.T) {
	long := strings.Repeat("// comment line\n", 50)
	hits := []types.Hit{
		mkHit("a", "func A() {\n"+long+"}", 1, 0.1),
		mkHit("b", "func B() {\n"+long+"}", 2, 0.2),
	}
	out, tokens := DensityAdjust(hits, 5) // unrealistically tight
	for _, h := range out {
		if !strings.HasPrefix(h.Snippet, "func ") {
			t.Errorf("expected signature-only on tight budget, got %q", h.Snippet)
		}
	}
	if tokens > 50 {
		t.Errorf("tokensUsed should be small after full collapse: %d", tokens)
	}
}

func TestSignatureOnlySkipsBlankLines(t *testing.T) {
	got := signatureOnly("\n\n// preamble comment\nfunc Foo() {}")
	if got != "// preamble comment" {
		t.Errorf("signatureOnly returned %q", got)
	}
}

func TestSignatureWithContext(t *testing.T) {
	text := "func Foo() {\n\tx := 1\n\ty := 2\n\tz := 3\n\treturn x + y + z\n}"
	got := signatureWithContext(text, 2)
	lines := strings.Split(got, "\n")
	if lines[0] != "func Foo() {" {
		t.Errorf("first line should be the signature, got %q", lines[0])
	}
	if len(lines) > 3 {
		// signature + 2 context lines = 3 lines max
		t.Errorf("expected ≤3 lines, got %d: %v", len(lines), lines)
	}
}
