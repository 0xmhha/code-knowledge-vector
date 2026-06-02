package ollama_test

// External-import gate for G1 (00 §C2 / 02-ckv-refactor.md M2.a).
//
// This file lives in `package ollama_test` — an EXTERNAL test package — and
// imports BOTH public surfaces a downstream module (cks, doc 03) needs:
//
//	github.com/0xmhha/code-knowledge-vector/pkg/embed/ollama  (the promoted embedder)
//	github.com/0xmhha/code-knowledge-vector/pkg/ckv           (the engine)
//
// If `pkg/embed/ollama` were still under the module's internal/ tree, this
// file would not compile — so the G1 move is what makes the test buildable at
// all. The
// ollama adapter is pure HTTP (no CGO): the construction below runs against an
// httptest server with NO Ollama daemon and NO ONNX/CGO in the embedder path.
// (pkg/ckv still pulls the sqlite-vec CGO store transitively — that is the
// store, not the embedder; C2's "no-CGO" promise is about the embedder cks
// constructs, which is this one.)

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
	"github.com/0xmhha/code-knowledge-vector/pkg/embed/ollama"
)

// fixedDimEmbedServer stands up an Ollama-compatible /api/embed endpoint that
// returns a vector of the requested dimension for every input string. It lets
// us exercise ollama.Open's dimension auto-probe without a live daemon.
func fixedDimEmbedServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := make([][]float32, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dim)
			// Deterministic, non-zero values so the vector is well-formed.
			for j := range vec {
				vec[j] = float32(j%7) * 0.01
			}
			out[i] = vec
		}
		_ = json.NewEncoder(w).Encode(struct {
			Embeddings [][]float32 `json:"embeddings"`
		}{Embeddings: out})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestExternalImport_OllamaConstruct is the core M2.a gate: an external package
// constructs a real ollama.Adapter (pure HTTP, no CGO, no daemon) and the
// auto-probed dimension matches the server's vector width. bge-m3 serves 1024.
func TestExternalImport_OllamaConstruct(t *testing.T) {
	srv := fixedDimEmbedServer(t, 1024)

	emb, err := ollama.Open(ollama.Options{Endpoint: srv.URL, ModelName: "bge-m3"})
	if err != nil {
		t.Fatalf("ollama.Open against httptest server: %v", err)
	}
	defer func() { _ = emb.Close() }()

	if got := emb.Dimension(); got != 1024 {
		t.Errorf("Dimension() = %d, want 1024 (auto-probed from /api/embed)", got)
	}
	if got := emb.MaxInputTokens(); got != 8192 {
		t.Errorf("MaxInputTokens() = %d, want 8192 (bge-m3 window)", got)
	}
	if got := emb.Name(); got != "bge-m3" {
		t.Errorf("Name() = %q, want bge-m3", got)
	}

	// The adapter satisfies the public Embedder contract end-to-end.
	vecs, err := emb.Embed(context.Background(), []string{"quorum size invariant"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 1024 {
		t.Fatalf("Embed shape = [%d][%d], want [1][1024]", len(vecs), len(vecs[0]))
	}
}

// TestExternalImport_OllamaAndCkvCompose proves the two public packages cks
// imports co-compile and compose in one external program: build a mock index,
// open it via pkg/ckv, and round-trip a SemanticSearch. The ollama adapter is
// constructed (CGO-free) in the same package to prove import compatibility;
// the index itself uses the deterministic mock embedder so the test needs no
// Ollama daemon.
func TestExternalImport_OllamaAndCkvCompose(t *testing.T) {
	// Constructing the ollama adapter here is the compile+runtime proof that
	// pkg/embed/ollama is externally importable alongside pkg/ckv.
	srv := fixedDimEmbedServer(t, 1024)
	if _, err := ollama.Open(ollama.Options{Endpoint: srv.URL, ModelName: "bge-m3"}); err != nil {
		t.Fatalf("ollama.Open: %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	src := filepath.Join(repoRoot, "testdata", "sample")
	out := t.TempDir()
	if _, err := build.Run(context.Background(), build.Options{
		SrcRoot:  src,
		OutDir:   out,
		Embedder: ckv.MockEmbedder(),
	}); err != nil {
		t.Fatalf("build mock index from %s: %v", src, err)
	}

	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("ckv.Open: %v", err)
	}
	defer func() { _ = engine.Close() }()

	resp, err := engine.SemanticSearch(context.Background(),
		"server listen function", ckv.SearchOptions{K: 5, Threshold: -1})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if resp == nil || len(resp.Hits) == 0 {
		t.Fatal("expected at least one hit from mock embedder + sample corpus")
	}
}
