package golang

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/fuzzcheck"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`package x
func Hello() {}
`))
	f.Add([]byte(`package x
type S struct{ v int }
func (s *S) Get() int { return s.v }
`))
	f.Add([]byte(`package x
type I interface { Do() }
`))
	f.Add([]byte(``))
	f.Add([]byte(`not valid go at all {{{{`))

	p := New()
	f.Fuzz(func(t *testing.T, src []byte) {
		spans, err := p.Parse("fuzz.go", src)
		if err != nil {
			return
		}
		fuzzcheck.CheckSpans(t, spans)
	})
}
