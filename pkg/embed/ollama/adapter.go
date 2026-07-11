// Package ollama implements the Embedder interface via Ollama's HTTP
// API. Useful when ONNX model files are unavailable (e.g. HuggingFace
// blocked) or when the user prefers Ollama's model management.
//
// Prerequisites: Ollama running locally (ollama serve) with the
// desired model pulled (ollama pull bge-m3).
//
// Usage:
//
//	ckv build --embedder=ollama --model-name=bge-m3 --src ./project
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/registry"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultEndpoint is the default Ollama API base URL.
// Override with CKV_OLLAMA_ENDPOINT environment variable.
const DefaultEndpoint = "http://localhost:11434"

// DefaultTimeout bounds every request to the Ollama daemon. Without it a
// wedged daemon (model loading, stuck GPU) that accepts the connection but
// never responds would block embed calls — and the startup probe — forever,
// hanging the build, the query path, and any consumer that opens the adapter
// at startup. A single embed of a chunk batch should be well under this.
const DefaultTimeout = 60 * time.Second

// DefaultMaxInputTokens is the fallback context limit for an Ollama model not
// present in the embedding registry. bge-m3's value; safe for BERT-family
// embedders that don't advertise a larger window.
const DefaultMaxInputTokens = 8192

// Adapter implements types.Embedder via Ollama's /api/embed endpoint.
type Adapter struct {
	endpoint      string
	modelName     string
	dim           int
	targetDim     int    // >0 → truncate each embedding to this many dims (MRL)
	queryInstruct string // non-empty → wrap queries in this Qwen3 instruct prompt
	maxInput      int
	client        *http.Client
}

// Options configures the Ollama adapter.
type Options struct {
	Endpoint  string        // Ollama API URL (default: http://localhost:11434)
	ModelName string        // model name as known to Ollama (e.g. "bge-m3")
	Timeout   time.Duration // per-request timeout (default: DefaultTimeout); <=0 uses the default
	// TargetDim, when >0 and smaller than the model's native dimension,
	// truncates every embedding to its first TargetDim components and
	// re-normalizes to unit length (Matryoshka Representation Learning). Used
	// by Qwen3-Embedding, which is MRL-trained, to trade a little precision for
	// a smaller vector (storage + search cost). 0 keeps the native dimension.
	TargetDim int
}

