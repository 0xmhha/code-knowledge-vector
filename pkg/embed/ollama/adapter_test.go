package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpen_RequiresModelName(t *testing.T) {
	_, err := Open(Options{})
	if err == nil {
		t.Fatal("expected error when ModelName is empty")
	}
}

func TestEmbed_SendsCorrectRequest(t *testing.T) {
	var receivedModel string
	var receivedInput []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedModel = req.Model
		receivedInput = req.Input

		resp := embedResponse{
			Embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := &Adapter{
		endpoint:  server.URL,
		modelName: "test-model",
		dim:       3,
	}

	vecs, err := a.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if receivedModel != "test-model" {
		t.Errorf("model = %q, want test-model", receivedModel)
	}
	if len(receivedInput) != 1 || receivedInput[0] != "hello world" {
		t.Errorf("input = %v, want [hello world]", receivedInput)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Errorf("vecs shape = [%d][%d], want [1][3]", len(vecs), len(vecs[0]))
	}
}

func TestEmbed_EmptyBatch(t *testing.T) {
	a := &Adapter{endpoint: "http://unused", modelName: "m", dim: 3}
	vecs, err := a.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty batch, got %v", vecs)
	}
}

func TestEmbed_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	a := &Adapter{endpoint: server.URL, modelName: "bad", dim: 3}
	_, err := a.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error on server 500")
	}
}

func TestAdapter_Interface(t *testing.T) {
	a := &Adapter{modelName: "test", dim: 768}
	if a.Name() != "test" {
		t.Errorf("Name = %q", a.Name())
	}
	if a.Dimension() != 768 {
		t.Errorf("Dimension = %d", a.Dimension())
	}
	if a.MaxInputTokens() <= 0 {
		t.Errorf("MaxInputTokens = %d", a.MaxInputTokens())
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
