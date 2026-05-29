package policy

import (
	"path/filepath"
	"testing"
)

// TestStablenetYaml_ParsesAndCovers is a structural sanity check on
// the shipped policy file. Asserts every well-known category is
// present and reasonably populated. Rule changes that drop a category
// should be deliberate — this test makes that deliberate.
func TestStablenetYaml_ParsesAndCovers(t *testing.T) {
	// Find the policy file from the test's working dir
	// (internal/policy/ → ../../policy/stablenet.yaml).
	path := filepath.Join("..", "..", "policy", "stablenet.yaml")
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load %s: %v", path, err)
	}

	wantCats := []string{
		"test", "consensus", "beacon", "state", "crypto",
		"p2p", "txpool", "rpc", "systemcontracts", "params",
		"miner", "cli", "docs",
	}
	got := map[string]CategoryRule{}
	for _, c := range p.Categories {
		got[c.Name] = c
	}
	for _, want := range wantCats {
		if _, ok := got[want]; !ok {
			t.Errorf("missing category %q in stablenet.yaml", want)
		}
	}

	// Each non-test, non-docs category should have ≥1 paths, ≥1
	// required_tests, and ≥1 watch_out (the agent's hint surface).
	for _, c := range p.Categories {
		if c.Name == "test" || c.Name == "docs" {
			continue
		}
		if len(c.Paths) == 0 {
			t.Errorf("%s: needs ≥1 paths", c.Name)
		}
		if len(c.WatchOut) == 0 {
			t.Errorf("%s: needs ≥1 watch_out", c.Name)
		}
	}
}

// TestStablenetYaml_ClassifiesKnownFiles verifies that representative
// paths from the go-stablenet tree resolve to the expected category.
// If go-stablenet's layout shifts (file moves, renames), this test
// flags the policy as out-of-date.
func TestStablenetYaml_ClassifiesKnownFiles(t *testing.T) {
	path := filepath.Join("..", "..", "policy", "stablenet.yaml")
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		file string
		want string
	}{
		{"consensus/clique/clique.go", "consensus"},
		{"consensus/wbft/engine.go", "consensus"},
		{"miner/worker.go", "consensus"},
		{"core/state/journal.go", "state"},
		{"core/state/snapshot/snapshot.go", "state"},
		{"trie/trie.go", "state"},
		{"triedb/database.go", "state"},
		{"crypto/bls/blst/signature.go", "crypto"},
		{"signer/core/api.go", "crypto"},
		{"p2p/discover.go", "p2p"},
		{"eth/downloader/downloader.go", "p2p"},
		{"eth/handler.go", "p2p"},
		{"core/txpool/txpool.go", "txpool"},
		{"rpc/server.go", "rpc"},
		{"internal/ethapi/api.go", "rpc"},
		{"systemcontracts/gov_minter.go", "systemcontracts"},
		{"params/config.go", "params"},
		{"beacon/light/api.go", "beacon"},
		{"cmd/geth/main.go", "cli"},
		{"core/blockchain_test.go", "test"},
		{"docs/CONTRIBUTING.md", "docs"},
		{"README.md", "docs"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, _ := p.match(tc.file)
			if got != tc.want {
				t.Errorf("classify(%s) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}
