// Package javascript parses .js / .jsx / .mjs / .cjs source files.
//
// Implementation note: TypeScript is a syntactic superset of JavaScript,
// and tree-sitter-typescript ships both the TS and TSX grammars. We
// therefore delegate parsing to internal/parse/typescript so plain JS
// goes through the TS grammar (which happily accepts type-free input)
// and JSX goes through the TSX grammar — same span set, no extra
// dependency, no grammar drift between the two adapters.
//
// The package exists as its own seam so discover/builder can tag JS
// files with Language="javascript" and a future native tree-sitter
// JavaScript grammar swap touches one file.
package javascript

import (
	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/typescript"
)

// Parser implements parse.Parser for JavaScript family extensions.
type Parser struct {
	inner *typescript.Parser
}

// New returns a stateless JS parser. Cheap to construct.
func New() *Parser {
	return &Parser{inner: typescript.New()}
}

// Language returns the tag stored on each Chunk produced from a JS
// file. The discover layer assigns the same tag based on extension;
// this method is what parse.Parser callers ask when they need the
// language name as a string.
func (p *Parser) Language() string { return "javascript" }

// Parse delegates to the TS parser. The TS adapter inspects the file
// extension to pick TS vs TSX grammar, and we already taught it to
// route .jsx through TSX — so .js / .mjs / .cjs end up on the TS
// grammar and .jsx on the TSX grammar, both with the same span
// extraction logic.
func (p *Parser) Parse(file string, src []byte) ([]cparse.SymbolSpan, error) {
	return p.inner.Parse(file, src)
}
