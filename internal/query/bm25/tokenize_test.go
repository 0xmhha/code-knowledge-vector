package bm25

import (
	"reflect"
	"testing"
)

// Minimal smoke tests on the code-aware tokenizer — full coverage lives
// in CKG. These pin the contract CKV's Rerank depends on (joined form +
// sub-words, lower-cased, length<2 dropped, identifier separators kept).

func TestTokenize_CamelCase(t *testing.T) {
	got := Tokenize("parseFile")
	want := []string{"parsefile", "parse", "file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize(parseFile) = %v, want %v", got, want)
	}
}

func TestTokenize_DottedSymbol(t *testing.T) {
	got := Tokenize("pkg.Type.Method")
	// "Type" / "Method" / "pkg" survive (length >= 2). Joined forms are
	// the dot-split tokens; they have no sub-words to add.
	want := []string{"pkg", "type", "method"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize(pkg.Type.Method) = %v, want %v", got, want)
	}
}

func TestTokenize_DropsShortTokens(t *testing.T) {
	// Sub-words that fall below length 2 must be dropped.
	got := Tokenize("v1Beta2")
	for _, tok := range got {
		if len(tok) < 2 {
			t.Errorf("Tokenize produced length<2 token: %q (full=%v)", tok, got)
		}
	}
}

func TestTokenize_EmptyInputReturnsNil(t *testing.T) {
	if got := Tokenize(""); got != nil {
		t.Errorf("Tokenize(\"\") should return nil; got %v", got)
	}
}
