// Package invariant extracts policy-bearing statements from Go source
// files in a three-tier confidence ladder:
//
//	Tier 1: existing markers — // CRITICAL, // IMPORTANT, // WARNING,
//	         // Deprecated:
//	Tier 2: new convention markers — // INVARIANT:, // CONSENSUS:,
//	         // SECURITY:
//	Tier 3: heuristics — panic(...) / fmt.Errorf("...") with policy
//	         keywords (must, cannot, consensus, validator, byzantine).
//
// The extractor parses Go via the standard go/parser package; it
// returns each detected invariant as a types.Chunk with
// ChunkKind = ChunkInvariant. The caller pairs invariants back to
// source chunks by file + line overlap.
//
// False-positive control:
//   - Tier 3 is suppressed in *_test.go files.
//   - A per-file cap (MaxTier3PerFile, default 10) prevents a single
//     test fixture or builder from saturating the index.
package invariant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// MaxTier3PerFile caps heuristic invariants per file. Bounded to
// prevent test fixtures or boilerplate panic chains from flooding
// the index. Override via Options for special-case files.
const MaxTier3PerFile = 10

// Options tune extraction behavior.
type Options struct {
	// MaxTier3PerFile bounds heuristic detections. 0 → MaxTier3PerFile.
	MaxTier3PerFile int

	// SkipTier3InTests omits heuristic detection in *_test.go.
	// Defaults true; set false only for fixture inspection.
	SkipTier3InTests bool
}

// Result is one extracted invariant.
type Result struct {
	Tier      types.InvariantTier
	Marker    string // "CRITICAL", "INVARIANT", "panic", "Errorf", ...
	StartLine int    // 1-based, inclusive
	EndLine   int    // 1-based, inclusive
	Text      string // the comment / call source text
}

// existingMarkers map their canonical name onto Tier 1.
var existingMarkers = map[string]bool{
	"CRITICAL":   true,
	"IMPORTANT":  true,
	"WARNING":    true,
	"Deprecated": true, // godoc convention, written as "Deprecated:"
}

// newMarkers map their canonical name onto Tier 2.
var newMarkers = map[string]bool{
	"INVARIANT": true,
	"CONSENSUS": true,
	"SECURITY":  true,
}

