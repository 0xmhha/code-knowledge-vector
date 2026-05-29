package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Single-star segment matching.
		{"core/*.go", "core/blockchain.go", true},
		{"core/*.go", "core/state/journal.go", false},

		// Double-star segment matching.
		{"consensus/**", "consensus/parlia/parlia.go", true},
		{"consensus/**", "consensus/engine.go", true},
		{"consensus/**", "core/state/journal.go", false},
		{"core/state/**", "core/state/journal.go", true},
		{"core/state/**", "core/state/snapshot/snapshot.go", true},
		{"core/state/**", "core/blockchain.go", false},

		// Mid-pattern doublestar.
		{"**/*.go", "x.go", true},
		{"**/*.go", "core/state/journal.go", true},
		{"**/*_test.go", "core/blockchain_test.go", true},
		{"**/*_test.go", "core/blockchain.go", false},

		// Trailing doublestar matches empty.
		{"params/**", "params", true},
		{"params/**", "params/config.go", true},
		{"params/**", "core/params.go", false},

		// No match on completely unrelated path.
		{"systemcontracts/**", "p2p/discover.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"|"+tc.path, func(t *testing.T) {
			if got := matchGlob(tc.pattern, tc.path); got != tc.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestParseAndValidate_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"missing name", `
version: 1
categories:
  - paths: ["consensus/**"]
`},
		{"missing paths", `
version: 1
categories:
  - name: consensus
`},
		{"duplicate name", `
version: 1
categories:
  - name: consensus
    paths: ["consensus/**"]
  - name: consensus
    paths: ["miner/**"]
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.yaml)); err == nil {
				t.Errorf("Parse(%s) should fail", tc.name)
			}
		})
	}
}

func TestApply_FirstMatchWins(t *testing.T) {
	yaml := `
version: 1
categories:
  - name: consensus
    paths: ["consensus/**", "miner/**"]
    also_review: ["state", "params"]
    required_tests: ["fork choice"]
    watch_out: ["hard-fork coordination"]
  - name: state
    paths: ["core/state/**", "trie/**"]
    also_review: ["consensus"]
    required_tests: ["state root consistency"]
    watch_out: ["DB migration compatibility"]
  - name: test
    paths: ["**/*_test.go"]
    watch_out: ["no production policy applies"]
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	chunks := []types.Chunk{
		{File: "consensus/parlia/parlia.go"}, // → consensus
		{File: "core/state/journal.go"},      // → state
		{File: "miner/worker.go"},            // → consensus (first rule)
		{File: "core/blockchain_test.go"},    // → test
		{File: "p2p/discover.go"},            // unclassified
	}
	counts := p.Apply(chunks)

	wantCat := []string{"consensus", "state", "consensus", "test", ""}
	for i, w := range wantCat {
		if chunks[i].Category != w {
			t.Errorf("chunks[%d].Category = %q, want %q", i, chunks[i].Category, w)
		}
	}

	// Consensus chunk gets the consensus guidance.
	g := chunks[0].Guidance
	if g == nil {
		t.Fatal("Guidance must be set for matched chunk")
	}
	if len(g.AlsoReview) != 2 || g.AlsoReview[0] != "state" {
		t.Errorf("AlsoReview=%v", g.AlsoReview)
	}
	if len(g.RequiredTests) != 1 || g.RequiredTests[0] != "fork choice" {
		t.Errorf("RequiredTests=%v", g.RequiredTests)
	}

	// Unclassified chunk has nil Guidance.
	if chunks[4].Guidance != nil {
		t.Errorf("unmatched chunk should have nil Guidance, got %+v", chunks[4].Guidance)
	}

	// Coverage counts include unclassified ("").
	if counts["consensus"] != 2 {
		t.Errorf("counts[consensus] = %d, want 2", counts["consensus"])
	}
	if counts[""] != 1 {
		t.Errorf("counts[\"\"] (unclassified) = %d, want 1", counts[""])
	}
}

func TestApply_GuidanceIsCopied_NotShared(t *testing.T) {
	// Each chunk should hold its own Guidance copy. Mutating one chunk's
	// slice must not bleed into others matched by the same rule.
	yaml := `
version: 1
categories:
  - name: shared
    paths: ["**/*.go"]
    watch_out: ["original"]
`
	p, _ := Parse([]byte(yaml))
	chunks := []types.Chunk{{File: "a.go"}, {File: "b.go"}}
	p.Apply(chunks)

	chunks[0].Guidance.WatchOut[0] = "modified"
	if chunks[1].Guidance.WatchOut[0] != "original" {
		t.Errorf("Guidance must be deep-copied per chunk; got cross-contamination")
	}
}

func TestLoad_EmptyPath_ReturnsEmptyPolicy(t *testing.T) {
	p, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if p == nil {
		t.Fatal("empty Load must return non-nil empty Policy, not nil")
	}
	if len(p.Categories) != 0 {
		t.Errorf("empty policy should have 0 categories, got %d", len(p.Categories))
	}
	// Apply on empty policy → all unclassified, no crash.
	chunks := []types.Chunk{{File: "anything.go"}}
	counts := p.Apply(chunks)
	if counts[""] != 1 {
		t.Errorf("empty policy must leave chunks unclassified, got counts=%v", counts)
	}
}

func TestLoad_FromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	yaml := `
version: 1
categories:
  - name: rpc
    paths: ["rpc/**"]
    watch_out: ["client compatibility"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(p.Categories) != 1 || p.Categories[0].Name != "rpc" {
		t.Errorf("unexpected loaded policy: %+v", p)
	}
}

func TestGuidanceJSON_RoundTrip(t *testing.T) {
	g := &types.ModificationGuidance{
		AlsoReview:    []string{"state"},
		RequiredTests: []string{"fork test"},
		WatchOut:      []string{"hard-fork"},
	}
	raw, err := GuidanceJSON(g)
	if err != nil {
		t.Fatalf("GuidanceJSON: %v", err)
	}
	if raw == "" {
		t.Error("non-nil guidance should produce non-empty JSON")
	}
	back, err := GuidanceFromJSON(raw)
	if err != nil {
		t.Fatalf("GuidanceFromJSON: %v", err)
	}
	if back == nil || back.AlsoReview[0] != "state" {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestGuidanceJSON_NilEmpty(t *testing.T) {
	raw, err := GuidanceJSON(nil)
	if err != nil {
		t.Fatalf("GuidanceJSON(nil): %v", err)
	}
	if raw != "" {
		t.Errorf("nil guidance should produce empty string, got %q", raw)
	}
	back, err := GuidanceFromJSON("")
	if err != nil {
		t.Fatalf("GuidanceFromJSON(\"\"): %v", err)
	}
	if back != nil {
		t.Errorf("empty JSON should round-trip to nil, got %+v", back)
	}
}
