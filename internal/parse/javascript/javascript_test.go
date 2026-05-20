package javascript

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParseExtractsTopLevelDecls(t *testing.T) {
	// Plain JS — no type annotations, exercising the same declaration
	// kinds the TS adapter handles. The delegated parser should treat
	// all of these the same way as the .ts equivalent.
	src := []byte(`// util module
export function greet(name) {
  return "hello, " + name;
}

export class Cache {
  constructor() { this.data = new Map(); }

  set(key, value) {
    this.data.set(key, value);
  }

  get(key) {
    return this.data.get(key);
  }
}

const noop = () => undefined;
`)
	spans, err := New().Parse("util.js", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]types.SymbolKind{
		"greet":     types.KindFunction,
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

func TestParseJSXFileSucceeds(t *testing.T) {
	// JSX in a .jsx file routes through the TSX grammar; the parser
	// must not crash and should still surface the function declaration.
	src := []byte(`import React from 'react';

export function Hello({ name }) {
  return <h1>Hi, {name}</h1>;
}
`)
	spans, err := New().Parse("hello.jsx", src)
	if err != nil {
		t.Fatalf("Parse .jsx: %v", err)
	}
	if len(spans) == 0 {
		t.Fatal("expected at least one span (Hello)")
	}
	found := false
	for _, s := range spans {
		if s.Name == "Hello" && s.Kind == types.KindFunction {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Hello function span not found in %+v", spans)
	}
}

func TestParseMJSAndCJSExtensions(t *testing.T) {
	// .mjs and .cjs share the JS grammar; verify both produce spans.
	for _, ext := range []string{".mjs", ".cjs"} {
		src := []byte(`export function start(port) { return port; }`)
		spans, err := New().Parse("server"+ext, src)
		if err != nil {
			t.Fatalf("Parse %s: %v", ext, err)
		}
		if len(spans) != 1 || spans[0].Name != "start" {
			t.Errorf("%s: expected start span, got %+v", ext, spans)
		}
	}
}

func TestLanguageTag(t *testing.T) {
	if got := New().Language(); got != "javascript" {
		t.Errorf("Language() = %q, want %q", got, "javascript")
	}
}
