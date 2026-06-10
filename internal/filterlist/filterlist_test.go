package filterlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAllow_NilFilter verifies that a nil filter accepts everything.
func TestAllow_NilFilter(t *testing.T) {
	var f *FilterList
	paths := []string{
		"consensus/wbft/validator/validator.go",
		"core/txpool/legacypool/legacypool.go",
		"contracts/Token.sol",
		"README.md",
	}
	for _, p := range paths {
		if !f.Allow(p) {
			t.Errorf("nil filter: Allow(%q) = false, want true", p)
		}
	}
}

// TestAllow_IncludeOnly verifies that only matching paths are accepted
// when Include is set and Exclude is empty.
func TestAllow_IncludeOnly(t *testing.T) {
	f := &FilterList{
		Include: []string{"consensus/wbft/**"},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"consensus/wbft/validator/validator.go", true},
		{"consensus/wbft/core/core.go", true},
		{"consensus/wbft/core/handler_test.go", true},
		{"core/txpool/legacypool/legacypool.go", false},
		{"contracts/Token.sol", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		got := f.Allow(tc.path)
		if got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestAllow_ExcludeTrumpsInclude verifies that exclude patterns take
// priority over include patterns.
func TestAllow_ExcludeTrumpsInclude(t *testing.T) {
	f := &FilterList{
		Include: []string{"consensus/wbft/**"},
		Exclude: []string{"**/*_test.go"},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"consensus/wbft/validator/validator.go", true},
		{"consensus/wbft/core/core_test.go", false},     // excluded by **/*_test.go
		{"consensus/wbft/core/handler_test.go", false},  // excluded
		{"core/txpool/legacypool/legacypool.go", false}, // not included
	}
	for _, tc := range cases {
		got := f.Allow(tc.path)
		if got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestAllow_EmptyInclude verifies that an empty include list with a
// non-empty exclude list accepts everything that is not excluded.
func TestAllow_EmptyInclude(t *testing.T) {
	f := &FilterList{
		Exclude: []string{"vendor/**", "**/*.pb.go"},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"core/txpool/legacypool/legacypool.go", true},
		{"vendor/github.com/spf13/cobra/cobra.go", false},
		{"internal/proto/gen/foo.pb.go", false},
		{"README.md", true},
		{"contracts/Token.sol", true},
	}
	for _, tc := range cases {
		got := f.Allow(tc.path)
		if got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestAllow_MultiLang verifies that the filter works across all ckv
// supported languages (Go, Solidity, TypeScript, JavaScript, Markdown).
func TestAllow_MultiLang(t *testing.T) {
	f := &FilterList{
		Include: []string{
			"contracts/**",
			"frontend/src/**",
			"docs/**",
		},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"contracts/Token.sol", true},
		{"contracts/Governance.sol", true},
		{"frontend/src/App.tsx", true},
		{"frontend/src/utils.js", true},
		{"docs/README.md", true},
		{"core/txpool/legacypool/legacypool.go", false},
		{"cmd/gstable/main.go", false},
	}
	for _, tc := range cases {
		got := f.Allow(tc.path)
		if got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestAllow_DoublestarZeroSegments verifies that ** matches zero segments
// (e.g. "consensus/wbft/**" matches "consensus/wbft/core.go" at depth 1).
func TestAllow_DoublestarZeroSegments(t *testing.T) {
	f := &FilterList{
		Include: []string{"consensus/wbft/**"},
	}
	// One segment deep (** matches zero additional segments beyond base).
	if !f.Allow("consensus/wbft/core.go") {
		t.Error("Allow(\"consensus/wbft/core.go\") = false, want true")
	}
	// Two segments deep.
	if !f.Allow("consensus/wbft/validator/validator.go") {
		t.Error("Allow(\"consensus/wbft/validator/validator.go\") = false, want true")
	}
}

// TestAllow_WildcardExtension verifies single-segment wildcard patterns.
func TestAllow_WildcardExtension(t *testing.T) {
	f := &FilterList{
		Include: []string{"contracts/*.sol"},
	}
	cases := []struct {
		path string
		want bool
	}{
		{"contracts/Token.sol", true},
		{"contracts/subdir/Token.sol", false}, // subdir not matched by single *
		{"contracts/Token.go", false},
	}
	for _, tc := range cases {
		got := f.Allow(tc.path)
		if got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestLoad_EmptyPath verifies that Load("") returns nil, nil.
func TestLoad_EmptyPath(t *testing.T) {
	f, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if f != nil {
		t.Fatalf("Load(\"\") = %+v, want nil", f)
	}
}

// TestLoad_JSONFile verifies that Load correctly parses a JSON file.
func TestLoad_JSONFile(t *testing.T) {
	data := FilterList{
		Include: []string{"consensus/wbft/**", "core/txpool/**"},
		Exclude: []string{"**/*_test.go"},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "files.json")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f == nil {
		t.Fatal("Load returned nil filter")
	}
	if len(f.Include) != 2 {
		t.Errorf("Include len = %d, want 2", len(f.Include))
	}
	if len(f.Exclude) != 1 {
		t.Errorf("Exclude len = %d, want 1", len(f.Exclude))
	}
	// Spot-check Allow after loading from file.
	if !f.Allow("consensus/wbft/validator/validator.go") {
		t.Error("Allow should be true for consensus/wbft/validator/validator.go")
	}
	if f.Allow("consensus/wbft/core/core_test.go") {
		t.Error("Allow should be false for consensus/wbft/core/core_test.go (excluded)")
	}
}

// TestFilterPaths verifies batch filtering.
func TestFilterPaths(t *testing.T) {
	f := &FilterList{
		Include: []string{"consensus/wbft/**"},
	}
	paths := []string{
		"consensus/wbft/validator/validator.go",
		"core/txpool/legacypool/legacypool.go",
		"consensus/wbft/core/core.go",
		"contracts/Token.sol",
	}
	got := f.FilterPaths(append([]string(nil), paths...))
	want := []string{
		"consensus/wbft/validator/validator.go",
		"consensus/wbft/core/core.go",
	}
	if len(got) != len(want) {
		t.Fatalf("FilterPaths len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("FilterPaths[%d] = %q, want %q", i, g, want[i])
		}
	}
}
