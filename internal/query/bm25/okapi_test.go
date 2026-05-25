package bm25

import (
	"math"
	"testing"
)

// Minimal regression coverage for the Okapi algorithm — full algorithm
// tests live in the CKG source from which this file was adapted. We keep
// just enough here to catch a copy-paste accident that breaks scoring.

func TestOkapi_EmptyCorpusReturnsZero(t *testing.T) {
	o := NewOkapi()
	if got := o.Score([]string{"foo"}, "x"); got != 0 {
		t.Errorf("empty corpus → 0; got %g", got)
	}
}

func TestOkapi_MatchingTermProducesPositiveScore(t *testing.T) {
	o := NewOkapi()
	o.Index([]Document{
		{ID: "a", Tokens: []string{"alpha", "shared"}},
		{ID: "b", Tokens: []string{"beta", "shared", "extra"}},
	})
	if got := o.Score([]string{"alpha"}, "a"); got <= 0 {
		t.Errorf("matching term should produce positive score, got %g", got)
	}
	if got := o.Score([]string{"alpha"}, "b"); got != 0 {
		t.Errorf("non-matching term in doc b should score 0, got %g", got)
	}
}

func TestOkapi_TopKSortsDescending(t *testing.T) {
	o := NewOkapi()
	o.Index([]Document{
		{ID: "a", Tokens: []string{"foo"}},
		{ID: "b", Tokens: []string{"foo", "foo"}},  // higher TF → higher score
		{ID: "c", Tokens: []string{"bar"}},
	})
	top := o.TopK([]string{"foo"}, 0)
	if len(top) != 2 { // "c" has 0 score; excluded
		t.Fatalf("expected 2 results, got %d", len(top))
	}
	if top[0].ID != "b" {
		t.Errorf("expected 'b' to rank first by TF; got %q", top[0].ID)
	}
	if top[0].Score <= top[1].Score {
		t.Errorf("top scores not descending: %g vs %g", top[0].Score, top[1].Score)
	}
}

func TestOkapi_IDFHigherForRareTerms(t *testing.T) {
	o := NewOkapi()
	o.Index([]Document{
		{ID: "1", Tokens: []string{"rare", "common"}},
		{ID: "2", Tokens: []string{"common"}},
		{ID: "3", Tokens: []string{"common"}},
	})
	rareIDF := o.idf("rare")
	commonIDF := o.idf("common")
	if rareIDF <= commonIDF {
		t.Errorf("rare term IDF (%g) should exceed common term IDF (%g)", rareIDF, commonIDF)
	}
	if math.IsNaN(rareIDF) || math.IsNaN(commonIDF) {
		t.Error("IDF must not be NaN")
	}
}
