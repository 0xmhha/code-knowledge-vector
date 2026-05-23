package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandQuery_NoAliasesReturnsIntentUnchanged(t *testing.T) {
	if got := ExpandQuery("anything", nil); got != "anything" {
		t.Errorf("nil aliases: got %q, want %q", got, "anything")
	}
	if got := ExpandQuery("anything", AliasMap{}); got != "anything" {
		t.Errorf("empty aliases: got %q, want %q", got, "anything")
	}
}

func TestExpandQuery_KoreanPhraseGetsEnglishKeywords(t *testing.T) {
	aliases := AliasMap{
		"0번 블록":      {"genesis", "GenesisAlloc"},
		"합의 알고리즘": {"consensus", "wbft"},
	}
	got := ExpandQuery("0번 블록 시스템 컨트랙트 어떻게 주입돼?", aliases)
	if !strings.HasPrefix(got, "0번 블록 시스템 컨트랙트 어떻게 주입돼?") {
		t.Errorf("expanded intent must keep original prefix: %q", got)
	}
	for _, kw := range []string{"genesis", "GenesisAlloc"} {
		if !strings.Contains(got, kw) {
			t.Errorf("expected alias keyword %q in expansion, got %q", kw, got)
		}
	}
	// Unmatched key (합의 알고리즘) must NOT leak its keywords.
	if strings.Contains(got, "consensus") || strings.Contains(got, "wbft") {
		t.Errorf("unmatched alias leaked: %q", got)
	}
}

func TestExpandQuery_DeterministicOrdering(t *testing.T) {
	// Same intent + same alias map must produce identical output across
	// runs — required so the query.embed fingerprint stays stable.
	aliases := AliasMap{
		"alpha": {"z_kw", "a_kw"},
		"beta":  {"m_kw", "b_kw"},
	}
	intent := "alpha beta"
	first := ExpandQuery(intent, aliases)
	for i := range 50 {
		if got := ExpandQuery(intent, aliases); got != first {
			t.Fatalf("non-deterministic expansion on run %d: %q vs %q", i, got, first)
		}
	}
	// Spot-check the order is sorted (a_kw before b_kw before m_kw before z_kw).
	posA := strings.Index(first, "a_kw")
	posB := strings.Index(first, "b_kw")
	posM := strings.Index(first, "m_kw")
	posZ := strings.Index(first, "z_kw")
	if !(posA < posB && posB < posM && posM < posZ) {
		t.Errorf("keywords not sorted: %q", first)
	}
}

func TestExpandQuery_DedupOverlappingMatches(t *testing.T) {
	// Two keys whose alias lists overlap should not produce duplicates.
	aliases := AliasMap{
		"genesis": {"GenesisAlloc", "genesis_block"},
		"제네시스": {"GenesisAlloc", "genesis"},
	}
	got := ExpandQuery("genesis 제네시스", aliases)
	// Count of "GenesisAlloc" should be exactly 1 in the [aliases: ...] tail.
	if strings.Count(got, "GenesisAlloc") != 1 {
		t.Errorf("GenesisAlloc duplicated: %q", got)
	}
}

func TestExpandQuery_EmptyKeyOrKeywordIgnored(t *testing.T) {
	aliases := AliasMap{
		"":        {"should-not-appear"},
		"keyword": {"", "  ", "valid"},
	}
	got := ExpandQuery("keyword", aliases)
	if !strings.Contains(got, "valid") {
		t.Errorf("non-empty keyword should appear: %q", got)
	}
	if strings.Contains(got, "should-not-appear") {
		t.Errorf("empty key matched everything; output should not contain its keywords: %q", got)
	}
}

func TestLoadAliasMap_EmptyPathReturnsNil(t *testing.T) {
	m, err := LoadAliasMap("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	if m != nil {
		t.Errorf("empty path should return nil map, got %+v", m)
	}
}

func TestLoadAliasMap_ParsesRealYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glossary.yaml")
	body := `aliases:
  "0번 블록":
    - genesis
    - GenesisAlloc
  "합의 알고리즘":
    - consensus
    - wbft
    - WBFT
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadAliasMap(path)
	if err != nil {
		t.Fatalf("LoadAliasMap: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("want 2 keys, got %d", len(m))
	}
	if len(m["0번 블록"]) != 2 || m["0번 블록"][0] != "genesis" {
		t.Errorf("'0번 블록' parsed wrong: %v", m["0번 블록"])
	}
	if len(m["합의 알고리즘"]) != 3 {
		t.Errorf("'합의 알고리즘' should have 3 keywords, got %v", m["합의 알고리즘"])
	}
}

func TestLoadAliasMap_EmptyAliasesBlockReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte("aliases: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadAliasMap(path)
	if err != nil {
		t.Fatalf("LoadAliasMap: %v", err)
	}
	if m != nil {
		t.Errorf("empty aliases block should return nil, got %+v", m)
	}
}

func TestLoadAliasMap_MissingFile(t *testing.T) {
	_, err := LoadAliasMap("/nonexistent/path/glossary.yaml")
	if err == nil {
		t.Fatal("missing file should error")
	}
}
