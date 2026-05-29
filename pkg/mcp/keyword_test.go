package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestHandleKeywordSearch_RequiresQuery(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleKeywordSearch(context.Background(),
		callRequest("cks.context.keyword_search", map[string]any{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Error("expected error when query is missing")
	}
}

func TestHandleKeywordSearch_BM25ScoresPopulated(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleKeywordSearch(context.Background(),
		callRequest("cks.context.keyword_search", map[string]any{
			"query": "server",
			"k":     5.0,
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, res))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hits, _ := resp["hits"].([]any)
	if len(hits) == 0 {
		t.Fatal("expected ≥1 hit for query 'server' over the sample corpus")
	}
	first, _ := hits[0].(map[string]any)
	score, _ := first["score"].(map[string]any)
	bm25, _ := score["bm25_score"].(float64)
	if bm25 <= 0 {
		t.Errorf("expected bm25_score > 0 for top hit, got %v: %v", bm25, score)
	}
}

func TestHandleKeywordSearch_LanguageFilter(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleKeywordSearch(context.Background(),
		callRequest("cks.context.keyword_search", map[string]any{
			"query":    "Handler",
			"language": "typescript",
			"k":        10.0,
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("err: %s", textContent(t, res))
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hits, _ := resp["hits"].([]any)
	for _, h := range hits {
		m, _ := h.(map[string]any)
		if m["language"] != "typescript" {
			t.Errorf("language filter leaked non-ts hit: %v", m)
		}
	}
}

// TestKeywordSearch_FindsExactSymbol verifies BM25 ranks an exact
// symbol name above accidental token overlaps elsewhere. The sample
// corpus's `NewServer` constructor is a clean test target.
func TestKeywordSearch_FindsExactSymbol(t *testing.T) {
	eng := buildSample(t)
	hits, err := eng.KeywordSearch(context.Background(), "NewServer", 5, types.Filter{})
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected ≥1 hit for 'NewServer'")
	}
	found := false
	for _, h := range hits {
		if strings.Contains(h.Symbol, "NewServer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("NewServer constructor should appear in top-5 keyword hits: %+v", hits)
	}
}