// Open creates an Ollama adapter and verifies connectivity by
// embedding a test string to determine the output dimension.
func Open(opts Options) (*Adapter, error) {
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("CKV_OLLAMA_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if opts.ModelName == "" {
		return nil, fmt.Errorf("ollama: model name is required (--model-name)")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	queryInstruct := registry.QueryInstruct(opts.ModelName)
	if os.Getenv("CKV_DISABLE_QUERY_PREFIX") == "1" {
		queryInstruct = "" // opt out of the asymmetric query prompt (A/B, debugging)
	}
	a := &Adapter{
		endpoint:      endpoint,
		modelName:     opts.ModelName,
		queryInstruct: queryInstruct,
		maxInput:      resolveMaxInput(opts.ModelName),
		client:        &http.Client{Timeout: timeout},
	}

	// Probe: embed a short string to discover the dimension. Bound it with a
	// context deadline too, so a wedged daemon fails fast at startup instead
	// of stalling the consumer that opened the adapter.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	vecs, err := a.Embed(ctx, []string{"dimension probe"})
	if err != nil {
		return nil, fmt.Errorf("ollama: connectivity check failed: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("ollama: model %q returned empty embedding", opts.ModelName)
	}
	nativeDim := len(vecs[0])
	a.dim = nativeDim

	// MRL truncation: the probe above ran with targetDim unset (native), so
	// nativeDim is authoritative for validation. Enabling it here makes every
	// subsequent Embed truncate + renormalize, and reports the reduced
	// dimension via Dimension()/Identity().
	if opts.TargetDim > 0 {
		if opts.TargetDim > nativeDim {
			return nil, fmt.Errorf("ollama: target dim %d exceeds model %q native dim %d",
				opts.TargetDim, opts.ModelName, nativeDim)
		}
		a.targetDim = opts.TargetDim
		a.dim = opts.TargetDim
	}

	return a, nil
}

// httpClient returns the adapter's timeout-bound client, falling back to a
// default-timeout client when the Adapter was constructed directly (e.g. in
// tests) rather than via Open — so no request is ever unbounded.
func (a *Adapter) httpClient() *http.Client {
	if a.client != nil {
		return a.client
	}
	return &http.Client{Timeout: DefaultTimeout}
}

func (a *Adapter) Name() string   { return a.modelName }
func (a *Adapter) Dimension() int { return a.dim }
func (a *Adapter) MaxInputTokens() int {
	if a.maxInput > 0 {
		return a.maxInput
	}
	return DefaultMaxInputTokens
}

// resolveMaxInput returns the model's context limit. It honors the registry's
// per-model MaxInput (matching the bgeonnx backend) so swapping the embedding
// model carries the right truncation budget; models Ollama serves but the
// registry does not know fall back to DefaultMaxInputTokens.
func resolveMaxInput(modelName string) int {
	if cfg, err := registry.Lookup(modelName); err == nil && cfg.MaxInput > 0 {
		return cfg.MaxInput
	}
	return DefaultMaxInputTokens
}

// Identity reports the embedding space. Ollama performs tokenization and
// pooling internally and does not expose those, so Pooling/Normalize are
// left empty; Provider+Model+Dim still distinguish an Ollama-built index
// from one built by another backend (e.g. ONNX) for the same model name,
// which is the swap query.Open must reject.
func (a *Adapter) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{
		Provider: "ollama",
		Model:    a.modelName,
		Dim:      a.dim,
	}
}
func (a *Adapter) Close() error { return nil }

// Embed calls Ollama's /api/embed endpoint with batch input.
func (a *Adapter) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	if len(batch) == 0 {
		return nil, nil
	}

	reqBody := embedRequest{
		Model: a.modelName,
		Input: batch,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	url := a.endpoint + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: HTTP request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: HTTP %d from %s: %s", resp.StatusCode, url, string(respBody))
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}

	// Validate the response shape at the boundary: a short response would
	// otherwise be paired positionally with the wrong chunks downstream,
	// surfacing as a confusing error far from the cause.
	if len(result.Embeddings) != len(batch) {
		return nil, fmt.Errorf("ollama: embedding count mismatch: got %d for %d inputs from %s",
			len(result.Embeddings), len(batch), url)
	}

	if a.targetDim > 0 {
		for i := range result.Embeddings {
			result.Embeddings[i] = truncateNormalize(result.Embeddings[i], a.targetDim)
		}
	}

	return result.Embeddings, nil
}

// EmbedQuery embeds retrieval queries. For an asymmetric model (Qwen3, which
// carries a QueryInstruct in the registry) it wraps each query in the model's
// instruct prompt before embedding; symmetric models (bge-*) fall through to
// Embed unchanged. Passages always go through Embed. Implements
// types.QueryEmbedder.
func (a *Adapter) EmbedQuery(ctx context.Context, queries []string) ([][]float32, error) {
	if a.queryInstruct == "" || len(queries) == 0 {
		return a.Embed(ctx, queries)
	}
	wrapped := make([]string, len(queries))
	for i, q := range queries {
		wrapped[i] = qwen3QueryText(a.queryInstruct, q)
	}
	return a.Embed(ctx, wrapped)
}

// qwen3QueryText builds Qwen3-Embedding's query prompt:
// "Instruct: {task}\nQuery: {query}". Applied to queries only.
func qwen3QueryText(instruct, query string) string {
	return "Instruct: " + instruct + "\nQuery: " + query
}

// truncateNormalize returns the first dim components of v, re-normalized to
// unit L2 length. Qwen3-Embedding is trained with Matryoshka Representation
// Learning, so a prefix of the full vector is itself a valid lower-dimensional
// embedding once renormalized. dim <= 0 or dim >= len(v) returns v unchanged.
func truncateNormalize(v []float32, dim int) []float32 {
	if dim <= 0 || dim >= len(v) {
		return v
	}
	out := make([]float32, dim)
	var sum float64
	for i := 0; i < dim; i++ {
		out[i] = v[i]
		sum += float64(v[i]) * float64(v[i])
	}
	if sum > 0 {
		inv := float32(1.0 / math.Sqrt(sum))
		for i := range out {
			out[i] *= inv
		}
	}
	return out
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
