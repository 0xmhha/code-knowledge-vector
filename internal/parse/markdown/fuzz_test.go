package markdown

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/fuzzcheck"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`# Heading
Some content.

## Subheading
More content.
`))
	f.Add([]byte(`# ADR-001: Decision
**Status**: Accepted

## Context
We needed to decide.

## Decision
We chose X.
`))
	f.Add([]byte(``))
	f.Add([]byte(`########## deeply nested
no real structure here {{{`))

	p := New()
	f.Fuzz(func(t *testing.T, src []byte) {
		spans, err := p.Parse("fuzz.md", src)
		if err != nil {
			return
		}
		fuzzcheck.CheckSpans(t, spans)
	})
}
