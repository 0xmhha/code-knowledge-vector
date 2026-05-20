package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestHealthHandlerReportsEmbedderStatus(t *testing.T) {
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

	// embedder object (CKV-6 expansion): name + dimension + status.
	emb, ok := m["embedder"].(map[string]any)
	if !ok {
		t.Fatalf("embedder block missing or wrong shape: %v", m["embedder"])
	}
	if name, _ := emb["name"].(string); name == "" {
		t.Errorf("embedder.name should not be empty")
	}
	if dim, _ := emb["dimension"].(float64); dim <= 0 {
		t.Errorf("embedder.dimension should be >0, got %v", emb["dimension"])
	}
	// buildSample uses the mock embedder → status should be "stub" so
	// cks can render a degraded indicator without inspecting the name.
	if got, _ := emb["status"].(string); got != "stub" {
		t.Errorf("embedder.status: got %q, want %q for mock embedder", got, "stub")
	}
	// provider and model_dir are optional; they only appear for
	// embedders that implement the duck-typed interfaces. Mock does
	// not, so both should be absent (or empty string).
	if v, present := emb["provider"]; present {
		if s, _ := v.(string); s != "" {
			t.Errorf("embedder.provider should be empty for mock, got %q", s)
		}
	}

	// index object: chunk_count + last_built_at duplicated from manifest
	// so cks can read everything index-side under one key.
	idx, ok := m["index"].(map[string]any)
	if !ok {
		t.Fatalf("index block missing or wrong shape: %v", m["index"])
	}
	if c, _ := idx["chunk_count"].(float64); c <= 0 {
		t.Errorf("index.chunk_count should be >0, got %v", idx["chunk_count"])
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

// CKV-4: a panic inside a tool handler must not crash the process.
// Without WithRecovery, the panic would unwind past the MCP dispatcher
// and ServeStdio, the OS would close stdout, and cks (subprocess
// caller) would see "transport closed" mid-eval. With WithRecovery
// installed in NewServer, the same panic surfaces as a normal MCP
// error response and the server stays alive.
func TestServerRecoversFromHandlerPanic(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	// Register a tool that always panics. Uses Underlying() because
	// ckv's own handlers do nil checks and won't panic on bad input —
	// we need a deliberate panic to exercise the recovery path.
	s.Underlying().AddTool(
		mcpgo.NewTool("test.panic"),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			panic("intentional panic for CKV-4 verification")
		},
	)

	resp := s.Underlying().HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {"name": "test.panic"}
	}`))

	errResp, ok := resp.(mcpgo.JSONRPCError)
	if !ok {
		t.Fatalf("expected JSONRPCError on panicking handler, got %T (%v)", resp, resp)
	}
	if !strings.Contains(errResp.Error.Message, "panic recovered") {
		t.Errorf("expected message to contain 'panic recovered', got %q", errResp.Error.Message)
	}
	if !strings.Contains(errResp.Error.Message, "intentional panic") {
		t.Errorf("expected message to relay the panic value, got %q", errResp.Error.Message)
	}

	// Sanity: a second call (on an unrelated tool) should still work,
	// confirming the server didn't terminate.
	follow := s.Underlying().HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "tools/call",
		"params": {"name": "cks.ops.health"}
	}`))
	if _, isErr := follow.(mcpgo.JSONRPCError); isErr {
		t.Errorf("post-panic health call returned error, server may have been disturbed: %v", follow)
	}
}
