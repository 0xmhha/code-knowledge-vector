package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHandleExplainMatch_RequiresArgs(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleExplainMatch(context.Background(),
		callRequest("cks.context.explain_match", map[string]any{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Error("expected error when args are missing")
	}
}

func TestHandleExplainMatch_UnknownChunk(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleExplainMatch(context.Background(),
		callRequest("cks.context.explain_match", map[string]any{
			"chunk_id": "deadbeef-not-real",
			"intent":   "anything",
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for unknown chunk_id")
	}
}

func TestHandleExplainMatch_ReturnsBothScores(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	// Seed: get a hit ID via semantic_search.
	sem, err := s.handleSemanticSearch(context.Background(),
		callRequest("cks.context.semantic_search", map[string]any{"intent": "server", "k": 1.0}))
	if err != nil {
		t.Fatalf("semantic: %v", err)
	}
	var semResp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, sem)), &semResp); err != nil {
		t.Fatalf("decode semantic: %v", err)
	}
	hits, _ := semResp["hits"].([]any)
	if len(hits) == 0 {
		t.Skip("no hits to explain")
	}
	first, _ := hits[0].(map[string]any)
	id, _ := first["chunk_id"].(string)

	res, err := s.handleExplainMatch(context.Background(),
		callRequest("cks.context.explain_match", map[string]any{
			"chunk_id": id,
			"intent":   "server",
		}))
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if res.IsError {
		t.Fatalf("explain returned error: %s", textContent(t, res))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["vector_score"].(map[string]any); !ok {
		t.Errorf("expected vector_score map: %v", resp)
	}
	if _, ok := resp["keyword_score"].(map[string]any); !ok {
		t.Errorf("expected keyword_score map: %v", resp)
	}
	if resp["chunk_id"] != id {
		t.Errorf("chunk_id round-trip broken: got %v, want %s", resp["chunk_id"], id)
	}
}
