//go:build bgeonnx && bgeonnx_smoke

// Smoke test for the production tokenizer. Skipped unless the model
// directory has tokenizer.json on disk — see docs/d1-installation-guide.md.
// Run: go test -tags 'bgeonnx bgeonnx_smoke' ./internal/embed/bgeonnx/

package bgeonnx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func defaultModelDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".cache", "ckv", "models", ModelName)
}

func TestHFTokenizerSmoke_PadsToMaxInBatch(t *testing.T) {
	dir := defaultModelDir(t)
	if _, err := os.Stat(filepath.Join(dir, fileTokenizer)); err != nil {
		t.Skipf("tokenizer.json not installed at %s — see docs/d1-installation-guide.md", dir)
	}
	tk, err := newHFTokenizer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer tk.Close()

	// Two inputs of very different lengths — the short one should be
	// padded to match the long one (not to ModelMaxInput).
	short := "x"
	long := "def fetch_user(id: int) -> User: " + // simulate ~30-token code snippet
		"return repo.get(id) if id > 0 else None"
	out, err := tk.Tokenize(context.Background(), []string{short, long}, ModelMaxInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.InputIDs) != 2 {
		t.Fatalf("expected 2 sequences, got %d", len(out.InputIDs))
	}
	if len(out.InputIDs[0]) != len(out.InputIDs[1]) {
		t.Errorf("padding mismatch: short=%d long=%d", len(out.InputIDs[0]), len(out.InputIDs[1]))
	}
	// AttentionMask on the padded tail of `short` must be 0.
	mask := out.AttentionMask[0]
	if mask[len(mask)-1] != 0 {
		t.Errorf("expected trailing attention=0 on padded short input, got %d", mask[len(mask)-1])
	}
}

func TestHFTokenizerSmoke_TruncatesAboveMaxLen(t *testing.T) {
	dir := defaultModelDir(t)
	if _, err := os.Stat(filepath.Join(dir, fileTokenizer)); err != nil {
		t.Skipf("tokenizer.json not installed at %s — see docs/d1-installation-guide.md", dir)
	}
	tk, err := newHFTokenizer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer tk.Close()

	// Build a string guaranteed to exceed maxLen=16 tokens.
	long := ""
	for range 200 {
		long += "function "
	}
	out, err := tk.Tokenize(context.Background(), []string{long}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(out.InputIDs[0]); got != 16 {
		t.Errorf("expected truncation to 16, got %d", got)
	}
}
