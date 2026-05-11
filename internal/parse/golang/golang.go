// Package golang parses Go source via the stdlib go/parser+go/ast. This
// is the same idiom CKG uses — for Go specifically, stdlib is more
// accurate than tree-sitter (full type information available; resolves
// generics; canonical line/column numbers via go/token).
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Parser is the Go-language SymbolSpan extractor.
type Parser struct{}

// New constructs a stateless parser. Parsers are cheap; instantiating
// one per file is fine.
func New() *Parser { return &Parser{} }

func (p *Parser) Language() string { return "go" }

// Parse extracts top-level func/method/type declarations. Nested
// closures inside functions are NOT lifted — they ride along with the
// enclosing function's chunk, which is the right granularity for
// retrieval (a closure rarely makes sense on its own).
func (p *Parser) Parse(file string, src []byte) ([]cparse.SymbolSpan, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}

	var spans []cparse.SymbolSpan
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			spans = append(spans, p.funcSpan(fset, src, d))
		case *ast.GenDecl:
			spans = append(spans, p.genSpans(fset, src, d)...)
		}
	}
	return spans, nil
}

func (p *Parser) funcSpan(fset *token.FileSet, src []byte, fn *ast.FuncDecl) cparse.SymbolSpan {
	kind := types.KindFunction
	name := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = types.KindMethod
		if recvName := receiverTypeName(fn.Recv.List[0]); recvName != "" {
			name = recvName + "." + fn.Name.Name
		}
	}
	startPos := fset.Position(fn.Pos())
	endPos := fset.Position(fn.End())
	return cparse.SymbolSpan{
		Name:      name,
		Kind:      kind,
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
		Text:      sliceText(src, fn.Pos(), fn.End(), fset),
	}
}

func (p *Parser) genSpans(fset *token.FileSet, src []byte, gen *ast.GenDecl) []cparse.SymbolSpan {
	// We only emit spans for TypeSpec. Imports/constants/vars are
	// covered later by the file_header fallback (chunker), not here.
	if gen.Tok != token.TYPE {
		return nil
	}
	var out []cparse.SymbolSpan
	for _, sp := range gen.Specs {
		ts, ok := sp.(*ast.TypeSpec)
		if !ok {
			continue
		}
		kind := types.KindType
		switch ts.Type.(type) {
		case *ast.StructType:
			kind = types.KindStruct
		case *ast.InterfaceType:
			kind = types.KindInterface
		}
		startPos := fset.Position(ts.Pos())
		endPos := fset.Position(ts.End())
		out = append(out, cparse.SymbolSpan{
			Name:      ts.Name.Name,
			Kind:      kind,
			StartLine: startPos.Line,
			EndLine:   endPos.Line,
			Text:      sliceText(src, ts.Pos(), ts.End(), fset),
		})
	}
	return out
}

// receiverTypeName turns the recv list of a method into the bare type
// name. Handles pointer (*T) and generic (T[U]) receivers — both common
// in modern Go.
func receiverTypeName(field *ast.Field) string {
	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
		if idx, ok := t.X.(*ast.IndexExpr); ok {
			if id, ok := idx.X.(*ast.Ident); ok {
				return id.Name
			}
		}
	case *ast.IndexExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// sliceText returns the source bytes between start and end positions as
// a string. We use the *token.FileSet to translate to absolute offsets
// because token.Pos is opaque without it.
func sliceText(src []byte, start, end token.Pos, fset *token.FileSet) string {
	a := fset.Position(start).Offset
	b := fset.Position(end).Offset
	if a < 0 || b > len(src) || a > b {
		return ""
	}
	return string(src[a:b])
}
