// Package binding wraps tree-sitter-solidity (vendored from
// github.com/JoranHonig/tree-sitter-solidity v1.2.13, MIT-licensed —
// see ./LICENSE) into a *sitter.Language for go-tree-sitter.
//
// Upstream ships only C / Node / Python / Rust / Swift bindings, so we
// vendor parser.c + the tree_sitter header into this package and let
// CGO compile them alongside our Go code. CKG follows the same
// pattern (internal/parse/solidity/binding) — both projects vendor
// independently rather than sharing a Go dep, matching the
// architectural rule that CKV does NOT import CKG.
package binding

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}
// #include "tree_sitter/parser.h"
// const TSLanguage *tree_sitter_solidity();
import "C"

import (
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// GetLanguage returns the Solidity *sitter.Language. Safe to call
// many times — go-tree-sitter wraps the same C pointer each time.
func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_solidity())
	return sitter.NewLanguage(ptr)
}
