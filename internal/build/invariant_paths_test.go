package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestIncludeTestInvariants(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"systemcontracts/test/gov_council_test.go", true},
		{"systemcontracts/test/gov_minter.go", true}, // dir match, not _test suffix-dependent
		{"systemcontracts/gov_council.go", false},
		{"consensus/wbft/quorum_test.go", false},
		{"pkg/foo/foo_test.go", false},
		{"systemcontracts\\test\\win_path_test.go", true}, // backslash normalized
	}
	for _, c := range cases {
		if got := includeTestInvariants(c.path); got != c.want {
			t.Errorf("includeTestInvariants(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestProcessFile_Tier3TestOverride proves the per-file override: the SAME
// Tier-3 heuristic panic (policy-keyword string literal) yields a
// ChunkInvariant when the file lives under systemcontracts/test/, but is
// suppressed for a generic *_test.go file. This is the behavioral core of
// 02 §4 — governance test invariants get indexed, ordinary test fixtures
// don't.
func TestProcessFile_Tier3TestOverride(t *testing.T) {
	// Tier-3 path (a): a direct string literal carrying a policy keyword
	// ("validator", "must", "quorum"→"must"/"validator"). NO Tier-1/Tier-2
	// markers, so any invariant chunk here is purely the Tier-3 heuristic.
	const fixture = `package gov

func mintGuard(power int) {
	if power != 1 {
		panic("validator must hold equal power under byzantine quorum")
	}
}
`
	dir := t.TempDir()
	abs := filepath.Join(dir, "f_test.go")
	if err := os.WriteFile(abs, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	parsers := newParsers()
	chunker := newChunker(mock.Default(), nil)

	countInvariant := func(relPath string) int {
		t.Helper()
		chunks, err := processFile(abs, relPath, "go", "", parsers, nil, chunker)
		if err != nil {
			t.Fatalf("processFile(%q): %v", relPath, err)
		}
		n := 0
		for _, c := range chunks {
			if c.ChunkKind == types.ChunkInvariant {
				n++
			}
		}
		return n
	}

	// Under systemcontracts/test/ → override active → Tier-3 runs in _test.go.
	if got := countInvariant("systemcontracts/test/gov_minter_test.go"); got < 1 {
		t.Errorf("systemcontracts/test/ _test.go: got %d invariant chunks, want >=1 (Tier-3 override should fire)", got)
	}

	// Generic _test.go → override inactive → Tier-3 suppressed.
	if got := countInvariant("pkg/minter/gov_minter_test.go"); got != 0 {
		t.Errorf("generic _test.go: got %d invariant chunks, want 0 (Tier-3 must stay suppressed)", got)
	}

	// Non-test file anywhere → Tier-3 always runs (sanity: the policy panic is
	// detectable at all, so the zero above is the override, not a dead fixture).
	if got := countInvariant("pkg/minter/gov_minter.go"); got < 1 {
		t.Errorf("non-test file: got %d invariant chunks, want >=1 (Tier-3 always runs outside tests)", got)
	}
}
