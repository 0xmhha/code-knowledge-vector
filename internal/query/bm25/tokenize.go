package bm25

import (
	"strings"
	"unicode"
)

// Tokenize converts an arbitrary string into BM25 tokens with code-aware
// splitting. The output preserves the joined identifier (lowercased) AND
// its sub-words so a query for either form scores. Concretely:
//
//   - "parseFile"          → ["parsefile", "parse", "file"]
//   - "HTTPServer"         → ["httpserver", "http", "server"]
//   - "read_file"          → ["read_file", "read", "file"]
//   - "pkg.Type.Method"    → ["pkg", "type", "method"]
//   - "json-rpc/v1"        → ["json", "rpc", "v1"]
//
// All tokens are lowercased; tokens of length < 2 are dropped (they add
// noise without helping rank). Caller-supplied tokens are not deduped —
// repetition is meaningful to BM25's TF term.
func Tokenize(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 8)
	for _, raw := range splitOnSeparators(s) {
		if raw == "" {
			continue
		}
		lower := strings.ToLower(raw)
		if len(lower) >= 2 {
			out = append(out, lower)
		}
		for _, sub := range splitCamelOrSnake(raw) {
			sl := strings.ToLower(sub)
			if len(sl) >= 2 && sl != lower {
				out = append(out, sl)
			}
		}
	}
	return out
}

// splitOnSeparators splits s on whitespace, dot, slash, and structural
// punctuation. Underscore and dash are NOT separators here — they are
// part of identifiers and handled by splitCamelOrSnake so the joined
// form (e.g. "read_file") survives as a token.
func splitOnSeparators(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r',
			'.', '/', '\\', '#', ':', ';', ',',
			'(', ')', '[', ']', '{', '}', '<', '>',
			'"', '\'', '`', '!', '?', '=', '+', '*', '|', '&', '^', '~':
			return true
		}
		return false
	})
}

// splitCamelOrSnake decomposes a single identifier on camelCase, snake_case,
// kebab-case, and digit boundaries. Returns the constituent words in order.
// Examples:
//
//	"parseFile"   → ["parse", "File"]
//	"HTTPServer"  → ["HTTP", "Server"]
//	"URLParser"   → ["URL", "Parser"]
//	"read_file"   → ["read", "file"]
//	"json-rpc"    → ["json", "rpc"]
//	"v1Beta2"     → ["v", "1", "Beta", "2"]
func splitCamelOrSnake(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	out := []string{}
	start := 0

	emit := func(end int) {
		if end > start {
			out = append(out, string(runes[start:end]))
		}
	}

	for i := 1; i < len(runes); i++ {
		c := runes[i]
		prev := runes[i-1]

		// Underscore / dash separator: emit the segment before, skip the
		// separator. The post-statement (i++) advances past it on the
		// next loop iteration.
		if c == '_' || c == '-' {
			emit(i)
			start = i + 1
			continue
		}

		// camelCase boundary 1: lower → upper. "parseFile" splits at F.
		if unicode.IsUpper(c) && unicode.IsLower(prev) {
			emit(i)
			start = i
			continue
		}

		// camelCase boundary 2: ALL-CAPS run followed by Capital+lower.
		// "URLParser" splits between L and P. Detect when prev is upper,
		// c is upper, and i+1 is lower — c starts the new word.
		if unicode.IsUpper(c) && unicode.IsUpper(prev) &&
			i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			emit(i)
			start = i
			continue
		}

		// Letter ↔ digit boundary. "v1" → ["v", "1"].
		if unicode.IsDigit(c) && unicode.IsLetter(prev) {
			emit(i)
			start = i
			continue
		}
		if unicode.IsLetter(c) && unicode.IsDigit(prev) {
			emit(i)
			start = i
			continue
		}
	}
	emit(len(runes))
	return out
}
