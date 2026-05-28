package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeHTTP_AcceptsRequests verifies the HTTP transport responds
// to a JSON-RPC initialize call. Picks a random free port so the test
// is parallelism-safe.
func TestServeHTTP_AcceptsRequests(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)

	// Pick a random free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Run ServeHTTP in background
	done := make(chan error, 1)
	go func() {
		done <- s.ServeHTTP(addr)
	}()

	// Wait for the server to bind
	if !waitForPort(addr, 2*time.Second) {
		t.Fatalf("server did not bind to %s in time", addr)
	}

	// Send an MCP initialize request
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test",
				"version": "1.0",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("http://%s/mcp", addr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		t.Errorf("server error: %d", resp.StatusCode)
	}
}

func waitForPort(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
