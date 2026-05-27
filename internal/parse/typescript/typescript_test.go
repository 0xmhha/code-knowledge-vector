package typescript

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParseExtractsTopLevelDecls(t *testing.T) {
	src := []byte(`// example
export function greet(name: string): string {
  return "hello, " + name;
}

export interface Server {
  listen(): Promise<void>;
}

export type Handler = (req: Request) => Promise<Response>;

export class Cache {
  private data = new Map<string, string>();

  set(key: string, value: string): void {
    this.data.set(key, value);
  }

  get(key: string): string | undefined {
    return this.data.get(key);
  }
}

export const noop = (): void => {};
`)
	spans, err := New().Parse("sample.ts", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]types.SymbolKind{
		"greet":     types.KindFunction,
		"Server":    types.KindInterface,
		"Handler":   types.KindType,
		"Cache":     types.KindStruct,
		"Cache.set": types.KindMethod,
		"Cache.get": types.KindMethod,
		"noop":      types.KindFunction,
	}
	got := map[string]types.SymbolKind{}
	for _, s := range spans {
		got[s.Name] = s.Kind
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("missing/wrong kind for %s: got %s, want %s", name, got[name], kind)
		}
	}
}

func TestParseTSXFileSucceeds(t *testing.T) {
	// JSX syntax in a .tsx file must not crash the parser.
	src := []byte(`export function Hello(props: {name: string}) {
  return <div>Hello, {props.name}</div>;
}
`)
	spans, err := New().Parse("hello.tsx", src)
	if err != nil {
		t.Fatalf("Parse .tsx: %v", err)
	}
	if len(spans) == 0 {
		t.Fatal("expected at least one span (Hello)")
	}
	if spans[0].Name != "Hello" || spans[0].Kind != types.KindFunction {
		t.Errorf("first span: got %+v, want Hello Function", spans[0])
	}
}

func TestParseRecordsLineRanges(t *testing.T) {
	src := []byte(`function a() {}

function b() {
  return 1;
}
`)
	spans, _ := New().Parse("x.ts", src)
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d: %+v", len(spans), spans)
	}
	if spans[0].StartLine != 1 || spans[0].EndLine != 1 {
		t.Errorf("a: got lines %d-%d, want 1-1", spans[0].StartLine, spans[0].EndLine)
	}
	if spans[1].StartLine != 3 || spans[1].EndLine != 5 {
		t.Errorf("b: got lines %d-%d, want 3-5", spans[1].StartLine, spans[1].EndLine)
	}
}
