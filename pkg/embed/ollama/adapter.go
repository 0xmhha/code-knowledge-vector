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
	"net/http"
	"os"
	"time"
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

// Adapter implements types.Embedder via Ollama's /api/embed endpoint.
type Adapter struct {
	endpoint  string
	modelName string
	dim       int
	client    *http.Client
}

// Options configures the Ollama adapter.
type Options struct {
	Endpoint  string        // Ollama API URL (default: http://localhost:11434)
	ModelName string        // model name as known to Ollama (e.g. "bge-m3")
	Timeout   time.Duration // per-request timeout (default: DefaultTimeout); <=0 uses the default
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

	a := &Adapter{
		endpoint:  endpoint,
		modelName: opts.ModelName,
		client:    &http.Client{Timeout: timeout},
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
	a.dim = len(vecs[0])

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

func (a *Adapter) Name() string        { return a.modelName }
func (a *Adapter) Dimension() int      { return a.dim }
func (a *Adapter) MaxInputTokens() int { return 8192 }
func (a *Adapter) Close() error        { return nil }

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

	return result.Embeddings, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
