// Package parse extracts symbol-level spans (functions, methods, types)
// from source files so the chunker can build embeddable chunks.
//
// Language strategy (matches CKG's parse package):
//   - Go: go/parser + go/ast (stdlib, no CGO)
//   - TypeScript / JavaScript: tree-sitter
//   - Solidity: tree-sitter
//
// Each parser turns one file into []SymbolSpan. The chunker then
// produces Chunks (with optional long-function splits) from those spans.
package parse

import "github.com/0xmhha/code-knowledge-vector/pkg/types"

// SymbolSpan is one indexable region of a source file. Line numbers
// are 1-based and inclusive on both ends.
type SymbolSpan struct {
	Name      string           // qualified name when known (e.g. "Server.handleEdges")
	Kind      types.SymbolKind // KindFunction, KindMethod, ...
	StartLine int
	EndLine   int
	Text      string // raw source for this span (signature + body)
}

// Parser is the contract every per-language parser fulfills.
type Parser interface {
	// Language returns the CKV language tag this parser handles
	// ("go" | "typescript" | "solidity" | "markdown").
	Language() string
	// Parse takes the full source text and returns the symbol spans
	// found within. The file argument is informational (used in
	// errors); the parser does not read from disk.
	Parse(file string, src []byte) ([]SymbolSpan, error)
}
