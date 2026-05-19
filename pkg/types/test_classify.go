package types

import (
	"path/filepath"
	"strings"
)

// IsTestPath classifies a source-relative path as a test file based on
// the conventional patterns of its language. It is intentionally a pure
// function so the chunker can call it without depending on language
// parsers, and so callers can re-classify when reindexing without a
// schema migration.
//
// Conventions covered:
//
//	Go         "*_test.go"
//	TypeScript "*.test.ts(x)", "*.spec.ts(x)"
//	JavaScript "*.test.js(x)", "*.spec.js(x)" (for future JS parser)
//	Solidity   "*.t.sol" (Foundry), any segment named "test" or "tests"
//
// path is forward-slash, repo-relative. lang is the language tag the
// discover/parse layer assigned ("go", "typescript", "solidity", or "").
//
// Why per-language convention: testing frameworks pick filename rules
// that drift between ecosystems. JUnit Java would be `Test*.java`,
// Python pytest is `test_*.py`. Adding a language => add one branch
// here. Keep the function short and explicit (P5 — readable beats
// clever) so the contributor adding the next language can see at a
// glance what to extend.
func IsTestPath(path, lang string) bool {
	if path == "" {
		return false
	}
	base := filepath.Base(path)
	switch lang {
	case "go":
		return strings.HasSuffix(base, "_test.go")
	case "typescript":
		return hasAnyOfSuffix(base, ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx")
	case "javascript":
		return hasAnyOfSuffix(base, ".test.js", ".spec.js", ".test.jsx", ".spec.jsx")
	case "solidity":
		if strings.HasSuffix(base, ".t.sol") {
			return true
		}
		// Foundry / Truffle convention: anything inside a "test" or
		// "tests" directory is a test file regardless of name.
		for _, seg := range strings.Split(path, "/") {
			if seg == "test" || seg == "tests" {
				return true
			}
		}
		return false
	}
	return false
}

func hasAnyOfSuffix(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}
