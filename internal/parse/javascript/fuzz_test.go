package javascript

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/fuzzcheck"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`function greet(name) { return name; }`))
	f.Add([]byte(`class Client { constructor() {} fetch(url) {} }`))
	f.Add([]byte(`const add = (a, b) => a + b;`))
	f.Add([]byte(``))
	f.Add([]byte(`{{{not javascript!!!`))

	p := New()
	f.Fuzz(func(t *testing.T, src []byte) {
		spans, err := p.Parse("fuzz.js", src)
		if err != nil {
			return
		}
		fuzzcheck.CheckSpans(t, spans)
	})
}
