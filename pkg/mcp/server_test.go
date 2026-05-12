package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// buildSample mirrors the helper in internal/query: it indexes
// testdata/sample with the mock embedder so MCP handlers have a real
// engine to talk to.
func buildSample(t *testing.T) *query.Engine {
	t.Helper()
	srcAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if _, err := build.Run(context.Background(), build.Options{
		SrcRoot:  srcAbs,
		OutDir:   outDir,
		Embedder: mock.Default(),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("build: %v", err)
	}
	eng, err := query.Open(outDir, mock.Default())
	if err != nil {
		t.Fatalf("query.Open: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func callRequest(name string, args map[string]any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: name, Arguments: args},
	}
}

// textContent extracts the first text content from a CallToolResult.
// The result's Content slice is []mcp.Content (interface); for our
// jsonResult helper the entry is *mcp.TextContent.
func textContent(t *testing.T, res *mcpgo.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if len(res.Content) == 0 {
		t.Fatal("empty result content")
	}
	tc, ok := res.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestSemanticSearchHandlerReturnsJSON(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	res, err := s.handleSemanticSearch(context.Background(),
		callRequest("cks.context.semantic_search", map[string]any{
			"intent": "TCP socket bind on port",
			"k":      float64(3),
		}))
	if err != nil {
		t.Fatalf("handleSemanticSearch: %v", err)
	}
	body := textContent(t, res)

	var decoded query.Response
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode response: %v\n%s", err, body)
	}
	if len(decoded.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	// Acceptance signal: server.go appears in the citations.
	var found bool
	for _, h := range decoded.Hits {
		if h.Citation.File == "server.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected server.go in hits, got: %+v", decoded.Hits)
	}
}

func TestSemanticSearchHandlerRequiresIntent(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleSemanticSearch(context.Background(),
		callRequest("cks.context.semantic_search", map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for missing intent")
	}
}

func TestHealthHandlerReportsManifest(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	res, err := s.handleHealth(context.Background(),
		callRequest("cks.ops.health", nil))
	if err != nil {
		t.Fatalf("handleHealth: %v", err)
	}
	body := textContent(t, res)
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	if m["server"] != "ckv" {
		t.Errorf("expected server=ckv, got %v", m["server"])
	}
	// chunk_count comes from manifest; should be > 0 after a build.
	if v, _ := m["chunk_count"].(float64); v <= 0 {
		t.Errorf("expected chunk_count > 0, got %v", m["chunk_count"])
	}
	if v, _ := m["embedding_model"].(string); v == "" {
		t.Errorf("expected embedding_model populated, got %q", v)
	}
}

func TestGetFreshnessHandlerExecutes(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	res, err := s.handleGetFreshness(context.Background(),
		callRequest("cks.ops.get_freshness", nil))
	if err != nil {
		t.Fatalf("handleGetFreshness: %v", err)
	}
	body := textContent(t, res)
	// The handler may return a warning when src_root isn't a git repo;
	// what matters here is that it returns a parseable JSON payload
	// with the expected shape.
	var report map[string]any
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	if _, ok := report["indexed_head"]; !ok {
		t.Errorf("freshness response missing indexed_head: %s", body)
	}
}

func TestServerConstructs(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	if s == nil || s.Underlying() == nil {
		t.Fatal("NewServer must produce a non-nil server with a non-nil MCPServer")
	}
}
