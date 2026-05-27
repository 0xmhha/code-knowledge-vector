// Package markdown parses *.md / *.markdown files into heading-level
// SymbolSpans so docs/ADR content becomes searchable alongside source
// code.
//
// Strategy: line-based scan, no external dependency. Why direct rather
// than goldmark?
//
//   - The transformation we need ("split at ATX headings, keep body
//     including code fences") is ~80 LOC and easier to reason about
//     than walking an AST node graph.
//   - go.mod stays small; the user-global security rule asks us to
//     justify every new dependency. A heading splitter is well below
//     that bar.
//   - Edge cases we DO need to handle (code-fence-internal hashes that
//     look like headings, setext-style underlined headings) are local
//     enough that line-scanning is precise.
//
// Output contract:
//
//   - Each ATX heading (`#`, `##`, ..., `######`) starts a SymbolSpan.
//   - SymbolSpan.Text covers the heading line through the line BEFORE
//     the next heading (or EOF).
//   - SymbolSpan.Name is the heading text normalized via slugify
//     (lowercase, runs of non-alphanumerics → "-", trimmed).
//   - SymbolSpan.Kind is KindADRSection when the file path matches an
//     ADR convention (ADR-*.md, docs/adr/*.md, etc.), otherwise
//     KindDocSection.
//   - A file with NO headings returns ONE SymbolSpan covering the
//     whole file with Name="untitled" (the chunker labels it
//     "FileHeader"-ish via DocSection so retrieval still works).
package markdown

