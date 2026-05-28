package query

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		intent string
		want   []string
	}{
		{"validate gas tip", []string{"validate", "gas", "tip"}},
		{"the gas validation", []string{"gas", "validation"}}, // "the" filtered
		{"a b cd", []string{}},                                // short tokens filtered
		{"", nil},
	}
	for _, tt := range tests {
		got := extractKeywords(tt.intent)
		if len(got) != len(tt.want) {
			t.Errorf("extractKeywords(%q) = %v, want %v", tt.intent, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractKeywords(%q)[%d] = %q, want %q", tt.intent, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMatchesSignature(t *testing.T) {
	h := types.Hit{
		Chunk: types.Chunk{
			SymbolName: "ValidateGasTip",
			Text:       "func ValidateGasTip(tip *big.Int) error {\n  return nil\n}",
		},
	}
	if !matchesSignature(h, []string{"validate"}) {
		t.Error("expected match on 'validate'")
	}
	if !matchesSignature(h, []string{"gastip"}) {
		t.Error("expected match on 'gastip' in symbol name")
	}
	if matchesSignature(h, []string{"unrelated"}) {
		t.Error("unexpected match on 'unrelated'")
	}
}

func TestMatchesDoc(t *testing.T) {
	h := types.Hit{
		Chunk: types.Chunk{
			Text: "// ValidateGasTip checks the gas tip range.\n// Returns ErrGasTipOutOfRange when invalid.\nfunc ValidateGasTip() {}",
		},
	}
	if !matchesDoc(h, []string{"range"}) {
		t.Error("expected match on 'range' in doc comment")
	}
	if !matchesDoc(h, []string{"invalid"}) {
		t.Error("expected match on 'invalid' in doc comment")
	}
	// Word from code body, not from doc — should NOT match
	if matchesDoc(h, []string{"func"}) {
		t.Error("'func' is in code body, not doc")
	}
}

func TestMatchesPackage(t *testing.T) {
	h := types.Hit{
		Chunk: types.Chunk{File: "internal/governance/config.go"},
	}
	if !matchesPackage(h, "governance") {
		t.Error("expected match on 'governance' in path")
	}
	if !matchesPackage(h, "Governance") {
		t.Error("expected case-insensitive match")
	}
	if matchesPackage(h, "unrelated") {
		t.Error("unexpected match")
	}
	if matchesPackage(h, "") {
		t.Error("empty package should not match")
	}
}

func TestIsRecent(t *testing.T) {
	h := types.Hit{Chunk: types.Chunk{CommitHash: "abc123"}}
	if !isRecent(h, "abc123") {
		t.Error("expected recent when commits match")
	}
	if isRecent(h, "different") {
		t.Error("expected not recent when commits differ")
	}
	if isRecent(h, "") {
		t.Error("empty head should not be recent")
	}
}

func TestBoostService_Run_SignatureBoost(t *testing.T) {
	hits := []types.Hit{
		{Chunk: types.Chunk{SymbolName: "Unrelated", Text: "body"}, Score: types.HitScore{Normalized: 0.6}},
		{Chunk: types.Chunk{SymbolName: "ValidateGasTip", Text: "body"}, Score: types.HitScore{Normalized: 0.5}},
	}
	s := &BoostService{}
	opts := DefaultBoostOptions()
	opts.SignatureMatch = true
	result := s.Run(hits, "validate gas tip", opts)
	// ValidateGasTip gets 0.5 * 1.5 = 0.75, should rank first
	if result[0].Chunk.SymbolName != "ValidateGasTip" {
		t.Errorf("expected ValidateGasTip first, got %q", result[0].Chunk.SymbolName)
	}
	if result[0].Score.Normalized != 0.75 {
		t.Errorf("score = %f, want 0.75", result[0].Score.Normalized)
	}
}

func TestBoostService_Run_MultipleBoosts(t *testing.T) {
	hits := []types.Hit{
		{
			Chunk: types.Chunk{
				SymbolName: "ValidateGasTip",
				File:       "internal/governance/validator.go",
				Text:       "// Validates gas tip\nfunc ValidateGasTip() {}",
				CommitHash: "abc123",
			},
			Score: types.HitScore{Normalized: 0.5},
		},
	}
	s := &BoostService{}
	opts := DefaultBoostOptions()
	opts.SignatureMatch = true
	opts.DocMatch = true
	opts.PackageProximity = true
	opts.PackageKeyword = "governance"
	opts.RecentModified = true
	opts.IndexedHead = "abc123"

	result := s.Run(hits, "validate gas tip", opts)
	// All 4 boosts: 0.5 * 1.5 * 1.3 * 1.1 * 1.2 = 1.287
	expected := 0.5 * 1.5 * 1.3 * 1.1 * 1.2
	if abs(result[0].Score.Normalized-expected) > 1e-6 {
		t.Errorf("score = %f, want %f", result[0].Score.Normalized, expected)
	}
}

func TestBoostService_Run_EmptyHits(t *testing.T) {
	s := &BoostService{}
	got := s.Run(nil, "intent", DefaultBoostOptions())
	if got != nil {
		t.Errorf("expected nil for empty hits, got %v", got)
	}
}

func TestBoostService_Run_StableSort(t *testing.T) {
	// Two hits with equal boosted scores should keep input order
	hits := []types.Hit{
		{Chunk: types.Chunk{SymbolName: "A"}, Score: types.HitScore{Normalized: 0.5}},
		{Chunk: types.Chunk{SymbolName: "B"}, Score: types.HitScore{Normalized: 0.5}},
	}
	s := &BoostService{}
	opts := DefaultBoostOptions()
	result := s.Run(hits, "intent", opts)
	if result[0].Chunk.SymbolName != "A" || result[1].Chunk.SymbolName != "B" {
		t.Errorf("stable sort broken: got %s, %s", result[0].Chunk.SymbolName, result[1].Chunk.SymbolName)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
