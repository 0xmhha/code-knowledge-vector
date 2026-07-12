package build

import (
	"context"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/chunk"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// stubPrefixer returns a fixed prefix for chunks whose Text is in want, and ""
// (a cache miss / generation failure) for everything else, so a single stub
// exercises both the LLM-prefix branch and the rule-based fallback.
type stubPrefixer struct{ prefixOf map[string]string }

func (s stubPrefixer) Prefix(_ context.Context, c types.Chunk) string {
	return s.prefixOf[c.Text]
}

func TestResolveEmbedTextFn_NoPrefixer(t *testing.T) {
	c := types.Chunk{Text: "body", File: "x.go", Language: "go", SymbolName: "F", SymbolKind: "Function"}

	// disablePrefix=false → rule-based prefix.
	if got, want := resolveEmbedTextFn(context.Background(), false, nil)(c), chunk.BuildEmbedText(c); got != want {
		t.Fatalf("rule-based fn = %q, want %q", got, want)
	}
	// disablePrefix=true → raw text.
	if got := resolveEmbedTextFn(context.Background(), true, nil)(c); got != "body" {
		t.Fatalf("raw fn = %q, want %q", got, "body")
	}
}

func TestResolveEmbedTextFn_LLMPrefixPrependsAndFallsBack(t *testing.T) {
	hit := types.Chunk{Text: "func F() {}", File: "x.go", Language: "go", SymbolName: "F", SymbolKind: "Function"}
	miss := types.Chunk{Text: "func G() {}", File: "y.go", Language: "go", SymbolName: "G", SymbolKind: "Function"}
	pf := stubPrefixer{prefixOf: map[string]string{hit.Text: "Runs F."}}

	fn := resolveEmbedTextFn(context.Background(), false, pf)

	// Cache/generation hit → "<LLM prose>\n<rule-based prefix + raw>": the LLM
	// prose is layered on top of the rule-based signal, which the PoC found
	// beats LLM-prose-alone (the rule-based prefix carries exact symbol/file
	// tokens a paraphrase would dilute).
	got := fn(hit)
	if want := "Runs F.\n" + chunk.BuildEmbedText(hit); got != want {
		t.Fatalf("hit embed text = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "Runs F.\n") {
		t.Fatalf("LLM prose should lead the embed text: %q", got)
	}
	if !strings.Contains(got, "language: go") {
		t.Fatalf("combined form must retain the rule-based prefix: %q", got)
	}

	// Miss (empty prefix) → rule-based prefix, so no chunk goes unprefixed.
	if got, want := fn(miss), chunk.BuildEmbedText(miss); got != want {
		t.Fatalf("miss embed text = %q, want rule-based %q", got, want)
	}
}

func TestResolveLLMPrefixer_EmptyModelIsNil(t *testing.T) {
	if p := resolveLLMPrefixer("", t.TempDir()); p != nil {
		t.Fatalf("empty model should yield a nil prefixer, got %v", p)
	}
}

func TestResolveLLMPrefixer_BuildsCachedPrefixer(t *testing.T) {
	p := resolveLLMPrefixer("llama3", t.TempDir())
	if p == nil {
		t.Fatal("non-empty model should yield a prefixer")
	}
}
