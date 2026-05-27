package chunk

import (
	"fmt"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// BuildEmbedText returns the embedder input for c with a rule-based
// contextual prefix prepended. Cheap, deterministic, no LLM call.
//
// Format: one descriptive line + blank line + raw chunk text.
//
// File header chunks:
//
//	"language: go. file: server.go. file header.\n\n<text>"
//
// Doc section chunks (markdown):
//
//	"language: markdown. file: docs/x.md. section: why-sqlite-vec.\n\n<text>"
//
// Symbol chunks:
//
//	"language: go. file: server.go. symbol: Server.Listen (Method).\n\n<text>"
//
// Design notes:
//   - The prefix is regenerated on every build/reindex. It is NOT
//     persisted in c.Text — chunk IDs hash the raw text only, so
//     re-running with the prefix on/off does not invalidate IDs.
//   - The query intent is NOT prefixed. The asymmetry is intentional
//     (Anthropic Contextual Retrieval pattern): prefixed chunks become
//     easier to retrieve from natural-language queries because the
//     embedding now carries location/type signal in addition to body.
//   - We pick natural-language phrasing over bracketed tags so the
//     embedder (typically trained on English prose) parses it cleanly.
func BuildEmbedText(c types.Chunk) string {
	switch c.ChunkKind {
	case types.ChunkFileHeader:
		return fmt.Sprintf("language: %s. file: %s. file header.\n\n%s",
			languageLabel(c.Language), c.File, c.Text)
	case types.ChunkDoc:
		// SymbolName for doc sections is the heading slug (e.g.
		// "why-sqlite-vec"). Kind ("DocSection"/"ADRSection") is
		// useful signal — keep it.
		return fmt.Sprintf("language: %s. file: %s. section: %s (%s).\n\n%s",
			languageLabel(c.Language), c.File, c.SymbolName, c.SymbolKind, c.Text)
	case types.ChunkPRBackground:
		return fmt.Sprintf("pull request background. file: %s.\n\n%s", c.File, c.Text)
	case types.ChunkPRSolution:
		return fmt.Sprintf("pull request solution. file: %s.\n\n%s", c.File, c.Text)
	case types.ChunkCommitMessage:
		return fmt.Sprintf("commit message. file: %s.\n\n%s", c.File, c.Text)
	default:
		// Symbol chunk. SymbolName is qualified when the parser knows
		// the receiver (e.g. "Server.Listen"); SymbolKind narrows the
		// shape (Method, Function, Struct, Interface, Modifier, ...).
		// Either field may be empty for partial parses; render what we
		// have without crashing.
		switch {
		case c.SymbolName != "" && c.SymbolKind != "":
			return fmt.Sprintf("language: %s. file: %s. symbol: %s (%s).\n\n%s",
				languageLabel(c.Language), c.File, c.SymbolName, c.SymbolKind, c.Text)
		case c.SymbolName != "":
			return fmt.Sprintf("language: %s. file: %s. symbol: %s.\n\n%s",
				languageLabel(c.Language), c.File, c.SymbolName, c.Text)
		default:
			return fmt.Sprintf("language: %s. file: %s.\n\n%s",
				languageLabel(c.Language), c.File, c.Text)
		}
	}
}

// RawEmbedText returns c.Text unchanged. Pass it as embedTextFn to
// disable contextual prefixing — useful for A/B measurement and for
// the existing baseline test corpus.
func RawEmbedText(c types.Chunk) string {
	return c.Text
}

// languageLabel humanizes the language tag for the prefix. The tags
// CKV stores ("go", "typescript", "solidity", "markdown", "javascript")
// are already lowercase; we keep them. A future tag like "ts" would
// expand to "typescript" here, but today the mapping is identity.
func languageLabel(lang string) string {
	if lang == "" {
		return "unknown"
	}
	return lang
}
