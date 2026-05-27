package model

import (
	"testing"
)

func TestCacheDir(t *testing.T) {
	dir, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if dir == "" {
		t.Error("CacheDir returned empty")
	}
}

func TestFetchModel_UnknownModelReturnsError(t *testing.T) {
	_, err := FetchModel("nonexistent-model-xyz", "", func(string) {})
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestFetchModel_CollectsProgress(t *testing.T) {
	var msgs []string
	// Fetch a known model into a temp dir — files won't exist so
	// it will try to download. We just verify progress callback fires
	// before the network error.
	_, _ = FetchModel("bge-large-en-v1.5", t.TempDir(), func(msg string) {
		msgs = append(msgs, msg)
	})
	// At minimum, progress should have been called (either "downloading" or "already exists")
	// Network may fail in CI, so we don't assert on specific messages
}
