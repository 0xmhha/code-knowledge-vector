package types

import "testing"

func TestIsTestPath_Go(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"foo_test.go", true},
		{"internal/build/builder_test.go", true},
		{"main.go", false},
		{"internal/build/builder.go", false},
		{"testdata/sample/server.go", false}, // path contains "test" but not in name
		{"_test.go", true},                   // edge: just _test.go
	}
	for _, c := range cases {
		if got := IsTestPath(c.path, "go"); got != c.want {
			t.Errorf("IsTestPath(%q, go) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsTestPath_TypeScript(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"app.test.ts", true},
		{"app.spec.ts", true},
		{"app.test.tsx", true},
		{"app.spec.tsx", true},
		{"app.ts", false},
		{"app.tsx", false},
		{"src/components/Button.test.ts", true},
		{"src/components/Button.tsx", false},
	}
	for _, c := range cases {
		if got := IsTestPath(c.path, "typescript"); got != c.want {
			t.Errorf("IsTestPath(%q, typescript) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsTestPath_Solidity(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"contracts/Token.sol", false},
		{"contracts/Token.t.sol", true},
		{"test/Token.sol", true},
		{"tests/integration/Token.sol", true},
		{"src/test/helper.sol", true}, // 'test' segment anywhere
		{"src/Token.sol", false},
	}
	for _, c := range cases {
		if got := IsTestPath(c.path, "solidity"); got != c.want {
			t.Errorf("IsTestPath(%q, solidity) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsTestPath_JavaScript(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"app.test.js", true},
		{"app.spec.jsx", true},
		{"app.js", false},
	}
	for _, c := range cases {
		if got := IsTestPath(c.path, "javascript"); got != c.want {
			t.Errorf("IsTestPath(%q, javascript) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsTestPath_UnknownLanguageIsAlwaysFalse(t *testing.T) {
	// Defensive: any language not in the switch returns false, even if
	// the path looks test-y. The chunker should never call us with a
	// blank language tag, but the chunker isn't the only caller.
	if IsTestPath("foo_test.go", "") {
		t.Error("empty language should not match")
	}
	if IsTestPath("foo_test.go", "rust") {
		t.Error("unsupported language should not match")
	}
}

func TestIsTestPath_EmptyPath(t *testing.T) {
	if IsTestPath("", "go") {
		t.Error("empty path should return false")
	}
}