// markerRE matches a comment leader like "CRITICAL", "INVARIANT:".
// We strip the leading "//" + spaces before applying it.
var markerRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]+)\s*[:\-]`)

// heuristicKeywords are the policy words that promote a panic /
// Errorf string to Tier 3. Conservative on purpose — any addition
// inflates false-positive rate.
var heuristicKeywords = []string{
	"must", "cannot", "consensus", "validator", "byzantine",
	"invariant", "forbidden", "illegal",
}

// Extract returns every invariant found in src. relPath is the
// repo-relative path of the file (used only for the IsTest decision).
//
// On parse error returns (nil, err). Partial extraction is not done:
// silent partial results would corrupt downstream tier statistics.
func Extract(relPath string, src []byte, opts Options) ([]Result, error) {
	if opts.MaxTier3PerFile == 0 {
		opts.MaxTier3PerFile = MaxTier3PerFile
	}
	// Tier 3 (heuristic on panic / Errorf) is suppressed in *_test.go
	// when opts.SkipTier3InTests is set, because test fixtures emit
	// far too many noise hits otherwise (assertion panics, mock errors).
	// Non-test files keep Tier 3 detection so production policy panics
	// surface to the agent.
	skipTier3 := opts.SkipTier3InTests && strings.HasSuffix(relPath, "_test.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relPath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var out []Result

	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if r, ok := classifyComment(c, fset); ok {
				out = append(out, r)
			}
		}
	}

	if !skipTier3 {
		tier3Count := 0
		ast.Inspect(file, func(n ast.Node) bool {
			if tier3Count >= opts.MaxTier3PerFile {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			r, ok := classifyCall(call, file, fset)
			if !ok {
				return true
			}
			out = append(out, r)
			tier3Count++
			// Don't descend into the matched call's args: e.g. when we
			// matched panic(fmt.Errorf("...")) we already attributed the
			// inner fmt.Errorf to this hit; visiting it again would
			// double-count.
			return false
		})
	}

	return out, nil
}

// classifyComment attempts to extract a Tier 1 or Tier 2 marker from
// a single comment node. Returns ok=false when nothing matched.
func classifyComment(c *ast.Comment, fset *token.FileSet) (Result, bool) {
	text := commentBody(c.Text)
	if text == "" {
		return Result{}, false
	}
	m := markerRE.FindStringSubmatch(text)
	if m == nil {
		return Result{}, false
	}
	name := m[1]
	tier := types.InvariantTier(0)
	switch {
	case existingMarkers[name]:
		tier = types.InvariantTierExistingMarker
	case newMarkers[name]:
		tier = types.InvariantTierNewMarker
	default:
		return Result{}, false
	}
	pos := fset.Position(c.Pos())
	end := fset.Position(c.End())
	return Result{
		Tier:      tier,
		Marker:    name,
		StartLine: pos.Line,
		EndLine:   end.Line,
		Text:      strings.TrimSpace(text),
	}, true
}

// classifyCall flags panic(...) / fmt.Errorf(...) / errors.New(...)
// calls that contain a policy keyword. Four detection paths:
//
//	(a) Direct string literal     : panic("validator must hold")
//	(b) fmt.Sprintf / fmt.Errorf  : panic(fmt.Sprintf("validator %s", v))
//	(c) Identifier + nearby doc   : panic(err) — only when a comment
//	                                within 3 lines above carries a
//	                                policy keyword (covers the dominant
//	                                Go pattern of wrapping an error value)
//	(d) (else)                    : not flagged
//
// (c) is conservative on purpose: panic(err) without an accompanying
// comment is too common in production code to flag as an invariant.
// The "nearby doc" rule converts the docstring into the chunk text so
// the agent reads the explanation, not just the bare panic line.
func classifyCall(call *ast.CallExpr, file *ast.File, fset *token.FileSet) (Result, bool) {
	name := callableName(call.Fun)
	switch name {
	case "panic", "Errorf", "errors.New", "fmt.Errorf":
	default:
		return Result{}, false
	}
	if len(call.Args) == 0 {
		return Result{}, false
	}
	arg0 := call.Args[0]

	// (a) Direct string literal.
	if lit := stringLiteral(arg0); lit != "" {
		if containsPolicyWord(lit) {
			return makeTier3(name, call, fset, lit), true
		}
		return Result{}, false
	}

	// (b) Nested fmt.Sprintf / fmt.Errorf wrapping a literal.
	if inner, ok := arg0.(*ast.CallExpr); ok {
		innerName := callableName(inner.Fun)
		if (innerName == "fmt.Sprintf" || innerName == "fmt.Errorf") && len(inner.Args) > 0 {
			if lit := stringLiteral(inner.Args[0]); lit != "" && containsPolicyWord(lit) {
				return makeTier3(name+"+"+innerName, call, fset, lit), true
			}
		}
		return Result{}, false
	}

	// (c) Identifier (panic(err) etc.) — require nearby policy comment.
	// Only applies to bare panic: errors.New/fmt.Errorf with non-literal
	// first arg is too unusual to flag.
	if name == "panic" {
		if _, ok := arg0.(*ast.Ident); ok {
			if text, ok := nearbyPolicyComment(call, file, fset); ok {
				return makeTier3("panic(ident)", call, fset, text), true
			}
		}
	}
	return Result{}, false
}

// makeTier3 is the small constructor shared by every Tier 3 detection
// path so they record the same line range / fields.
func makeTier3(marker string, call *ast.CallExpr, fset *token.FileSet, text string) Result {
	pos := fset.Position(call.Pos())
	end := fset.Position(call.End())
	return Result{
		Tier:      types.InvariantTierHeuristic,
		Marker:    marker,
		StartLine: pos.Line,
		EndLine:   end.Line,
		Text:      text,
	}
}

// nearbyPolicyComment returns (text, true) when a comment within 3
// lines above call contains a policy keyword. The returned text is the
// concatenated comment body — it is what the invariant chunk stores
// in lieu of the bare panic source, giving the agent the explanation
// the developer left behind.
func nearbyPolicyComment(call *ast.CallExpr, file *ast.File, fset *token.FileSet) (string, bool) {
	callLine := fset.Position(call.Pos()).Line
	for _, cg := range file.Comments {
		endLine := fset.Position(cg.End()).Line
		if endLine >= callLine {
			continue // comment is on / below the panic
		}
		if callLine-endLine > 3 {
			continue // too far above
		}
		var b strings.Builder
		for _, c := range cg.List {
			body := commentBody(c.Text)
			if body == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(body)
		}
		joined := b.String()
		if containsPolicyWord(joined) {
			return joined, true
		}
	}
	return "", false
}

// commentBody strips "//" / "/* */" wrappers and leading whitespace.
func commentBody(raw string) string {
	switch {
	case strings.HasPrefix(raw, "//"):
		return strings.TrimSpace(raw[2:])
	case strings.HasPrefix(raw, "/*"):
		return strings.TrimSpace(strings.TrimSuffix(raw[2:], "*/"))
	}
	return raw
}

// callableName resolves the call expression to either an identifier
// or a dotted "pkg.Fn" form. Returns "" for unknown shapes (interface
// method calls etc).
func callableName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok {
			return pkg.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	}
	return ""
}

// stringLiteral returns the unquoted value of a *ast.BasicLit when it
// is a string. Empty for anything else.
func stringLiteral(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s := lit.Value
	if len(s) < 2 {
		return ""
	}
	if s[0] == '"' || s[0] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// containsPolicyWord runs a case-insensitive substring check for the
// policy keyword list. Cheap; called once per literal.
func containsPolicyWord(s string) bool {
	low := strings.ToLower(s)
	for _, k := range heuristicKeywords {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// EmitChunks converts a slice of Results into ChunkInvariant chunks
// plus a list of back-pointer InvariantRefs the caller should staple
// onto matching source chunks. file is the chunk's File field; commit
// is propagated for citation parity.
//
// Each invariant chunk's ID is deterministic over (file, line range,
// SHA256 of text), so re-running the extractor on unchanged source
// produces the same chunk IDs (Upsert behaves correctly).
func EmitChunks(file, commit string, results []Result) ([]types.Chunk, []types.InvariantRef) {
	if len(results) == 0 {
		return nil, nil
	}
	chunks := make([]types.Chunk, 0, len(results))
	refs := make([]types.InvariantRef, 0, len(results))
	for _, r := range results {
		text := r.Text
		sha := types.ContentSHA256(text)
		id := types.ChunkID(file, r.StartLine, r.EndLine, sha)
		chunks = append(chunks, types.Chunk{
			ID:            id,
			File:          file,
			StartLine:     r.StartLine,
			EndLine:       r.EndLine,
			Language:      "go",
			ChunkKind:     types.ChunkInvariant,
			SymbolName:    r.Marker,
			SymbolKind:    "Invariant",
			CommitHash:    commit,
			ContentSHA256: sha,
			Text:          text,
		})
		refs = append(refs, types.InvariantRef{
			ChunkID: id,
			Tier:    r.Tier,
			Marker:  r.Marker,
		})
	}
	return chunks, refs
}

// AttachRefs decorates each source chunk with InvariantRefs whose line
// range overlaps the chunk's [StartLine, EndLine]. Mutates chunks in
// place. The chunk slice and refs slice come from the same file; chunk
// IDs in refs must already be present in the emitted invariant chunks.
//
// All-line invariants (Marker == "CRITICAL" at top of file, etc.) that
// fall outside every source chunk's span attach to no one — they are
// still indexed as ChunkInvariant on their own row.
func AttachRefs(chunks []types.Chunk, results []Result, refs []types.InvariantRef) {
	if len(chunks) == 0 || len(refs) == 0 {
		return
	}
	for i := range chunks {
		c := &chunks[i]
		for j, r := range results {
			if r.StartLine >= c.StartLine && r.EndLine <= c.EndLine {
				c.Invariants = append(c.Invariants, refs[j])
			}
		}
	}
}
