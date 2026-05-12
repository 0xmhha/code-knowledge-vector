// Package typescript parses .ts and .tsx source via tree-sitter. We
// extract symbol-level spans (function, method, class, interface,
// type alias, enum) — the same shape go/parser produces for Go in
// internal/parse/golang.
//
// We deliberately do NOT match CKG's tree-sitter query DSL — CKV
// only needs spans, not full graph extraction, so a recursive descent
// over named children is simpler and easier to keep in head.
package typescript

import (
	"fmt"
	"path/filepath"
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsbind "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Parser implements parse.Parser for .ts and .tsx.
type Parser struct{}

// New returns a stateless TS parser. Tree-sitter parsers themselves
// are not safe to share across goroutines, but we instantiate a fresh
// sitter.Parser per Parse() call — see comment inside Parse.
func New() *Parser { return &Parser{} }

func (p *Parser) Language() string { return "typescript" }

// Parse extracts top-level + class-nested symbol spans. ext-driven
// grammar selection: .tsx uses the TSX grammar so JSX-aware syntax
// parses without errors.
func (p *Parser) Parse(file string, src []byte) ([]cparse.SymbolSpan, error) {
	// One sitter.Parser per call: tree-sitter's Go binding is not
	// goroutine-safe at the parser level, and Parse() itself is cheap
	// (μs to allocate).
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(languageForExt(filepath.Ext(file))); err != nil {
		return nil, fmt.Errorf("typescript: SetLanguage: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("typescript: parser returned nil tree for %s", file)
	}
	defer tree.Close()

	var spans []cparse.SymbolSpan
	root := tree.RootNode()
	collectSpans(root, src, "", &spans)
	return spans, nil
}

// collectSpans walks named children of n, emitting a SymbolSpan for
// each declaration we care about. parentClass non-empty inside a
// class body so methods can be reported as "ClassName.method".
func collectSpans(n *sitter.Node, src []byte, parentClass string, out *[]cparse.SymbolSpan) {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "function_declaration":
			if sp, ok := spanFor(c, src, nameFromField(c, "name", src), types.KindFunction); ok {
				*out = append(*out, sp)
			}
		case "method_definition":
			name := nameFromField(c, "name", src)
			if parentClass != "" {
				name = parentClass + "." + name
			}
			if sp, ok := spanFor(c, src, name, types.KindMethod); ok {
				*out = append(*out, sp)
			}
		case "class_declaration":
			className := nameFromField(c, "name", src)
			if sp, ok := spanFor(c, src, className, types.KindStruct); ok {
				// We map TS class → KindStruct (closest CKV kind). The
				// pkg/types enum currently lacks a dedicated "Class";
				// adding one is a no-cost extension when needed.
				*out = append(*out, sp)
			}
			// Recurse to pick methods up with class-qualified name.
			collectSpans(c, src, className, out)
		case "interface_declaration":
			if sp, ok := spanFor(c, src, nameFromField(c, "name", src), types.KindInterface); ok {
				*out = append(*out, sp)
			}
		case "type_alias_declaration":
			if sp, ok := spanFor(c, src, nameFromField(c, "name", src), types.KindType); ok {
				*out = append(*out, sp)
			}
		case "enum_declaration":
			// No dedicated KindEnum yet; enum → KindType keeps the
			// retrieval surface simple.
			if sp, ok := spanFor(c, src, nameFromField(c, "name", src), types.KindType); ok {
				*out = append(*out, sp)
			}
		case "lexical_declaration", "variable_declaration":
			// const foo = (...) => {...} pattern. Walk the declarator
			// list and emit if the initializer is an arrow_function.
			collectArrowFunctions(c, src, out)
		default:
			// Recurse into other named children (export_statement,
			// namespace bodies, etc.) so nested decls aren't missed.
			collectSpans(c, src, parentClass, out)
		}
	}
}

// collectArrowFunctions handles `const foo = () => { ... }` which
// tree-sitter parses as `lexical_declaration → variable_declarator
// → arrow_function`. We treat these as top-level functions so they
// are retrievable like a function_declaration.
func collectArrowFunctions(declNode *sitter.Node, src []byte, out *[]cparse.SymbolSpan) {
	for i := uint(0); i < declNode.NamedChildCount(); i++ {
		decl := declNode.NamedChild(i)
		if decl == nil || decl.Kind() != "variable_declarator" {
			continue
		}
		valueNode := decl.ChildByFieldName("value")
		if valueNode == nil || valueNode.Kind() != "arrow_function" {
			continue
		}
		name := nameFromField(decl, "name", src)
		if name == "" {
			continue
		}
		// Span is the whole declaration so the snippet shows `const x = ...`.
		if sp, ok := spanFor(declNode, src, name, types.KindFunction); ok {
			*out = append(*out, sp)
		}
	}
}

// spanFor turns a sitter.Node into a SymbolSpan. Returns ok=false
// when name is empty (anonymous expressions we can't usefully index).
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

// nameFromField fetches the named-child under fieldName (e.g. "name")
// and returns its UTF-8 source slice. Empty string if absent.
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

// languageForExt picks TS vs TSX grammar. tree-sitter-typescript ships
// both; TSX accepts JSX syntax while TS rejects it.
func languageForExt(ext string) *sitter.Language {
	switch ext {
	case ".tsx":
		return sitter.NewLanguage(unsafe.Pointer(tsbind.LanguageTSX()))
	default:
		return sitter.NewLanguage(unsafe.Pointer(tsbind.LanguageTypescript()))
	}
}
