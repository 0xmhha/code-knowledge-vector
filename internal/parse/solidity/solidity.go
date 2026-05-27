// Package solidity parses .sol via the vendored tree-sitter-solidity
// grammar (see internal/parse/solidity/binding). Emits symbol-level
// spans for the retrieval layer:
//
//   - contract/library/interface → KindContract / KindInterface
//   - function (inside contract)  → KindMethod ("Contract.fn")
//   - free function (rare, 0.7.4+) → KindFunction
//   - constructor                  → KindMethod ("Contract.constructor")
//   - modifier                     → KindModifier
//   - event                        → KindEvent
//   - struct                       → KindStruct
//   - enum                         → KindType
package solidity

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"

	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/solidity/binding"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Parser implements parse.Parser for .sol files.
type Parser struct{}

// New returns a stateless Solidity parser.
func New() *Parser { return &Parser{} }

func (p *Parser) Language() string { return "solidity" }

// Parse extracts contract-scoped + top-level declarations. Walks named
// children of the source_unit; recurses into contract/library/interface
// bodies so methods get qualified names.
func (p *Parser) Parse(file string, src []byte) ([]cparse.SymbolSpan, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(binding.GetLanguage()); err != nil {
		return nil, fmt.Errorf("solidity: SetLanguage: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("solidity: parser returned nil tree for %s", file)
	}
	defer tree.Close()

	var spans []cparse.SymbolSpan
	root := tree.RootNode()
	collectSpans(root, src, "", &spans)
	return spans, nil
}

// collectSpans descends named children. parentContract is non-empty
// inside a contract/library/interface body so methods get qualified
// (e.g. "Token.transfer").
func collectSpans(n *sitter.Node, src []byte, parentContract string, out *[]cparse.SymbolSpan) {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "contract_declaration", "library_declaration":
			name := nameFromField(c, "name", src)
			if sp, ok := spanFor(c, src, name, types.KindContract); ok {
				*out = append(*out, sp)
			}
			collectSpans(c, src, name, out)

		case "interface_declaration":
			name := nameFromField(c, "name", src)
			if sp, ok := spanFor(c, src, name, types.KindInterface); ok {
				*out = append(*out, sp)
			}
			collectSpans(c, src, name, out)

		case "function_definition":
			name := nameFromField(c, "name", src)
			kind := types.KindFunction
			if parentContract != "" {
				kind = types.KindMethod
				name = parentContract + "." + name
			}
			if sp, ok := spanFor(c, src, name, kind); ok {
				*out = append(*out, sp)
			}

		case "constructor_definition":
			name := "constructor"
			if parentContract != "" {
				name = parentContract + ".constructor"
			}
			if sp, ok := spanFor(c, src, name, types.KindMethod); ok {
				*out = append(*out, sp)
			}

		case "modifier_definition":
			name := nameFromField(c, "name", src)
			if parentContract != "" && name != "" {
				name = parentContract + "." + name
			}
			if sp, ok := spanFor(c, src, name, types.KindModifier); ok {
				*out = append(*out, sp)
			}

		case "event_definition":
			name := nameFromField(c, "name", src)
			if parentContract != "" && name != "" {
				name = parentContract + "." + name
			}
			if sp, ok := spanFor(c, src, name, types.KindEvent); ok {
				*out = append(*out, sp)
			}

		case "struct_declaration":
			name := nameFromField(c, "name", src)
			if parentContract != "" && name != "" {
				name = parentContract + "." + name
			}
			if sp, ok := spanFor(c, src, name, types.KindStruct); ok {
				*out = append(*out, sp)
			}

		case "enum_declaration":
			name := nameFromField(c, "name", src)
			if parentContract != "" && name != "" {
				name = parentContract + "." + name
			}
			if sp, ok := spanFor(c, src, name, types.KindType); ok {
				*out = append(*out, sp)
			}

		default:
			// Recurse into other named children — pragma_directive,
			// import_directive, using_directive don't yield spans but
			// may wrap inner nodes we want.
			collectSpans(c, src, parentContract, out)
		}
	}
}

func spanFor(n *sitter.Node, src []byte, name string, kind types.SymbolKind) (cparse.SymbolSpan, bool) {
	if name == "" {
		return cparse.SymbolSpan{}, false
	}
	startB := n.StartByte()
	endB := n.EndByte()
	if int(endB) > len(src) || startB > endB {
		return cparse.SymbolSpan{}, false
	}
	return cparse.SymbolSpan{
		Name:      name,
		Kind:      kind,
		StartLine: int(n.StartPosition().Row) + 1,
		EndLine:   int(n.EndPosition().Row) + 1,
		Text:      string(src[startB:endB]),
	}, true
}

func nameFromField(n *sitter.Node, fieldName string, src []byte) string {
	field := n.ChildByFieldName(fieldName)
	if field == nil {
		return ""
	}
	a := field.StartByte()
	b := field.EndByte()
	if int(b) > len(src) || a > b {
		return ""
	}
	return string(src[a:b])
}
