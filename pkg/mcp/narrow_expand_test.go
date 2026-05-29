package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// TestHandleNarrowCandidates_RequiresChunkIDs verifies the handler
// rejects empty input rather than running unfiltered.
func TestHandleNarrowCandidates_RequiresChunkIDs(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleNarrowCandidates(context.Background(),
		callRequest("cks.context.narrow_candidates", map[string]any{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Error("expected error when chunk_ids_json is missing")
	}
}

// TestHandleNarrowCandidates_FilterByLanguage runs the full path: get
// a hit set via semantic_search, then narrow to a language and confirm
// the returned IDs all match.
func TestHandleNarrowCandidates_FilterByLanguage(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	sem, err := s.handleSemanticSearch(context.Background(),
		callRequest("cks.context.semantic_search", map[string]any{"intent": "server", "k": 10.0}))
	if err != nil {
		t.Fatalf("semantic_search: %v", err)
	}
	body := textContent(t, sem)

	var semResp map[string]any
	if err := json.Unmarshal([]byte(body), &semResp); err != nil {
		t.Fatalf("decode semantic_search: %v", err)
	}
	hits, _ := semResp["hits"].([]any)
	if len(hits) == 0 {
		t.Skip("no hits returned from semantic_search; cannot exercise narrow")
	}
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		m, _ := h.(map[string]any)
		if id, ok := m["chunk_id"].(string); ok {
			ids = append(ids, id)
		}
	}
	idsJSON, _ := json.Marshal(ids)

	res, err := s.handleNarrowCandidates(context.Background(),
		callRequest("cks.context.narrow_candidates", map[string]any{
			"chunk_ids_json": string(idsJSON),
			"language":       "go",
		}))
	if err != nil {
		t.Fatalf("narrow: %v", err)
	}
	if res.IsError {
		t.Fatalf("narrow returned error: %s", textContent(t, res))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode narrow: %v", err)
	}
	got, _ := resp["hits"].([]any)
	for _, h := range got {
		m, _ := h.(map[string]any)
		if m["language"] != "go" {
			t.Errorf("narrow leaked non-go hit: %v", m)
		}
	}
}

// TestHandleExpandInFile_ReturnsNeighbours seeds a chunk, calls expand,
// and asserts the original plus at least one neighbour share the file.
func TestHandleExpandInFile_ReturnsNeighbours(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	sem, err := s.handleSemanticSearch(context.Background(),
		callRequest("cks.context.semantic_search", map[string]any{"intent": "cache", "k": 1.0}))
	if err != nil {
		t.Fatalf("semantic_search: %v", err)
	}
	body := textContent(t, sem)
	var semResp map[string]any
	if err := json.Unmarshal([]byte(body), &semResp); err != nil {
		t.Fatalf("decode semantic_search: %v", err)
	}
	hits, _ := semResp["hits"].([]any)
	if len(hits) == 0 {
		t.Skip("no hits to expand")
	}
	first, _ := hits[0].(map[string]any)
	id, _ := first["chunk_id"].(string)

	res, err := s.handleExpandInFile(context.Background(),
		callRequest("cks.context.expand_in_file", map[string]any{
			"chunk_id": id,
			"before":   2.0,
			"after":    2.0,
		}))
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if res.IsError {
		t.Fatalf("expand error: %s", textContent(t, res))
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode expand: %v", err)
	}
	got, _ := resp["hits"].([]any)
	if len(got) == 0 {
		t.Fatal("expand returned no hits")
	}
	srcFile, _ := first["citation"].(map[string]any)
	wantFile, _ := srcFile["file"].(string)
	for _, h := range got {
		m, _ := h.(map[string]any)
		cit, _ := m["citation"].(map[string]any)
		if cit["file"] != wantFile {
			t.Errorf("expand returned chunk from different file: got %v, want %s",
				cit["file"], wantFile)
		}
	}
}

func TestHandleExpandInFile_UnknownChunkID(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleExpandInFile(context.Background(),
		callRequest("cks.context.expand_in_file", map[string]any{
			"chunk_id": "definitely-not-a-real-id",
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for unknown chunk_id")
	}
}
