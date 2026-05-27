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
)

// DefaultEndpoint is the default Ollama API base URL.
// Override with CKV_OLLAMA_ENDPOINT environment variable.
const DefaultEndpoint = "http://localhost:11434"

// Adapter implements types.Embedder via Ollama's /api/embed endpoint.
type Adapter struct {
	endpoint  string
	modelName string
	dim       int
}

// Options configures the Ollama adapter.
type Options struct {
	Endpoint  string // Ollama API URL (default: http://localhost:11434)
	ModelName string // model name as known to Ollama (e.g. "bge-m3")
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

	a := &Adapter{
		endpoint:  endpoint,
		modelName: opts.ModelName,
	}

	// Probe: embed a short string to discover the dimension.
	vecs, err := a.Embed(context.Background(), []string{"dimension probe"})
	if err != nil {
		return nil, fmt.Errorf("ollama: connectivity check failed: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("ollama: model %q returned empty embedding", opts.ModelName)
	}
	a.dim = len(vecs[0])

	return a, nil
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

	resp, err := http.DefaultClient.Do(req)
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

	return result.Embeddings, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
