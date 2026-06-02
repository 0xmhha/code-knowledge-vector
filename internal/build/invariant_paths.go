package build

import "strings"

// testInvariantPaths lists source-tree path fragments whose *_test.go files
// SHOULD still run the Tier-3 invariant heuristic. Normally Tier-3 is
// suppressed in tests (fixtures emit panic/Errorf that aren't real
// invariants), but governance test suites encode load-bearing properties —
// TOCTOU ordering, burn atomicity, equal-power quorum — as test assertions we
// want indexed (00 §4 / 02 §4).
//
// Kept as a hardcoded fragment list (matched case-sensitively against the
// forward-slash relPath). Promoting this to a projectcfg knob is a follow-up;
// a fixed systemcontracts/test/ matcher satisfies the contract at lowest risk.
var testInvariantPaths = []string{
	"systemcontracts/test/",
}

// includeTestInvariants reports whether relPath is under a path where Tier-3
// invariant heuristics should run even in *_test.go files. relPath is the
// repo-relative, forward-slash path the build pipeline already uses.
func includeTestInvariants(relPath string) bool {
	p := strings.ReplaceAll(relPath, "\\", "/")
	for _, frag := range testInvariantPaths {
		if strings.Contains(p, frag) {
			return true
		}
	}
	return false
}