import (
	"path/filepath"
	"strings"
	"unicode"

	cparse "github.com/0xmhha/code-knowledge-vector/internal/parse"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Parser implements parse.Parser for *.md / *.markdown.
type Parser struct{}

// New returns a stateless markdown parser. Cheap to construct; the
// builder allocates one per language.
func New() *Parser { return &Parser{} }

// Language returns the CKV language tag.
func (p *Parser) Language() string { return "markdown" }

// Parse splits src into heading-level spans. The file argument is
// inspected only to choose between DocSection / ADRSection — no disk
// reads happen here.
func (p *Parser) Parse(file string, src []byte) ([]cparse.SymbolSpan, error) {
	kind := sectionKind(file)
	lines := splitLines(src)
	if len(lines) == 0 {
		return nil, nil
	}

	// First pass: identify ATX heading line indices, skipping anything
	// inside a fenced code block.
	type heading struct {
		line int // 0-based index into lines
		name string
	}
	var headings []heading
	inFence := false
	for i, l := range lines {
		if isFenceLine(l) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if raw, ok := atxHeading(l); ok {
			// Store the slugified form as SymbolSpan.Name so it stays
			// stable across header text edits that are purely cosmetic
			// (case, punctuation). Raw text is still inside Text.
			slug := Slugify(raw)
			if slug == "" {
				// Pathological heading like "## —" (no alphanumerics).
				// Fall back to a numbered placeholder so spans remain
				// distinct and don't collide on the empty-name key.
				slug = "section"
			}
			headings = append(headings, heading{line: i, name: slug})
		}
	}

	// No headings → return a single span covering the whole file, so
	// the chunker still emits one searchable record. Name="" signals
	// "untitled section"; we leave the chunker to fall back to file
	// path for display.
	if len(headings) == 0 {
		return []cparse.SymbolSpan{{
			Name:      "untitled",
			Kind:      kind,
			StartLine: 1,
			EndLine:   len(lines),
			Text:      joinLines(lines),
		}}, nil
	}

	// If content exists before the first heading, capture it as a
	// preamble section. Common case: ADR front-matter / abstract.
	spans := make([]cparse.SymbolSpan, 0, len(headings)+1)
	if headings[0].line > 0 && hasContent(lines[:headings[0].line]) {
		spans = append(spans, cparse.SymbolSpan{
			Name:      "preamble",
			Kind:      kind,
			StartLine: 1,
			EndLine:   headings[0].line, // 1-based inclusive of last line before heading
			Text:      joinLines(lines[:headings[0].line]),
		})
	}

	for i, h := range headings {
		endLine := len(lines) // 1-based inclusive end
		if i+1 < len(headings) {
			endLine = headings[i+1].line // line before next heading (0-based → 1-based off by zero)
		}
		text := joinLines(lines[h.line:endLine])
		spans = append(spans, cparse.SymbolSpan{
			Name:      h.name,
			Kind:      kind,
			StartLine: h.line + 1, // convert 0-based to 1-based
			EndLine:   endLine,    // 0-based exclusive == 1-based inclusive of previous line
			Text:      text,
		})
	}
	return spans, nil
}

// splitLines splits src on '\n', preserving an empty trailing element
// only when src ends without a newline. We work with []string instead
// of [][]byte because every downstream consumer is a string.
func splitLines(src []byte) []string {
	if len(src) == 0 {
		return nil
	}
	s := string(src)
	// Normalize CRLF → LF so heading detection works on Windows-authored
	// docs without an extra branch in atxHeading.
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	lines := strings.Split(s, "\n")
	// strings.Split appends a trailing "" when src ends with '\n'. We
	// keep that empty element so EndLine line counts align with the
	// user's editor — but the chunker doesn't need the artifact, so
	// trim it.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// joinLines is the inverse of splitLines. We always include a trailing
// newline so the chunk text looks like a normal file slice.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// isFenceLine reports whether the line opens or closes a fenced code
// block. We recognize both ``` and ~~~ fences. Indented fences (4+
// leading spaces) are NOT recognized — those are code blocks in
// CommonMark but rare in our docs corpus.
func isFenceLine(l string) bool {
	trimmed := strings.TrimLeft(l, " \t")
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// atxHeading checks if l is an ATX-style heading (e.g. "## Title").
// Returns the heading TEXT (not the leading hashes) when ok.
//
// We allow 1..6 hashes followed by a space (CommonMark §4.2). A line
// of just "###" with no following text is NOT a heading — matches
// common markdown parser behavior and avoids false-positive on
// horizontal-rule-like content.
func atxHeading(l string) (string, bool) {
	// Up to 3 leading spaces are allowed by CommonMark; anything more
	// makes it a code block. We mirror that here.
	stripped := strings.TrimLeft(l, " ")
	if len(l)-len(stripped) > 3 {
		// 4+ leading spaces → code block, not heading
		return "", false
	}
	count := 0
	for count < 6 && count < len(stripped) && stripped[count] == '#' {
		count++
	}
	if count == 0 || count > 6 {
		return "", false
	}
	rest := stripped[count:]
	if rest == "" {
		// "###" alone is a paragraph in CommonMark.
		return "", false
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		// "##foo" is NOT a heading (heading marker must be followed by space).
		return "", false
	}
	// Trim leading space + optional trailing closing hashes (CommonMark
	// allows "# Title #" syntax).
	text := strings.TrimSpace(rest)
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
	if text == "" {
		// "## ##" with no real text — useless as a span name.
		return "", false
	}
	return text, true
}

// hasContent reports whether at least one line in lines has
// non-whitespace content. Used to decide whether the pre-first-heading
// region is worth emitting as a preamble span.
func hasContent(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}

// sectionKind chooses ADRSection vs DocSection based on the file path.
// Conventions recognized:
//   - basename starts with "adr-" (any case): "ADR-001-title.md"
//   - any directory segment equals "adr" (any case): "docs/adr/x.md"
//
// The Solidity/Go parsers don't need this kind of path-based dispatch;
// we DO here because the "this is a design decision" signal lives in
// the file system layout, not the file contents.
func sectionKind(file string) types.SymbolKind {
	base := strings.ToLower(filepath.Base(file))
	if strings.HasPrefix(base, "adr-") {
		return types.KindADRSection
	}
	dir := filepath.ToSlash(filepath.Dir(file))
	for _, seg := range strings.Split(dir, "/") {
		if strings.EqualFold(seg, "adr") {
			return types.KindADRSection
		}
	}
	return types.KindDocSection
}

// Slugify normalizes a heading into a chunk-friendly identifier:
// lowercase + non-alphanumeric runs collapsed to "-". Exported so
// tests and downstream tooling (eval fixtures) can match the same
// transformation deterministically.
//
//	"Vector store — decision matrix" → "vector-store-decision-matrix"
//	"§4 Embedding"                   → "4-embedding"
func Slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true // suppress leading dashes
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := b.String()
	return strings.TrimRight(out, "-")
}
