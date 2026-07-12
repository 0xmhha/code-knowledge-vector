package llmprefix

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

// OllamaGenerator generates contextual prefixes via a local Ollama chat model
// (POST /api/generate, non-streaming). The default endpoint is overridable with
// CKV_OLLAMA_ENDPOINT, matching the embedder adapter.
type OllamaGenerator struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaGenerator builds a generator for the named Ollama model (e.g.
// "llama3"). Requires `ollama serve` with the model pulled.
func NewOllamaGenerator(model string) *OllamaGenerator {
	ep := os.Getenv("CKV_OLLAMA_ENDPOINT")
	if ep == "" {
		ep = "http://localhost:11434"
	}
	return &OllamaGenerator{
		endpoint: ep,
		model:    model,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (g *OllamaGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":  g.model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", err
	}
	url := g.endpoint + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama generate HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama generate decode: %w", err)
	}
	return out.Response, nil
}
