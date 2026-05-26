package typescript

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/fuzzcheck"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`export function greet(name: string): string { return name; }`))
	f.Add([]byte(`class Server { listen(port: number): void {} }`))
	f.Add([]byte(`interface Handler { handle(): void; }`))
	f.Add([]byte(`const arrow = (x: number) => x * 2;`))
	f.Add([]byte(``))
	f.Add([]byte(`}{}{}{not typescript`))

	p := New()
	f.Fuzz(func(t *testing.T, src []byte) {
		spans, err := p.Parse("fuzz.ts", src)
		if err != nil {
			return
		}
		fuzzcheck.CheckSpans(t, spans)
	})
}
