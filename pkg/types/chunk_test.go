package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChunkIDIsDeterministic(t *testing.T) {
	h := ContentSHA256("func foo() { return 1 }")
	a := ChunkID("internal/x.go", 10, 12, h)
	b := ChunkID("internal/x.go", 10, 12, h)
	if a != b {
		t.Fatalf("chunk_id must be deterministic: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("chunk_id must be hex-encoded sha256 (64 chars), got %d", len(a))
	}
}

func TestChunkIDChangesWithEachField(t *testing.T) {
	h := ContentSHA256("body")
	base := ChunkID("a.go", 1, 5, h)

	cases := []struct {
		name string
		got  string
	}{
		{"different file", ChunkID("b.go", 1, 5, h)},
		{"different start", ChunkID("a.go", 2, 5, h)},
		{"different end", ChunkID("a.go", 1, 6, h)},
		{"different content", ChunkID("a.go", 1, 5, ContentSHA256("other"))},
	}
	for _, c := range cases {
		if c.got == base {
			t.Errorf("%s: expected different chunk_id from base", c.name)
		}
	}
}

func TestFilterMatches(t *testing.T) {
	c := Chunk{
		File:       "internal/store/sqlitevec/store.go",
		Language:   "go",
		SymbolKind: KindMethod,
		CommitHash: "abc123",
	}

	cases := []struct {
		name string
		f    Filter
		want bool
	}{
		{"empty filter matches", Filter{}, true},
		{"language match", Filter{Language: "go"}, true},
		{"language mismatch", Filter{Language: "typescript"}, false},
		{"commit match", Filter{CommitHash: "abc123"}, true},
		{"commit mismatch", Filter{CommitHash: "deadbeef"}, false},
		{"kind set with match", Filter{SymbolKinds: []SymbolKind{KindMethod, KindFunction}}, true},
		{"kind set without match", Filter{SymbolKinds: []SymbolKind{KindContract}}, false},
		// filepath.Match doesn't support "**" globs — single-star only.
		// Use a path that the simple matcher can hit.
		{"path single-star match", Filter{PathGlob: "internal/store/sqlitevec/*.go"}, true},
		{"path mismatch", Filter{PathGlob: "cmd/*"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.Matches(c); got != tc.want {
				t.Errorf("Filter.Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterIsZero(t *testing.T) {
	if !(Filter{}).IsZero() {
		t.Error("empty filter must report IsZero")
	}
	if (Filter{Language: "go"}).IsZero() {
		t.Error("non-empty filter must not report IsZero")
	}
}

func TestChunkCategoryAndGuidance_JSONOmitempty(t *testing.T) {
	// Unclassified chunk: Category="" and Guidance=nil should be omitted
	// from JSON so old consumers (schema_version 1.0) stay compatible.
	plain := Chunk{
		ID: "x", File: "f.go", StartLine: 1, EndLine: 2,
		Language: "go", ChunkKind: ChunkSymbol, ContentSHA256: "h",
	}
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"category"`) {
		t.Errorf("empty Category must be omitted: %s", b)
	}
	if strings.Contains(string(b), `"guidance"`) {
		t.Errorf("nil Guidance must be omitted: %s", b)
	}
}

func TestChunkCategoryAndGuidance_RoundTrip(t *testing.T) {
	in := Chunk{
		ID: "x", File: "consensus/parlia/parlia.go",
		StartLine: 100, EndLine: 200,
		Language: "go", ChunkKind: ChunkSymbol, ContentSHA256: "h",
		Category: "consensus",
		Guidance: &ModificationGuidance{
			AlsoReview:    []string{"state", "params"},
			RequiredTests: []string{"fork choice", "byzantine"},
			WatchOut:      []string{"validator set change requires hard-fork coordination"},
		},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Chunk
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Category != "consensus" {
		t.Errorf("Category=%q, want consensus", out.Category)
	}
	if out.Guidance == nil {
		t.Fatal("Guidance must round-trip non-nil")
	}
	if len(out.Guidance.AlsoReview) != 2 || out.Guidance.AlsoReview[0] != "state" {
		t.Errorf("AlsoReview=%v", out.Guidance.AlsoReview)
	}
	if len(out.Guidance.RequiredTests) != 2 {
		t.Errorf("RequiredTests=%v", out.Guidance.RequiredTests)
	}
	if len(out.Guidance.WatchOut) != 1 {
		t.Errorf("WatchOut=%v", out.Guidance.WatchOut)
	}
}
