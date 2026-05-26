// Package fuzzcheck provides shared invariant checks for parser fuzz tests.
// Each parser's FuzzParse calls CheckSpans after a successful parse to
// verify structural invariants that must hold for any output.
package fuzzcheck

import (
	"fmt"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse"
)

// CheckSpans verifies structural invariants on parser output.
// Calls t.Errorf on violations (does not abort — collects all).
func CheckSpans(t *testing.T, spans []parse.SymbolSpan) {
	t.Helper()
	for i, s := range spans {
		tag := fmt.Sprintf("span[%d] (%s %q)", i, s.Kind, s.Name)
		if s.StartLine < 1 {
			t.Errorf("%s: StartLine=%d, want >= 1", tag, s.StartLine)
		}
		if s.EndLine < s.StartLine {
			t.Errorf("%s: EndLine=%d < StartLine=%d", tag, s.EndLine, s.StartLine)
		}
		if s.Name == "" {
			t.Errorf("%s: empty Name", tag)
		}
		if s.Kind == "" {
			t.Errorf("%s: empty Kind", tag)
		}
		if s.Text == "" {
			t.Errorf("%s: empty Text", tag)
		}
	}
}
