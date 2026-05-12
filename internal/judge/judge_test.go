package judge

import (
	"context"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestExtractVerdictPlainJSON(t *testing.T) {
	out := []byte(`{"score": 5, "rationale": "top hit is exact"}`)
	v, ok := extractVerdict(out)
	if !ok || v.Score != 5 {
		t.Fatalf("expected score=5, got ok=%v v=%+v", ok, v)
	}
	if v.Rationale != "top hit is exact" {
		t.Errorf("rationale not parsed: %q", v.Rationale)
	}
}

func TestExtractVerdictWithCodeFence(t *testing.T) {
	out := []byte("```json\n{\"score\": 3, \"rationale\": \"ok\"}\n```")
	v, ok := extractVerdict(out)
	if !ok || v.Score != 3 {
		t.Fatalf("fenced parsing failed: ok=%v v=%+v", ok, v)
	}
}

func TestExtractVerdictWithPreamble(t *testing.T) {
	out := []byte("Sure! Here's my judgment:\n{\"score\":2, \"rationale\":\"partial\"}\nLet me know if you want more.")
	v, ok := extractVerdict(out)
	if !ok || v.Score != 2 {
		t.Fatalf("preamble parsing failed: ok=%v v=%+v", ok, v)
	}
}

func TestExtractVerdictRejectsUnscored(t *testing.T) {
	out := []byte(`{"rationale": "no score field"}`)
	if _, ok := extractVerdict(out); ok {
		t.Error("expected parse failure when score absent or zero")
	}
}

func TestBuildPromptIncludesAllPieces(t *testing.T) {
	hits := []query.Hit{
		{
			ChunkID:  "x",
			Citation: types.Citation{File: "server.go", StartLine: 22, EndLine: 29},
			Snippet:  "func (s *Server) Listen() error { ... }",
			Symbol:   "Server.Listen",
			SymbolKind: types.KindMethod,
			Score:    types.HitScore{Normalized: 0.92},
		},
	}
	p := buildPrompt("bind TCP socket", hits)
	for _, want := range []string{
		"bind TCP socket",
		"server.go:22-29",
		"Server.Listen",
		"Method",
		"Score rubric",
		"Output JSON only.",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
}

func TestClaudeCLIMissingBinaryFoldsToError(t *testing.T) {
	j := &ClaudeCLI{Binary: "claude-this-binary-does-not-exist"}
	v := j.Grade(context.Background(), "q1", "intent", nil)
	if v.Error == "" {
		t.Errorf("expected Error populated, got %+v", v)
	}
	if v.Score != 0 {
		t.Errorf("expected Score=0 on missing binary, got %d", v.Score)
	}
}
